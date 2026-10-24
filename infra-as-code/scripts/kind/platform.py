# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Install the shared chart into the owned Kind cluster with its private dependency and component configuration."""
import base64
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import secrets
import socket
import subprocess
import time
from urllib.parse import urlsplit

import yaml

import build_inputs
from commands import run, stop_children, wait_until
from config_files import includes, merge
import git_host
import published_dependencies
from kind.cluster import (CLUSTER, GIT, KIND_NODE_IMAGE, KUBECONFIG, NAMESPACE, NODE_PATH, SERVER_CONFIG, SERVER_CONFIG_KEY, apply, helm, kubectl,
                          missing_node_mounts, object_meta, port_forward_argv, prepare_repositories, read_object, recreation_advice,
                          require_node_mount, require_tools, save, start_nodes, state, verify_owner)
from kind import gateway, server_config
if importlib.util.find_spec("kind.engine") is not None:
    from kind import engine
else:
    engine = None  # The private Engine layer is absent from the public mirror.
from local_platform import (POSTGRES_CREDENTIALS, POSTGRES_DATABASE, POSTGRES_OWNER, POSTGRES_SERVER, POSTGRES_SUPERUSER, READ_MODEL_STORES, create_buckets,
                            dex_config, keycloak_credentials, keycloak_secret_environment, postgres_credentials, postgres_roles_sql)
from kind.deployment_identity import json_digest, kind_deployment_identity
from image_identity import docker_image_id
from kind.images import dependency_reference, load_dependency, resolve_project_dependency_images
from kind.node_images import component_reference
from kind.server_secrets import read_server_secrets, require_server_secrets
from operator_errors import require
from paths import CHART, LOCAL_KIND_VALUES, MAIN_CHECKOUT, ROOT, STATE, project_env_value
from pins import local_dependency_image, local_image
from private_files import retained_secret, write_private

CHART_VALUES = STATE / "helm-values.json"
DEPENDENCY_CREDENTIALS = STATE / "dependencies.json"


def require_login_loopback_hosts(document):
    """The browser's two canonical Kind names must resolve only to loopback."""
    origins = [document.get("studio", {}).get("publicOrigin", "")]
    login = document.get("accounts", {}).get("loginHost", {})
    if login.get("handoffEnabled") or login.get("exchangeEnabled"):
        origins.append(login.get("publicOrigin", ""))
    for origin in origins:
        host = urlsplit(origin).hostname
        try:
            addresses = {answer[4][0] for answer in socket.getaddrinfo(host, None)}
        except OSError:
            addresses = set()
        require(addresses and all(address.startswith("127.") or address == "::1" for address in addresses),
                f"{host} must resolve to loopback; add it to the local hosts file before starting Kind")


def login_auth_secrets(enabled_clients):
    """Retain distinct private local credentials across Kind redeploys and replicas."""
    def key(name):
        return retained_secret(STATE / ("login-" + name + "-key"),
                               lambda: base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip("="))
    return {"issuer": {"current": key("issuer")}, "clients": {id: {"current": key(id + "-client")} for id in enabled_clients},
            "browser": {"login": {"current": key("browser-login")}, "product": {"current": key("browser-product")}}}


def up(_args):
    require_tools("docker", "kind", "kubectl", "helm", "git", "aws")
    value = state(required=False)
    require(value is None or not value.get("engineEnabled") or engine is not None,
            "The deployed private Kind Engine layer is unavailable in this checkout")
    if CLUSTER in run(["kind", "get", "clusters"], capture=True).splitlines():
        require(value is not None, "Existing superverse-v2 cluster is not owned by this checkout; refusing adoption")
        verify_owner()
        # Dependencies work without it, but a deploy of the server needs the node mount that only a new cluster gets.
        missing = missing_node_mounts()
        if missing:
            print(recreation_advice(missing))
    else:
        if value is None:
            value = {"version": 1, "checkout": str(MAIN_CHECKOUT), "cluster": CLUSTER, "owner": secrets.token_hex(16), "images": {}}
            save(value)
        config_path = STATE / "kind.json"
        # The node mounts the Kind lane's local Git repositories of the main checkout, which the chart mounts into the server Pod.
        prepare_repositories()
        mounts = [{"hostPath": str(GIT.repositories), "containerPath": NODE_PATH}]
        write_private(config_path, json.dumps({"kind": "Cluster", "apiVersion": "kind.x-k8s.io/v1alpha4",
                                               "nodes": [{"role": "control-plane", "extraMounts": mounts}]}))
        run(["kind", "create", "cluster", "--name", CLUSTER, "--image", KIND_NODE_IMAGE, "--kubeconfig", KUBECONFIG, "--config", config_path, "--wait", "180s"], timeout=900)
        start_nodes()  # a created node restarts with the engine until this sets its restart policy
        KUBECONFIG.chmod(0o600)
        apply({"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": NAMESPACE, "labels": {"app.kubernetes.io/part-of": "superverse-v2"}}})
        apply({"apiVersion": "v1", "kind": "ConfigMap", "metadata": object_meta("superverse-v2-owner"), "data": {"checkout": str(MAIN_CHECKOUT), "owner": value["owner"]}})
    credentials = json.loads(retained_secret(DEPENDENCY_CREDENTIALS, lambda: json.dumps({"access": secrets.token_hex(12), "secret": secrets.token_urlsafe(36)})))
    apply({"apiVersion": "v1", "kind": "Secret", "metadata": object_meta("superverse-v2-minio"),
           "stringData": {"MINIO_ROOT_USER": credentials["access"], "MINIO_ROOT_PASSWORD": credentials["secret"]}})
    # The superuser password only seeds a new data volume; the roles script below sets every password on each run.
    postgres = postgres_credentials(STATE)
    apply({"apiVersion": "v1", "kind": "Secret", "metadata": object_meta("superverse-v2-postgres"),
           "stringData": {"POSTGRES_PASSWORD": postgres[POSTGRES_SUPERUSER], "SUPERVERSE_POSTGRES_OWNER_PASSWORD": postgres[POSTGRES_OWNER]}})
    apply({"apiVersion": "v1", "kind": "Secret", "metadata": object_meta("superverse-v2-keycloak"),
           "stringData": keycloak_secret_environment(keycloak_credentials(STATE))})
    # An existing cluster keeps its accepted MinIO image; only a new one loads the current pin. A recorded image whose
    # local tag a Docker prune removed loads again from its digest-pinned source, which must give the recorded image.
    architecture = build_inputs.architecture()
    dependencies = value.setdefault("dependencies", {})
    if "minio" not in dependencies:
        dependencies["minio"] = load_dependency("minio", local_dependency_image("minio", architecture))
    elif docker_image_id(dependencies["minio"]["image"]) is None:
        require(load_dependency("minio", dependencies["minio"]["source"]) == dependencies["minio"], "The restored MinIO dependency is not the recorded image")
    value["localDependencies"] = {"temporalImage": local_dependency_image("temporal", architecture), "minioImage": dependency_reference("minio", dependencies["minio"]),
                                  "postgresImage": local_image("postgres"), "keycloakImage": local_image("keycloak")}
    # Once a deploy migrated the read model and gave Dex its Attribute Store, Dex keeps the stores that deploy rendered, not
    # the checkout's, whose new tables only the next deploy's migrations create; a new cluster starts without.
    stores = dict(READ_MODEL_STORES)
    if value.get("engineEnabled"):
        stores.update(engine.read_model_stores(engine.credentials()))
    dex = kind_dex_config(credentials, postgres if value.get("readModelStore") else None, stores)
    # The digest reaches the Dex Pod template, so a changed configuration restarts Dex.
    value["dexConfigRevision"] = json_digest(dex)
    save(value)
    apply({"apiVersion": "v1", "kind": "Secret", "metadata": object_meta("superverse-v2-dex-config"), "stringData": {"config.yaml": json.dumps(dex)}})
    chart_values = local_chart_values(value)
    if not value.get("deployedSourceFingerprint"):
        chart_values["server"]["enabled"] = False
        chart_values["studio"]["enabled"] = False
    elif not value.get("deploymentInputs", {}).get("config", {}).get("input"):
        # A server deployed before the configuration profiles has no document to render, and without one it only crash-loops.
        # It stays scaled down until `kind deploy` renders a profile.
        chart_values["server"]["replicas"] = 0
        print("The deployed server predates the server configuration profiles: it stays scaled down until `kind deploy` renders one.")
    install_chart(chart_values, wait=False)
    for name in ("temporal", "minio", "postgres"):
        kubectl("rollout", "status", "statefulset/" + name, "-n", NAMESPACE, "--timeout=300s", timeout=330)
    initialize_buckets(credentials)
    create_postgres_roles(postgres)
    if value.get("engineEnabled"):
        kubectl("rollout", "status", "statefulset/paradedb", "-n", NAMESPACE, "--timeout=300s", timeout=330)
        engine.create_database_roles(engine.credentials())
    kubectl("rollout", "status", "deployment/superverse-v2-dex", "-n", NAMESPACE, "--timeout=300s", timeout=330)
    kubectl("rollout", "status", "deployment/keycloak", "-n", NAMESPACE, "--timeout=300s", timeout=330)
    kubectl("rollout", "status", "deployment/agent-sandbox-controller", "-n", "agent-sandbox-system", "--timeout=300s", timeout=330)
    print("V2 dependency readiness passed. This is not E2E acceptance.")


def deploy(args):
    value = verify_owner()
    require(not args.engine_config or engine is not None, "The private Kind Engine layer is unavailable in this checkout")
    require(not value.get("engineEnabled") or engine is not None, "The deployed private Kind Engine layer is unavailable")
    require(args.engine_config or not value.get("engineEnabled"),
            "This Kind cluster has an Engine layer; pass --engine-config to preserve it on deploy")
    require(args.secret_env_file, "Kind deploy requires --secret-env-file with the server secrets: the Secret is replaced as a whole, so every secret must be in it")
    require(args.published_dependencies, "Kind deploy requires --published-dependencies with a committed approval from infra-as-code/dependencies/published")
    dependencies = published_dependencies.load(args.published_dependencies, verify_publication=True)
    fingerprint = build_inputs.kind_behavior_fingerprint()
    for name in build_inputs.COMPONENTS:
        require(value["images"].get(name, {}).get("sourceFingerprint") == build_inputs.component_fingerprint(name),
                "Build the current " + name + " component before deployment")
    document, config_record = server_config.load_profile(args.config)
    server_config.require_kind_profile(document)
    require_login_loopback_hosts(document)
    mock = bool(document.get("agent", {}).get("mockModel"))
    repohost = git_host.repository_host(document)
    credentials = json.loads(DEPENDENCY_CREDENTIALS.read_text())
    server = {"AWS_ACCESS_KEY_ID": credentials["access"], "AWS_SECRET_ACCESS_KEY": credentials["secret"], "SUPERVERSE_COOKIE_SIGNING_KEY": cookie_signing_key()}
    # The env file may be shared with other layers: only its secrets are taken, and every other key is ignored with one notice.
    # It never supplies ANTHROPIC_API_KEY.
    server.update(read_server_secrets(args.secret_env_file))
    # The Local Kind chart mounts the repository directory of the node into the server Pod whatever the profile's repository host, and a
    # cluster created before the local Git host has no such mount: recreating the cluster adds it.
    require_node_mount()
    if repohost == git_host.LOCAL_GIT:
        # Only generated, never from the env file: the end-to-end runner derives its administrator token from the same retained key.
        server["SUPERVERSE_LOCAL_GIT_SIGNING_KEY"] = GIT.signing_key()
    # Only generated, never from the env file: kind up created the role with this password, and the integration tests read the same file.
    require((STATE / POSTGRES_CREDENTIALS).exists(), "Run `sv2 kind up` first: it creates the platform PostgreSQL and its roles")
    postgres = postgres_credentials(STATE)
    server["SUPERVERSE_POSTGRES_PASSWORD"] = postgres[POSTGRES_SERVER]
    # The relay tests sign events with the retained test Signing Secret; a developer's manual test with a real Slack app sets its
    # Signing Secret and the platform client's secret in the same .env (docs/runbooks/slack-relay-manual-test.md).
    server["SUPERVERSE_SLACK_SIGNING_SECRET"] = project_env_value("SUPERVERSE_SLACK_SIGNING_SECRET") or slack_signing_secret()
    client_secrets = project_env_value("SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS")
    if client_secrets:
        server["SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS"] = client_secrets
    login = document.get("accounts", {}).get("loginHost", {})
    if login.get("handoffEnabled") or login.get("exchangeEnabled"):
        auth = login_auth_secrets([id for id, client in login.get("clients", {}).items() if client.get("enabled")])
        server["SUPERVERSE_LOGIN_HOST_ISSUER_SECRET_RING"] = json.dumps(auth["issuer"])
        server["SUPERVERSE_LOGIN_HOST_CLIENT_SECRET_RINGS"] = json.dumps(auth["clients"])
    require_server_secrets(server, repohost)
    chart_values = local_chart_values(value)
    profile_name = rendered_profile_name(chart_values)
    # The values this deploy computes: the coding profile the chart installs and the project images it verified on every node.
    document = merge(document, {"sandbox": {"profile": profile_name}, "live": resolve_project_dependency_images(value, dependencies["projectDexImage"])})
    require(document.get("studio", {}).get("publicOrigin") == studio_origin(), "studio.publicOrigin must be the Studio origin of the Local Kind values: " + studio_origin())
    server_config.preflight(document)
    chart_values["server"]["config"] = document
    studio = {}
    if args.studio_secret_env_file:
        studio = read_env_file(args.studio_secret_env_file,
                               lambda key: key == "SUPERVERSE_COOKIE_SIGNING_KEY" or key.startswith(("AWS_", "OPENAI_", "ANTHROPIC_", "SUPERVERSE_SECRET_")),
                               "Studio env file lines must be KEY=value; the server key and provider/credential-store keys must not be copied into Studio")
    account_key = base64.b64decode(server["SUPERVERSE_COOKIE_SIGNING_KEY"], validate=True)
    require(len(account_key) >= 32, "Account session signing requires the accepted server key")
    studio["BROWSER_SIGNING_KEY"] = browser_signing_key(account_key, studio.get("BROWSER_SIGNING_KEY"))
    if login.get("handoffEnabled") or login.get("exchangeEnabled"):
        studio["SUPERVERSE_LOGIN_HOST_ISSUER_SECRET_RING"] = json.dumps(auth["issuer"])
        studio["SUPERVERSE_LOGIN_HOST_CLIENT_SECRET_RINGS"] = json.dumps(auth["clients"])
        studio["SUPERVERSE_LOGIN_BROWSER_KEYS"] = json.dumps(auth["browser"])
    apply({"apiVersion": "v1", "kind": "Secret", "metadata": object_meta("superverse-v2-server"), "stringData": server},
          {"apiVersion": "v1", "kind": "Secret", "metadata": object_meta("superverse-v2-studio"), "stringData": studio})
    prune_secret("superverse-v2-server", server)
    # A Secret change changes the Pod template checksum, which rolls the component; so does a change of the configuration document.
    server_digest = json_digest(server)
    chart_values["server"]["secretRevision"] = server_digest
    chart_values["studio"]["secretRevision"] = json_digest(studio)
    # The migrations grant to every role, so a role the source added must exist before the server's init container migrates.
    create_postgres_roles(postgres)
    install_chart(chart_values)
    # What the chart rendered is what the server loads; it is the document and the digest that the deployment records.
    rendered = read_object("configmap", SERVER_CONFIG)["data"][SERVER_CONFIG_KEY]
    loaded = yaml.safe_load(rendered)
    require(includes(loaded, document), "The chart rendered a server configuration that differs from the deployed profile")
    live = document["live"]
    value["deploymentInputs"] = {
        "chartFingerprint": build_inputs.fingerprint(build_inputs.files_under("infra-as-code/kubernetes")),
        "projectDexImage": live["dexImage"], "temporalImage": live["temporalImage"], "storageImage": live["s3Image"], "profile": profile_name,
        # The profile, the digests of its checked-in layers, the merged document that went into the chart (input), the ConfigMap
        # document the chart rendered, which the server loads (document), and the SHA-256 of that ConfigMap's text.
        "config": dict(config_record, input=document, document=loaded, renderedSHA256=hashlib.sha256(rendered.encode()).hexdigest()),
        "secretDigest": server_digest, "mockModel": mock, "agentModel": document["agent"]["model"],
        "buildProfile": dependencies["buildProfile"], "artifactAuthority": dependencies["artifactAuthority"],
        "publishedApprovalSHA256": hashlib.sha256(Path(args.published_dependencies).read_bytes()).hexdigest()}
    for name in ("server", "studio", "dex"):
        kubectl("rollout", "status", "deployment/superverse-v2-" + name, "-n", NAMESPACE, "--timeout=300s", timeout=330)
    if args.engine_config:
        engine_profile = engine.load_profile(args.engine_config)
        engine_values = engine.chart_values(engine_profile, credentials, engine.session_signing_key())
        engine_values["runtimeEnabled"] = bool(value.get("engineEnabled"))
        chart_values["engineKind"] = engine_values
        engine.label_node()
        engine.create_bucket(engine_profile, credentials)
        install_chart(chart_values)
        kubectl("rollout", "status", "statefulset/paradedb", "-n", NAMESPACE, "--timeout=300s", timeout=330)
        engine_roles = engine.credentials()
        engine.create_database_roles(engine_roles)
        engine.migrate(engine_roles)
        stores = dict(READ_MODEL_STORES)
        stores.update(engine.read_model_stores(engine_roles))
        configure_read_model(value, chart_values, credentials, postgres, stores)
        engine_values["runtimeEnabled"] = True
        install_chart(chart_values)
        kubectl("rollout", "status", "deployment/dexengine-server", "-n", NAMESPACE, "--timeout=300s", timeout=330)
        value.update(engineEnabled=True, engineChartValues=engine_values)
        value["deploymentInputs"]["engine"] = {
            "profile": str(engine.PROFILE.relative_to(ROOT)),
            "profileSHA256": engine_values["profileSHA256"], "sourceFingerprint": engine_values["sourceFingerprint"],
            "controlImage": engine_values["controlImage"], "serverZDImage": engine_values["config"]["engineServers"]["serverImage"],
            "paradeDBImage": engine_values["paradeDBImage"], "configRevision": engine_values["configRevision"]}
    else:
        configure_read_model(value, chart_values, credentials, postgres)
    deployed_images = copy.deepcopy(value["images"])
    value["runtimeIdentity"] = wait_until(lambda: kind_deployment_identity(profile_name, deployed_images, value.get("engineChartValues")), seconds=90, interval=2)
    require(state()["images"] == deployed_images, "Component build identities changed during deployment; redeploy before acceptance")
    require(build_inputs.kind_behavior_fingerprint() == fingerprint, "Behavioral inputs changed during deployment; acceptance is not valid")
    value["deployedImages"] = deployed_images
    value["deployedSourceFingerprint"] = fingerprint
    value["commonSourceFingerprint"] = build_inputs.common_behavior_fingerprint()
    save(value)
    print("V2 platform readiness passed. Run ports and e2e for acceptance.")


def kind_dex_config(credentials, postgres=None, stores=None):
    """The shared platform Dex configuration for outbound pull Workers."""
    return dex_config(credentials, postgres, stores)


def create_postgres_roles(postgres):
    """Through the container's socket as the superuser: the roles of the server, its migrations and Dex, with the retained passwords."""
    kubectl("exec", "-i", "-n", NAMESPACE, "statefulset/postgres", "--", "psql", "-v", "ON_ERROR_STOP=1", "-U", POSTGRES_SUPERUSER, "-d", POSTGRES_DATABASE,
            input=postgres_roles_sql(postgres), private=True, timeout=60)


def configure_read_model(value, chart_values, credentials, postgres, stores=None):
    """Give platform Dex the read model's Attribute Store, which the server's Flows select where they start.

    Dex refuses to start while a table of the store is missing, so the store is configured only after the server's rollout,
    whose init container migrated; until Dex restarts with it, the first such deploy's server cannot start those Flows.
    A later deploy changes nothing here and restarts nothing: only a changed configuration rolls Dex, through its Pod
    template's configuration digest.
    """
    dex = kind_dex_config(credentials, postgres, stores)
    revision = json_digest(dex)
    changed = revision != value.get("dexConfigRevision")
    if changed:
        apply({"apiVersion": "v1", "kind": "Secret", "metadata": object_meta("superverse-v2-dex-config"), "stringData": {"config.yaml": json.dumps(dex)}})
    # Recorded at once, apart from the rest of the deploy's state, so that a later up keeps the store when this deploy fails.
    value.update(readModelStore=True, readModelStores=READ_MODEL_STORES, dexConfigRevision=revision)
    recorded = state()
    recorded.update(readModelStore=True, readModelStores=READ_MODEL_STORES, dexConfigRevision=revision)
    save(recorded)
    if changed:
        chart_values["dex"]["configRevision"] = revision
        install_chart(chart_values)
        kubectl("rollout", "status", "deployment/superverse-v2-dex", "-n", NAMESPACE, "--timeout=300s", timeout=330)


def studio_origin():
    """The fixed Studio origin of the Local Kind values, which the server configuration must repeat."""
    return yaml.safe_load(LOCAL_KIND_VALUES.read_text())["studio"]["config"]["SUPERVERSE_STUDIO_PUBLIC_ORIGIN"]


def local_chart_values(value):
    """The dependency images, each built component's Kind-loaded digest alias and the server configuration last deployed."""
    dependencies = dict(value["localDependencies"], minioImage=dependency_reference("minio", value["dependencies"]["minio"]))
    result = {"localDependencies": dependencies, "server": {}, "studio": {}, "dex": {"configRevision": value.get("dexConfigRevision", "")},
              "engineKind": value.get("engineChartValues", {"enabled": False})}
    deployed = value.get("deploymentInputs", {}).get("config", {}).get("input")
    if deployed:
        result["server"]["config"] = deployed
    for name, record in value["images"].items():
        repository, digest = component_reference(name, record).split("@", 1)
        image = {"repository": repository, "digest": digest}
        if name == "coding":
            result["sandboxProfiles"] = {"codingProfile": {"enabled": True, "image": image}}
        else:
            result[name]["image"] = image
    return result


def rendered_profile_name(values):
    """The content-addressed coding profile name these chart values render."""
    write_private(CHART_VALUES, json.dumps(values))
    rendered = run(["helm", "template", "superverse-v2", CHART, "--namespace", NAMESPACE, "--include-crds", "-f", LOCAL_KIND_VALUES, "-f", CHART_VALUES], capture=True)
    profiles = set(re.findall(r"name: (coding-sandbox-profile-coding-v4-[0-9a-f]{12})(?:\n|\s)", rendered))
    require(len(profiles) == 1, "Shared chart did not render one immutable coding profile")
    return profiles.pop()


# Older Kind deployments used a kubectl-owned Recreate strategy for the local Git directory.
SERVER_DEPLOYMENT = "superverse-v2-server"


def adopt_rolling_strategy():
    """Transfer an old Recreate strategy to Helm before its server-side apply.

    Cross-process Git locks allow overlapping replicas. The previous migration used kubectl-patch as the field manager;
    transferring the strategy with Helm's manager avoids an ownership conflict on the first upgrade. The Pod template is
    unchanged, so this migration alone starts no rollout.
    """
    found = kubectl("--request-timeout=30s", "get", "deployment", SERVER_DEPLOYMENT, "-n", NAMESPACE, "--ignore-not-found",
                    "-o", "json", capture=True)
    if not found.strip() or json.loads(found).get("spec", {}).get("strategy", {}).get("type") != "Recreate":
        return
    patch = [{"op": "replace", "path": "/spec/strategy", "value": {
        "type": "RollingUpdate", "rollingUpdate": {"maxUnavailable": 0, "maxSurge": 1}}}]
    kubectl("--request-timeout=30s", "patch", "deployment", SERVER_DEPLOYMENT, "-n", NAMESPACE,
            "--field-manager=helm", "--type=json", "-p", json.dumps(patch))


def install_chart(values, *, wait=True):
    gateway.install(helm, kubectl)
    write_private(CHART_VALUES, json.dumps(values))
    adopt_rolling_strategy()
    helm("upgrade", "--install", "superverse-v2", CHART, "--namespace", NAMESPACE, "-f", LOCAL_KIND_VALUES, "-f", CHART_VALUES,
         "--timeout", "10m", *(["--wait"] if wait else []), timeout=660)
    # Project routes were Ingresses that Traefik served before the project Gateway; none is served any more, and the next
    # Live activation or Preview start creates its HTTPRoute.
    kubectl("delete", "ingress", "--all-namespaces", "-l", "app.kubernetes.io/managed-by=superverse-v2", "--ignore-not-found", "--wait=false")


def read_env_file(path, rejected, message):
    """KEY=value lines of the private Studio env file; `rejected(key)` names keys that must not appear.

    A rejection names the offending key, never a value; a line without `=` is not named at all.
    """
    values = {}
    for line in Path(path).read_text().splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, separator, value = line.partition("=")
        require(separator, message)
        require(not rejected(key.strip()), message + " (refused key: " + key.strip() + ")")
        values[key.strip()] = value
    return values


def prune_secret(name, keys):
    """Remove the keys that a deploy-owned Secret holds but the deploy no longer sets.

    kubectl apply leaves them in `data`, where a setting of an earlier deploy would stay alive beside the configuration
    document. Only key names are handled here, never values.
    """
    stale = sorted(set(read_object("secret", name, private=True).get("data", {})) - set(keys))
    if stale:
        patch = [{"op": "remove", "path": "/data/" + key.replace("~", "~0").replace("/", "~1")} for key in stale]
        kubectl("patch", "secret", name, "-n", NAMESPACE, "--type=json", "-p", json.dumps(patch), private=True)


def slack_signing_secret():
    """The test Signing Secret of the Slack event relay, which the relay tests sign events with."""
    return retained_secret(STATE / "slack-signing-secret", lambda: secrets.token_hex(32))


def cookie_signing_key():
    """The server-only Account session and Watch cursor key."""
    return retained_secret(STATE / "cookie-signing-key", lambda: base64.b64encode(secrets.token_bytes(32)).decode())


def browser_signing_key(account_key, explicit=None):
    """Studio-only key: an explicit key from the Studio env file replaces the retained private one."""
    path = STATE / "browser-signing-key"
    value = explicit if explicit is not None else retained_secret(path, lambda: base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip("="))
    require(re.fullmatch(r"[A-Za-z0-9_-]{43}", value), "BROWSER_SIGNING_KEY requires canonical unpadded base64url for 32 bytes")
    decoded = base64.urlsafe_b64decode(value + "=")
    require(len(decoded) == 32 and base64.urlsafe_b64encode(decoded).decode().rstrip("=") == value,
            "BROWSER_SIGNING_KEY requires canonical unpadded base64url for 32 bytes")
    require(decoded != account_key, "Studio browser signing must not reuse the server Account key")
    if explicit is not None:
        write_private(path, value)
    return value


def initialize_buckets(credentials):
    """Create the versioned local blob and artifact buckets through a private, temporary MinIO forward."""
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        port = reservation.getsockname()[1]
    forward = subprocess.Popen(port_forward_argv("minio", f"{port}:9000"), stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        deadline = time.monotonic() + 30
        while True:
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=1):
                    break
            except OSError:
                require(time.monotonic() < deadline and forward.poll() is None, "V2 MinIO port-forward could not start")
                time.sleep(0.2)
        create_buckets(f"http://127.0.0.1:{port}", credentials)
    finally:
        stop_children([forward])
