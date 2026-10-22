# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Static chart validation; no cluster, cloud, fake provider or business acceptance.

The one Helm source is rendered for every environment with shape-only image references, which are
never deployed or recorded as acceptance. The rendered platform, ingress, coding profile and admission
contracts are checked, and the template guards must reject invalid debug configurations.
"""
import copy
import hashlib
import json
import re
from pathlib import Path
import subprocess
import tempfile
from urllib.parse import urlsplit

import yaml

from git_host import GITHUB, LOCAL_GIT
from kind.cluster import FORWARDS, NODE_PATH as GIT_NODE_PATH
from kind.server_config import BASE as LOCAL_KIND_PROFILE
from kind.server_secrets import DEPLOYED_SECRET_KEYS
from paths import CHART, LOCAL_KIND_VALUES, ROOT
from local_platform import dex_config
from pins import names_for

LOCAL = ("local-kind",)
SHAPE_DIGEST = "sha256:" + "0" * 64
DEBUG_PROXY_IMAGE = "docker.io/nginxinc/nginx-unprivileged@sha256:ed04ec1ff34502c339ee5c3ae3f855442398edc1d05591e2b98981dcbbd20b1e"
CODING_PROFILE = "coding-v4"
PREVIEW_STATE_DIRECTORY = "/var/lib/superverse/preview"
# sandboxd REST and gRPC, and the Sandbox-local Preview Dex.
SANDBOX_PLATFORM_PORTS = (8080, 9090, 8801)
# The Pod reads of the server's namespaced roles; the server never forwards to a Pod.
POD_READ_RULES = [{"apiGroups": [""], "resources": ["pods"], "verbs": ["get", "list"]}]
# Local Kind only: the local Git host's port on the server Pod and Service, and where the Pod mounts its repositories (localgit.dir).
GIT_PORT = 8085
# The server's credential read endpoint (infra.CredentialReadPort), the one server port that project applications and
# Sandboxes reach.
CREDENTIAL_READ_PORT = 8086
# The Slack event relay's events listener (connections.slackRelay.listenAddress, config.SlackEventsPort).
SLACK_EVENTS_PORT = 8087
REMOTE_AGENT_PORT = 8088
GIT_MOUNT = "/var/lib/superverse/git"
# Local Native's platform services, which run with Local Kind's arguments.
NATIVE_COMPOSE = Path(__file__).resolve().parent / "native/compose.yaml"
# Reserved examples exercise hosted templates without AWS credentials or DNS ownership.
STUDIO_CERTIFICATE = "arn:aws:acm:us-west-2:000000000000:certificate/00000001-0000-0000-0000-000000000000"
CERTIFICATE = "arn:aws:acm:us-west-2:000000000000:certificate/00000000-0000-0000-0000-000000000000"
# The Kubernetes API address range that Terraform supplies (network.kubernetesApiCIDRs).
KUBERNETES_API_CIDR = "10.0.32.0/24"
# ALB settings that apply to the whole load balancer of an Ingress group, which every member must agree on.
GROUP_WIDE_ALB_ANNOTATIONS = tuple("alb.ingress.kubernetes.io/" + key for key in (
    "scheme", "ip-address-type", "subnets", "inbound-cidrs", "load-balancer-attributes", "security-groups", "load-balancer-name", "tags"))


def main():
    # The loopback port catalog of `kind ports`; its ports must stay distinct.
    if len({forward.local_port for forward in FORWARDS.values()}) != len(FORWARDS):
        raise ValueError("Local forwarding ports overlap")
    with tempfile.TemporaryDirectory(prefix="sv2-helm-validation-") as temporary:
        directory = Path(temporary)
        for environment in ("local-kind", "test", "staging", "prod"):
            values = environment_values(environment)
            subprocess.run(["helm", "lint", str(CHART), *chart_files(directory, environment, values)], check=True)
            render = Render(environment, helm_template(directory, environment, values))
            render.check()
            print(f"Static chart render passed: {environment}; platform identities, Worker target and {len(render.ingresses)} approved Ingresses verified")
            if environment == "local-kind":
                check_native_platform(render)
            if environment in ("test", "staging"):
                check_debug_variants(directory, environment, values)
            expect_render_rejected(directory, environment, values, ("server", "config", "SUPERVERSE_ENV"), "test", "environment variable name is not a setting")
            if environment not in LOCAL:
                expect_render_rejected(directory, environment, values, ("server", "config", "repohost"), LOCAL_GIT, "exists only in the local lanes")
                for hostname in ("", "*.example.com", "https://studio.example.com"):
                    expect_render_rejected(directory, environment, values, ("studio", "hostname"), hostname, "Studio")
                for keys in (("server", "config", "studio", "publicOrigin"), ("studio", "config", "SUPERVERSE_STUDIO_PUBLIC_ORIGIN")):
                    expect_render_rejected(directory, environment, values, keys, "https://wrong.example.com", "origins must match")
                for cidrs in ([], ["0.0.0.0/0"]):
                    expect_render_rejected(directory, environment, values, ("ingress", "targetCIDRs"), cidrs, "platform ALB health checks require")
            if environment == "prod":
                expect_render_rejected(directory, environment, values, ("dex", "debugIngress", "enabled"), True, "only available in hosted test/staging")
            # The memory Stream store is per process, and the local Git host belongs to one server Pod.
            expect_render_rejected(directory, environment, values, ("dex", "replicas"), 2, "only with a shared Stream store")
            if environment in LOCAL:
                expect_render_rejected(directory, environment, values, ("server", "replicas"), 2, "Local Kind runs one server Pod")


def check_native_platform(render):
    """Native's Compose file runs platform Temporal, MinIO, Dex and Keycloak with the arguments of Local Kind's (scripts/local_platform.py).

    Only Temporal's database file differs: Native keeps it in the image user's home, which its new volume takes the ownership of.
    Keycloak's issuer and redirect origin are each lane's own, and its secrets come from the lane's private credentials.
    """
    services = yaml.safe_load(NATIVE_COMPOSE.read_text())["services"]
    namespace = names_for("test")["temporal_namespace"]

    def normalized(arguments):
        values = [str(value).replace("${SV2_TEMPORAL_NAMESPACE:?}", namespace) for value in arguments]
        if "--db-filename" in values:
            values[values.index("--db-filename") + 1] = "<database>"
        return values
    for name in ("temporal", "minio"):
        arguments = render.get("StatefulSet", name)["spec"]["template"]["spec"]["containers"][0]["args"]
        render.fail_if(normalized(arguments) != normalized(services[name]["command"]), f"Native {name} arguments differ from Local Kind's")
    postgres = render.get("StatefulSet", "postgres")["spec"]["template"]["spec"]["containers"][0]
    settings = {item["name"]: item.get("value") for item in postgres["env"] if "value" in item}
    render.fail_if({key: services["postgres"]["environment"].get(key) for key in settings} != settings,
                   "Native PostgreSQL database or data directory differs from Local Kind's")
    dex = render.get("Deployment", render.product + "-dex")["spec"]["template"]["spec"]["containers"][0]
    render.fail_if(dex["args"] != services["dex"]["command"] or {item["name"]: item.get("value") for item in dex["env"]} != services["dex"]["environment"],
                   "Native platform Dex arguments or environment differ from Local Kind's")
    keycloak = render.get("Deployment", "keycloak")["spec"]["template"]["spec"]["containers"][0]
    lane_settings = ("KC_HOSTNAME", "SV2_KEYCLOAK_REDIRECT_ORIGIN")
    rendered = {item["name"]: item["value"] for item in keycloak["env"]}
    settings = {key: value for key, value in rendered.items() if key not in lane_settings}
    environment = services["keycloak"]["environment"]
    render.fail_if(keycloak["args"] != services["keycloak"]["command"] or {key: environment.get(key) for key in settings} != settings
                   or not set(lane_settings) <= set(environment), "Native Keycloak arguments or settings differ from Local Kind's")
    render.fail_if(urlsplit(rendered["KC_HOSTNAME"]).port != FORWARDS["keycloak"].local_port, "Local Kind's Keycloak issuer is not its `kind ports` forward")
    print("Static Native platform check passed: Compose runs Temporal, MinIO, PostgreSQL, Dex and Keycloak with Local Kind's arguments")


def catalog(environment):
    return names_for("test" if environment in LOCAL else environment)


def studio_hostname(environment):
    # Test and staging deliberately use a separate reserved product domain; prod preserves the catalog host.
    return {"test": "test.studio.example.com", "staging": "studio.example.com"}.get(environment, catalog(environment)["dns_domain"])


def studio_certificate(environment):
    return STUDIO_CERTIFICATE if environment in ("test", "staging") else CERTIFICATE


def environment_values(environment):
    """The chart inputs Terraform or Kind supplies for one environment, with shape-only images."""
    names = catalog(environment)
    local = environment in LOCAL
    values = {
        "environment": "test" if local else environment, "localKind": local,
        "server": {"image": {"repository": "superverse-v2/server", "digest": SHAPE_DIGEST}},
        "studio": {"image": {"repository": "superverse-v2/studio", "digest": SHAPE_DIGEST}},
        "dex": {"image": {"digest": SHAPE_DIGEST}, "temporalAddress": "temporal:7233"},
        "sandboxProfiles": {"codingProfile": {"enabled": True, "image": {"repository": "superverse-v2/coding-sandbox", "digest": SHAPE_DIGEST}}},
        "nodePools": {"clusterName": names["cluster"], "nodeRoleName": "superverse-v2-node",
                      "gvisorRelease": "release/20260907.0", "gvisorSHA256": "2b162adb35860f598ab2f89b9d752bff2c7ee6175c05d9cc532174a336cfb38c"},
    }
    # The server configuration the Kind deploy renders from its profile, or Terraform from its variables.
    values["server"]["config"] = {"studio": {"publicOrigin": "http://localhost:23000" if local else "https://" + studio_hostname(environment)}}
    if local:
        # The local lanes use the local Git host of the base profile; hosted environments always use GitHub.
        profile = yaml.safe_load(LOCAL_KIND_PROFILE.read_text())
        values["server"]["config"].update({"repohost": profile["repohost"], "localgit": profile["localgit"]})
    else:
        values["server"]["config"]["repohost"] = GITHUB
        # The RDS endpoint Terraform renders from the cloud layer's outputs (terraform/kubernetes/platform.tf).
        values["server"]["config"]["postgres"] = {"host": "superverse-v2-test-postgres.example.us-west-2.rds.amazonaws.com", "port": 5432, "database": "superverse", "sslMode": "require"}
    if local:
        values["localDependencies"] = {"temporalImage": "temporalio/temporal@" + SHAPE_DIGEST, "minioImage": "superverse-v2/minio@" + SHAPE_DIGEST,
                                       "postgresImage": "postgres@" + SHAPE_DIGEST, "keycloakImage": "keycloak@" + SHAPE_DIGEST}
        return values
    domain = studio_hostname(environment)
    values["studio"]["hostname"] = domain
    values["hostedProjects"] = {"dexImage": "docker.io/superdurable/dex-server@" + SHAPE_DIGEST, "certificateArn": CERTIFICATE,
                                "applicationRepository": "000000000000.dkr.ecr.us-west-2.amazonaws.com/" + names["ecr_prefix"] + "/applications"}
    values["ingress"] = {"certificateArn": studio_certificate(environment), "targetCIDRs": ["198.51.100.0/24"]}
    values["network"] = {"kubernetesApiCIDRs": [KUBERNETES_API_CIDR]}
    values["studio"]["config"] = {"SUPERVERSE_STUDIO_PUBLIC_ORIGIN": "https://" + domain}
    if environment in ("test", "staging"):
        values["dex"]["internalClientCIDRs"] = ["10.0.0.0/20"]
        values["dex"]["debugIngress"] = {"enabled": True, "webAuthSecretName": "dex-web-auth", "certificateArn": CERTIFICATE, "subnetIDs": ["subnet-00000000000000000"], "targetCIDRs": ["198.51.100.0/24"]}
    return values


def chart_files(directory, environment, values, label=""):
    path = directory / f"{environment}{label}.json"
    path.write_text(json.dumps(values))
    return (["-f", str(LOCAL_KIND_VALUES)] if environment in LOCAL else []) + ["-f", str(path)]


def helm_template(directory, environment, values, *flags):
    names = catalog(environment)
    return subprocess.run(["helm", "template", names["product"], str(CHART), "--namespace", names["platform_namespace"], "--include-crds",
                           *chart_files(directory, environment, values), *flags], check=True, stdout=subprocess.PIPE, text=True).stdout


def expect_render_rejected(directory, environment, values, keys, value, fragment, *, skip_schema=True):
    """Rendering with `keys` set to `value` must fail with `fragment`; skipping the schema proves the template guard itself."""
    rejected = copy.deepcopy(values)
    target = rejected
    for key in keys[:-1]:
        target = target.setdefault(key, {})
    target[keys[-1]] = value
    names = catalog(environment)
    result = subprocess.run(["helm", "template", names["product"], str(CHART), "--namespace", names["platform_namespace"],
                             *chart_files(directory, environment, rejected, "-rejected"), *(["--skip-schema-validation"] if skip_schema else [])],
                            capture_output=True, text=True)
    if result.returncode == 0 or fragment not in result.stderr:
        raise ValueError(f"{environment}: invalid configuration was not rejected by its guard: {'.'.join(keys)}={value!r}")


def check_debug_variants(directory, environment, values):
    """Static guards only; reserved subnet examples are never acceptance evidence."""
    disabled = copy.deepcopy(values)
    disabled["dex"]["debugIngress"]["enabled"] = False
    Render(environment, helm_template(directory, environment, disabled), debug_enabled=False).check()
    invalid = [
        (("dex", "debugIngress", "subnetIDs"), []),
        (("dex", "debugIngress", "subnetIDs"), ["arbitrary-subnet"]),
        (("dex", "debugIngress", "subnetIDs"), ["subnet-01", "subnet-02"]),
        (("dex", "debugIngress", "targetCIDRs"), ["0.0.0.0/0"]),
        (("dex", "internalClientCIDRs"), []),
    ]
    for keys, value in invalid:
        expect_render_rejected(directory, environment, values, keys, value, "platform Dex")
    for value in ("", "Invalid_name", "a" * 254):
        expect_render_rejected(directory, environment, values, ("dex", "debugIngress", "webAuthSecretName"), value, "platform Dex Web")
        expect_render_rejected(directory, environment, values, ("dex", "debugIngress", "webAuthSecretName"), value, "webAuthSecretName", skip_schema=False)
    print(f"Static debug guards passed: {environment}; disabled API-only and {len(invalid)} rejections")


class Render:
    """One rendered environment, indexed by (kind, namespace, name)."""

    def __init__(self, environment, manifest, *, debug_enabled=None):
        names = catalog(environment)
        self.environment, self.namespace, self.product = environment, names["platform_namespace"], names["product"]
        self.domain = names["dns_domain"]
        self.studio_hostname = studio_hostname(environment)
        self.studio_certificate = studio_certificate(environment)
        self.hosted = environment not in LOCAL
        self.debug = environment in ("test", "staging") if debug_enabled is None else debug_enabled
        self.web = not self.hosted or self.debug
        self.resources = {}
        for document in yaml.safe_load_all(manifest):
            if document is None:
                continue
            metadata = document.get("metadata", {})
            key = (document.get("kind"), metadata.get("namespace", ""), metadata.get("name"))
            self.fail_if(key in self.resources, f"duplicate rendered resource {key}")
            self.resources[key] = document
        self.ingresses = {}

    def fail_if(self, condition, message):
        if condition:
            raise ValueError(f"{self.environment}: {message}")

    def get(self, kind, name):
        return self.get_in(kind, self.namespace, name)

    def get_in(self, kind, namespace, name):
        key = (kind, namespace, name)
        self.fail_if(key not in self.resources, f"required {kind} {namespace}/{name} was not rendered")
        return self.resources[key]

    def check(self):
        self.check_node_capacity()
        self.check_platform_workloads()
        self.check_server_configuration()
        self.check_server_roles()
        self.check_project_gateway()
        self.check_ingresses()
        self.check_coding_profile()
        self.check_credential_read()

    def check_node_capacity(self):
        """Every hosted pool prefers diversified Spot, can fall back, and can consolidate back."""
        pools = {name: resource for (kind, _, name), resource in self.resources.items() if kind == "NodePool"}
        expected = {self.product + "-" + pool for pool in ("platform", "runsc", "live")} if self.hosted else set()
        self.fail_if(set(pools) != expected, "expected all three capacity pools only in hosted environments")
        for name, pool in pools.items():
            requirements = {item["key"]: item for item in pool["spec"]["template"]["spec"]["requirements"]}
            for key, values in {"karpenter.sh/capacity-type": ["spot", "on-demand"], "kubernetes.io/arch": ["arm64"],
                                "karpenter.k8s.aws/instance-category": ["c", "m", "r", "t"],
                                "karpenter.k8s.aws/instance-cpu": ["2", "4", "8"]}.items():
                self.fail_if(requirements[key] != {"key": key, "operator": "In", "values": values}, f"{name}: capacity diversity differs")
            self.fail_if(requirements["karpenter.k8s.aws/instance-memory"] != {"key": "karpenter.k8s.aws/instance-memory", "operator": "Gt", "values": ["3999" if name.endswith("-platform") else "7999"]},
                         f"{name}: instance memory floor differs")
            self.fail_if("topology.kubernetes.io/zone" in requirements, f"{name}: capacity must use all discovered cluster zones")
            self.fail_if(pool["spec"]["disruption"] != {"consolidationPolicy": "WhenEmptyOrUnderutilized", "consolidateAfter": "5m"},
                         f"{name}: fallback must be eligible for automatic cheaper Spot replacement")

    def check_platform_workloads(self):
        """Server, Studio and Dex run digest-pinned images behind their fixed Services and origins."""
        for component, ports in {"server": {8080, 8082, CREDENTIAL_READ_PORT, SLACK_EVENTS_PORT, REMOTE_AGENT_PORT} | (set() if self.hosted else {GIT_PORT}), "studio": {3000}, "dex": {8801, 8802} if self.web else {8801}}.items():
            name = self.product + "-" + component
            containers = self.get("Deployment", name)["spec"]["template"]["spec"]["containers"]
            self.fail_if([item["name"] for item in containers] != ([component, "web-proxy"] if component == "dex" and self.debug else [component]), f"unexpected containers for {name}")
            self.fail_if("@sha256:" not in containers[0]["image"], f"{name} image is not digest-qualified")
            if component == "dex":
                self.check_dex(containers)
            service = self.get("Service", name)
            self.fail_if(service["spec"]["selector"] != {"app": name} or {port["port"] for port in service["spec"]["ports"]} != ports, f"unexpected Service target/ports for {name}")
            if component == "dex" and self.web:
                self.fail_if(service["spec"].get("type", "ClusterIP") != "ClusterIP" or
                             [port for port in service["spec"]["ports"] if port["port"] == 8802] != [{"name": "web", "port": 8802, "targetPort": "web-proxy" if self.debug else "web"}],
                             "Dex Web Service target or private Service type changed")
        origin = "https://" + self.studio_hostname if self.hosted else "http://localhost:23000"
        studio_origins = [item.get("value") for item in self.get("Deployment", self.product + "-studio")["spec"]["template"]["spec"]["containers"][0]["env"]
                          if item["name"] == "SUPERVERSE_STUDIO_PUBLIC_ORIGIN"]
        self.fail_if(studio_origins != [origin] or self.server_configuration().get("studio", {}).get("publicOrigin") != origin,
                     "Go and Studio OAuth public origins must match their fixed environment")
        self.fail_if(any(kind == "Service" and name == "superverse-server-worker" for kind, _, name in self.resources), "pull Workers have no callback Service")
        self.get("ServiceAccount", "superverse-server")
        server = self.get("Deployment", self.product + "-server")["spec"]
        self.fail_if(server["selector"]["matchLabels"] != {"app": self.product + "-server"} or server["replicas"] != 1, "unexpected server replica/Pod selector")
        self.fail_if(server["template"]["spec"]["containers"][0].get("lifecycle") != {"preStop": {"sleep": {"seconds": 15}}}, "server endpoint removal sleep must be 15 seconds")
        self.fail_if(server["template"]["spec"]["terminationGracePeriodSeconds"] != 120, "server grace must cover preStop, pull drain and listener shutdown")
        self.fail_if(server.get("strategy") != {"type": "RollingUpdate", "rollingUpdate": {"maxUnavailable": 0, "maxSurge": 1}}, "platform server must start a ready replacement before terminating the old Pod")
        dex = self.get("Deployment", self.product + "-dex")["spec"]
        self.fail_if(dex["replicas"] != 1 or dex.get("strategy") != {"type": "Recreate"}, "platform Dex must run one replica replaced by Recreate")
        # Studio keeps no state between requests: Local Kind runs two Pods, which a PodDisruptionBudget keeps serving.
        studio = self.get("Deployment", self.product + "-studio")["spec"]
        replicas = 1 if self.hosted else 2
        budget = self.resources.get(("PodDisruptionBudget", self.namespace, self.product + "-studio"))
        self.fail_if(studio["replicas"] != replicas, f"Studio must render {replicas} replicas")
        self.fail_if((budget is None) != (replicas == 1) or (budget is not None and budget["spec"] != {"minAvailable": 1, "selector": {"matchLabels": {"app": self.product + "-studio"}}}),
                     "a Studio of several replicas, and only it, has the PodDisruptionBudget that keeps one Pod serving")

    def check_credential_read(self):
        """Project applications and Sandboxes reach only the credential read endpoint, at the address configurations carry.

        Local Kind runs no platform policies, so its companion policy keeps the server's other ports open as before.
        """
        product, profile = self.product, CODING_PROFILE
        managed = {"app.kubernetes.io/managed-by": product}
        expected = {"podSelector": {"matchLabels": {"app": product + "-server"}}, "policyTypes": ["Ingress"], "ingress": [{
            "from": [{"namespaceSelector": {"matchLabels": managed, "matchExpressions": [{"key": "superdurable.io/project-id", "operator": "Exists"}]},
                      "podSelector": {"matchLabels": {"superdurable.io/component": "application"}}},
                     {"namespaceSelector": {"matchLabels": managed, "matchExpressions": [{"key": "superdurable.io/scope", "operator": "In", "values": ["preview"]}]},
                      "podSelector": {"matchLabels": {"superdurable.io/sandbox-profile": profile}}}],
            "ports": [{"port": CREDENTIAL_READ_PORT, "protocol": "TCP"}]}]}
        self.fail_if(self.get("NetworkPolicy", product + "-credential-read")["spec"] != expected, "the credential read policy admits other peers or ports")
        companion = [(kind, name) for (kind, _, name) in self.resources if kind == "NetworkPolicy" and name == product + "-server-local"]
        if self.hosted:
            self.fail_if(companion, "only Local Kind keeps the server's other ports open with a companion policy")
        else:
            ports = sorted(port["port"] for port in self.get("NetworkPolicy", product + "-server-local")["spec"]["ingress"][0]["ports"])
            self.fail_if(ports != sorted([8080, 8082, SLACK_EVENTS_PORT, REMOTE_AGENT_PORT, GIT_PORT]), "the Local Kind companion policy must keep exactly the server's other ports open")
        connections = self.server_configuration().get("connections", {})
        self.fail_if(connections.get("readListen") != f":{CREDENTIAL_READ_PORT}" or connections.get("readURL") !=
                     f"http://{product}-server.{self.namespace}.svc.cluster.local:{CREDENTIAL_READ_PORT}/internal/connection-credentials/read",
                     "the server's credential read listener or the address configurations carry differs from its Service")

    def server_configuration(self):
        """The server configuration document the chart renders, which the Pod mounts as its only configuration file."""
        configmap = self.get("ConfigMap", self.product + "-server-config")
        self.fail_if(set(configmap["data"]) != {"server.yaml"}, "the server configuration ConfigMap must hold exactly server.yaml")
        return yaml.safe_load(configmap["data"]["server.yaml"])

    def check_server_configuration(self):
        """The server reads one mounted file, its configuration carries no secret, and the chart fixes what it owns."""
        pod = self.get("Deployment", self.product + "-server")["spec"]["template"]
        container = pod["spec"]["containers"][0]
        secret_keys = {item["valueFrom"]["secretKeyRef"]["key"] for item in container["env"]
                       if "secretKeyRef" in item.get("valueFrom", {})}
        self.fail_if(secret_keys != DEPLOYED_SECRET_KEYS or bool(container.get("envFrom")),
                     f"server secret references differ: missing={sorted(DEPLOYED_SECRET_KEYS - secret_keys)}, "
                     f"extra={sorted(secret_keys - DEPLOYED_SECRET_KEYS)}; envFrom is forbidden")
        self.fail_if([item for item in container["env"] if "valueFrom" not in item] !=
                     [{"name": "SUPERVERSE_CONFIG_FILE", "value": "/etc/superverse/server.yaml"}],
                     "the server container must set only its configuration file and approved secret references; every other setting is in the configuration document")
        self.fail_if({"name": "server-config", "mountPath": "/etc/superverse", "readOnly": True} not in container["volumeMounts"] or
                     {"name": "server-config", "configMap": {"name": self.product + "-server-config", "items": [{"key": "server.yaml", "path": "server.yaml"}]}} not in pod["spec"]["volumes"],
                     "the server configuration must be mounted read-only from its ConfigMap")
        configuration = self.server_configuration()
        self.fail_if(configuration.get("environment") != ("test" if self.environment in LOCAL else self.environment) or configuration.get("localKind") is not (self.environment in LOCAL),
                     "the chart must fix the environment and the Local Kind flag of the server configuration")
        self.fail_if(configuration.get("dex") != {"serverAddress": self.product + "-dex:8801", "pull": {"maxConcurrentTasks": 256, "idleStreamsPerQueue": 1, "stopDrainTimeout": "60s"}},
                     "server configuration must name platform Dex and bounded pull options")
        self.fail_if(configuration.get("server") != {"grpcAddress": ":8080", "healthAddress": ":8082", "blobCacheDir": "/var/cache/superverse/dex-blobs"},
                     "the server configuration must bind the Pod's ports and its cache volume")
        self.fail_if(not pod["metadata"]["annotations"].get("checksum/server-config"), "the server Pod must roll when its rendered configuration changes")
        # Only the local lanes run PostgreSQL beside the server, so only they may skip TLS; the hosted one is RDS, which requires it.
        postgres = configuration.get("postgres", {})
        self.fail_if(self.hosted and (not postgres.get("host") or not postgres.get("database") or postgres.get("sslMode") not in ("require", "verify-full")),
                     "a hosted server connects to its database host over TLS: postgres.host, database and sslMode require or verify-full")
        self.check_local_git(pod, container, configuration)
        self.check_migration(pod)
        keys = set()

        def collect(node):
            for key, value in (node.items() if isinstance(node, dict) else ()):
                keys.add(key.lower())
                collect(value)
        collect(configuration)
        self.fail_if(any(word in key for key in keys for word in ("secret", "password", "privatekey", "apikey", "signingkey")),
                     "the server configuration must not carry a secret setting")

    def check_migration(self, pod):
        """The init container migrates the database with the server's image and configuration, and alone holds the owner's password."""
        migrate = pod["spec"].get("initContainers", [])
        server = pod["spec"]["containers"][0]
        self.fail_if([item["name"] for item in migrate] != ["migrate"] or migrate[0]["image"] != server["image"] or migrate[0].get("args") != ["migrate"],
                     "the server Pod must run `superverse-server migrate` with its own image in its one init container")
        owner = {"name": "SUPERVERSE_POSTGRES_OWNER_PASSWORD", "valueFrom": {"secretKeyRef": {"name": self.product + "-postgres", "key": "SUPERVERSE_POSTGRES_OWNER_PASSWORD"}}}
        self.fail_if(migrate[0]["env"] != [server["env"][0], owner] or migrate[0]["volumeMounts"] != [{"name": "server-config", "mountPath": "/etc/superverse", "readOnly": True}],
                     "the migration reads the server configuration and only the owner's password")

    def check_local_git(self, pod, container, configuration):
        """Only Local Kind carries the local Git host's port and its hostPath volume, and the configuration agrees with the Pod."""
        ports = [port for port in container["ports"] if port["name"] == "git-http"]
        mounts = [mount for mount in container["volumeMounts"] if mount["name"] == "git-repositories"]
        volumes = [volume for volume in pod["spec"]["volumes"] if volume["name"] == "git-repositories"]
        if self.hosted:
            self.fail_if(ports or mounts or volumes or configuration.get("repohost") != GITHUB, "a hosted server uses GitHub and has no local Git host port or volume")
            return
        self.fail_if(ports != [{"name": "git-http", "containerPort": GIT_PORT}] or mounts != [{"name": "git-repositories", "mountPath": GIT_MOUNT}] or
                     volumes != [{"name": "git-repositories", "hostPath": {"path": GIT_NODE_PATH, "type": "Directory"}}],
                     "the local server must expose git-http and mount the node's repository directory, which must exist")
        localgit = configuration.get("localgit", {})
        self.fail_if(configuration.get("repohost") != LOCAL_GIT or localgit.get("dir") != GIT_MOUNT or localgit.get("listen") != f"0.0.0.0:{GIT_PORT}" or
                     localgit.get("cloneBaseURL") != f"http://{self.product}-server.{self.namespace}.svc.cluster.local:{GIT_PORT}",
                     "the local Git host's directory, address and clone URL must be those of the server Pod and Service")

    def check_pull_capacity(self):
        """The Go owner, local Dex generators and rollout stream budget must agree."""
        source = (ROOT / "cmd/superverse-server/config/config.go").read_text()
        cap = int(re.search(r"PlatformDexWorkerStreamCap\s+int\s*=\s*(\d+)", source).group(1))
        matching = dex_config({"access": "shape", "secret": "shape"})["matching"]
        self.fail_if(matching != {"maxConcurrentTasksCap": cap, "maxStreams": 100000},
                     "local platform Dex matching limits must match the typed Go stream cap")
        server = self.get("Deployment", self.product + "-server")["spec"]
        surge = server["strategy"]["rollingUpdate"]["maxSurge"]
        self.fail_if((server["replicas"] + surge) * cap > matching["maxStreams"],
                     "server replicas plus rollout surge exceed one matching instance's stream budget")

    def check_dex(self, containers):
        """Dex Web is served only locally or behind the canonical private debug proxy."""
        self.check_pull_capacity()
        dex = containers[0]
        ports = [{"name": "grpc", "containerPort": 8801}] + ([{"name": "web", "containerPort": 8802}] if self.web else [])
        self.fail_if(dex.get("args") != ["--services", "api,interpreter,matching,web" if self.web else "api,interpreter,matching"] or dex.get("ports") != ports, "unexpected platform Dex components/ports")
        pod = self.get("Deployment", self.product + "-dex")["spec"]["template"]["spec"]
        auth_volumes = [v for v in pod["volumes"] if v["name"] == "web-proxy-auth"]
        if not self.debug:
            self.fail_if(auth_volumes or ("ConfigMap", self.namespace, self.product + "-dex-web-proxy") in self.resources,
                         "disabled hosted Web must not mount auth or render a proxy")
            return
        proxy = containers[1]
        self.fail_if(proxy["image"] != DEBUG_PROXY_IMAGE or proxy["ports"] != [{"name": "web-proxy", "containerPort": 8083}], "unexpected debug proxy image or port")
        security = proxy["securityContext"]
        self.fail_if(not security["runAsNonRoot"] or not security["readOnlyRootFilesystem"] or security["allowPrivilegeEscalation"] or security["capabilities"] != {"drop": ["ALL"]},
                     "debug proxy must remain unprivileged and read-only")
        self.fail_if(auth_volumes != [{"name": "web-proxy-auth", "secret": {"secretName": "dex-web-auth", "defaultMode": 0o440,
                     "items": [{"key": "htpasswd", "path": "htpasswd"}]}}] or
                     {"name": "web-proxy-auth", "mountPath": "/etc/nginx/auth", "readOnly": True} not in proxy["volumeMounts"],
                     "Web auth must mount the operator Secret directory read-only without subPath so rotations refresh")
        self.fail_if(("Secret", self.namespace, "dex-web-auth") in self.resources, "Helm must never own the auth Secret or hash")
        self.fail_if(proxy["readinessProbe"]["httpGet"] != {"path": "/readyz", "port": "web-proxy"}, "proxy readiness must use the unauthenticated exact health path")
        config = self.get("ConfigMap", self.product + "-dex-web-proxy")["data"]["nginx.conf"]
        self.fail_if('    auth_basic "Platform Dex Web";\n    auth_basic_user_file /etc/nginx/auth/htpasswd;\n' not in config or
                     '    location = /readyz {\n      auth_basic off;\n      return 200;\n    }\n' not in config or
                     config.count("auth_basic off;") != 1 or config.count("location ") != 2 or
                     'proxy_set_header Authorization "";' not in config,
                     "all Web paths except exact /readyz must require Basic Auth without forwarding the credentials")
        host = "dex-web." + self.domain
        canonical = {"Host": host, "X-Forwarded-Prefix": "/", "X-Forwarded-Proto": "https", "X-Forwarded-Host": host, "X-Dex-Web-Embedded": "false",
                     "X-Dex-Web-CSRF-Token": '""', "X-Dex-Connector-OAuth-Redirect-URI": '""', "X-Dex-Work-Queue-Permissions": '""', "X-Dex-Project-Actor": '""', "Forwarded": '""'}
        self.fail_if(any(config.count("proxy_set_header " + key + " " + value + ";") != 1 for key, value in canonical.items()) or "$http_" in config,
                     "proxy must replace all browser embedding metadata with canonical root values")
        self.fail_if("proxy_pass http://127.0.0.1:8802;" not in config or "access_log off;" not in config or "proxy_buffering off;" not in config,
                     "proxy upstream, logging and buffering boundaries changed")

    def check_server_roles(self):
        """The server's namespaced roles read Pods and only the project runtime reads Pod logs; nothing forwards to a Pod."""
        for role in (self.product + "-session-manager", self.product + "-project-runtime"):
            rules = self.resources[("ClusterRole", "", role)]["rules"]
            forward_rules = [rule for rule in rules if any(resource in ("pods", "pods/portforward", "replicasets") for resource in rule.get("resources", []))]
            # Both roles read Pods in every environment: the session manager waits
            # until a Sandbox's Pods are gone, and the project runtime finds the
            # Live application Pod whose log the Agent reads.
            self.fail_if(forward_rules != POD_READ_RULES, f"unexpected Pod read or forwarding permissions in {role}")
            log_rules = [rule for rule in rules if "pods/log" in rule.get("resources", [])]
            expected_log = [{"apiGroups": [""], "resources": ["pods/log"], "verbs": ["get"]}] if role.endswith("-project-runtime") else []
            self.fail_if(log_rules != expected_log, f"unexpected Pod log permissions in {role}")

    def check_project_gateway(self):
        """Every project route attaches to the one Gateway, whose proxies a ClusterIP Service fronts on every lane.

        Hosted, one wildcard Ingress forwards every project host to that Service as the last rule of the platform ALB group,
        and no Ingress of the group sets a group-wide ALB setting that another one sets differently: one conflict stops the
        controller from reconciling the whole group, Studio included. Local Kind has no Ingress at all.
        """
        namespace, domain = self.product + "-gateway", ("*." + self.environment + ".dexai.dev") if self.hosted else "*.localhost"
        proxy = self.get_in("EnvoyProxy", namespace, "projects")["spec"]["provider"]["kubernetes"]
        self.fail_if(proxy["envoyService"] != {"name": "projects-gateway", "type": "ClusterIP"},
                     "the project Gateway's proxies need a fixed ClusterIP Service: a LoadBalancer Service creates a cloud load balancer")
        self.fail_if(proxy["envoyDeployment"]["replicas"] != (2 if self.hosted else 1) or ("envoyPDB" in proxy) != self.hosted,
                     "hosted project Gateway proxies run two replicas under a disruption budget")
        gateway = self.get_in("Gateway", namespace, "projects")["spec"]
        self.fail_if(gateway["gatewayClassName"] != self.product + "-projects" or gateway["listeners"] != [{
            "name": "projects", "protocol": "HTTP", "port": 80, "hostname": domain,
            "allowedRoutes": {"kinds": [{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute"}], "namespaces": {"from": "Selector", "selector": {
                "matchLabels": {"app.kubernetes.io/managed-by": self.product},
                "matchExpressions": [{"key": "superdurable.io/project-id", "operator": "Exists"}]}}}}],
            "the project Gateway must admit only HTTPRoutes of project namespaces under the project domain")
        ingresses = {(scope, name): document for (kind, scope, name), document in self.resources.items() if kind == "Ingress"}
        if not self.hosted:
            self.fail_if(ingresses or ("Deployment", self.namespace, self.product + "-preview-gateway") in self.resources,
                         "Local Kind routes every host through the project Gateway: no Ingress and no Traefik")
            return
        # The cluster's only reader of every Secret reaches only the Kubernetes API, and only its proxies reach it.
        self.fail_if(self.get_in("NetworkPolicy", namespace, "envoy-gateway-controller")["spec"] != {
            "podSelector": {"matchLabels": {"control-plane": "envoy-gateway"}}, "policyTypes": ["Ingress", "Egress"],
            "ingress": [{"from": [{"podSelector": {"matchLabels": {"app.kubernetes.io/name": "envoy", "app.kubernetes.io/managed-by": "envoy-gateway"}}}],
                         "ports": [{"port": 18000, "protocol": "TCP"}]}],
            "egress": [{"to": [{"ipBlock": {"cidr": KUBERNETES_API_CIDR}}]}]},
            "the Envoy Gateway controller must reach only the Kubernetes API and admit only its proxies' xDS")
        wildcard = self.get_in("Ingress", namespace, self.product + "-projects")
        annotations = wildcard["metadata"]["annotations"]
        rule = {"host": domain, "http": {"paths": [{"path": "/", "pathType": "Prefix", "backend": {"service": {"name": "projects-gateway", "port": {"number": 80}}}}]}}
        self.fail_if(wildcard["spec"]["ingressClassName"] != "alb" or wildcard["spec"]["rules"] != [rule] or
                     annotations["alb.ingress.kubernetes.io/group.name"] != self.product + "-platform" or
                     annotations["alb.ingress.kubernetes.io/group.order"] != "1000" or
                     annotations["alb.ingress.kubernetes.io/certificate-arn"] != CERTIFICATE or
                     annotations["alb.ingress.kubernetes.io/healthcheck-port"] != "19003" or annotations["alb.ingress.kubernetes.io/healthcheck-path"] != "/ready" or
                     annotations["external-dns.alpha.kubernetes.io/hostname"] != domain,
                     "the project wildcard must be the platform ALB group's last rule, health-checking the Envoy proxies")
        orders = [document["metadata"]["annotations"].get("alb.ingress.kubernetes.io/group.order") for document in ingresses.values()]
        self.fail_if(max(int(order) for order in orders if order) != 1000 or orders.count("1000") != 1, "only the project wildcard may be the platform ALB's last rule")
        for key in GROUP_WIDE_ALB_ANNOTATIONS:
            values = {document["metadata"]["annotations"][key] for document in ingresses.values() if key in document["metadata"]["annotations"]}
            self.fail_if(len(values) > 1, f"the platform ALB group's Ingresses disagree on {key}: {sorted(values)}")

    def check_ingresses(self):
        """Hosted Studio and, in test/staging, the authenticated Web debug surface are the only Ingresses, each on its fixed host."""
        if self.hosted:
            self.ingresses[self.product + "-studio"] = (self.studio_hostname, self.product + "-studio", 3000, None)
        if self.debug:
            self.ingresses[self.product + "-platform-dex-web"] = ("dex-web." + self.domain, self.product + "-dex", 8802, "HTTP1")
            self.fail_if(self.get("NetworkPolicy", self.product + "-platform-dex-debug")["spec"] != {
                "podSelector": {"matchLabels": {"app": self.product + "-dex"}}, "policyTypes": ["Ingress"],
                "ingress": [
                    {"from": [{"ipBlock": {"cidr": "10.0.0.0/20"}}], "ports": [{"port": 8801, "protocol": "TCP"}]},
                    {"from": [{"ipBlock": {"cidr": "198.51.100.0/24"}}], "ports": [{"port": 8083, "protocol": "TCP"}]},
                ]}, "internal gRPC and ALB admission must remain separate")
        slack_events = self.product + "-slack-events"
        expected = set(self.ingresses) | ({slack_events} if self.hosted else set())
        actual = {name for kind, scope, name in self.resources if kind == "Ingress" and scope == self.namespace}
        self.fail_if(actual != expected, f"Ingress names {sorted(actual)} differ from {sorted(expected)}")
        if self.hosted:
            # The two public server endpoints reuse the existing Studio host and ALB.
            ingress = self.get("Ingress", slack_events)
            annotations = ingress["metadata"]["annotations"]
            rule = {"host": self.studio_hostname, "http": {"paths": [{"path": "/api/slack/events", "pathType": "Exact",
                                                            "backend": {"service": {"name": self.product + "-server", "port": {"number": SLACK_EVENTS_PORT}}}},
                {"path": "/remote-agent/", "pathType": "Prefix", "backend": {"service": {"name": self.product + "-server", "port": {"number": REMOTE_AGENT_PORT}}}}]}}
            self.fail_if(ingress["spec"]["ingressClassName"] != "alb" or ingress["spec"]["rules"] != [rule] or
                         annotations["alb.ingress.kubernetes.io/group.name"] != self.product + "-platform" or
                         json.loads(annotations["alb.ingress.kubernetes.io/listen-ports"]) != [{"HTTPS": 443}] or
                         annotations["alb.ingress.kubernetes.io/certificate-arn"] != self.studio_certificate or
                         annotations["alb.ingress.kubernetes.io/healthcheck-port"] != "8082" or annotations["alb.ingress.kubernetes.io/healthcheck-path"] != "/readyz",
                         "the shared server Ingress must route exactly Slack events and the remote-agent prefix on the platform ALB")
            # Slack's exact path comes before Studio's "/" prefix on the shared ALB.
            self.fail_if(annotations.get("alb.ingress.kubernetes.io/group.order") != "10" or
                         self.get("Ingress", self.product + "-studio")["metadata"]["annotations"].get("alb.ingress.kubernetes.io/group.order") != "20",
                         "the Slack events Ingress must be ordered before Studio's on the platform ALB")
            # The ALB's health checks of the events target reach the server's health port from the ALB subnets only.
            self.fail_if(self.get("NetworkPolicy", self.product + "-slack-events-health")["spec"] != {
                "podSelector": {"matchLabels": {"app": self.product + "-server"}}, "policyTypes": ["Ingress"],
                "ingress": [{"from": [{"ipBlock": {"cidr": "198.51.100.0/24"}}], "ports": [{"port": 8082, "protocol": "TCP"}]}]},
                "the platform ALB's health checks must reach the server's health port from the ALB subnets only")
        if self.hosted:
            studio = self.get("Ingress", self.product + "-studio")
            self.fail_if(studio["metadata"]["annotations"]["external-dns.alpha.kubernetes.io/hostname"] != self.studio_hostname,
                         "external-dns must publish only the exact Studio hostname")
        for name, (host, service, port, protocol) in self.ingresses.items():
            ingress = self.get("Ingress", name)
            annotations = ingress["metadata"]["annotations"]
            rule = {"host": host, "http": {"paths": [{"path": "/", "pathType": "Prefix", "backend": {"service": {"name": service, "port": {"number": port}}}}]}}
            self.fail_if(ingress["spec"]["ingressClassName"] != "alb" or ingress["spec"]["rules"] != [rule], f"unexpected Ingress host/backend for {name}")
            self.fail_if(json.loads(annotations["alb.ingress.kubernetes.io/listen-ports"]) != [{"HTTPS": 443}] or annotations["alb.ingress.kubernetes.io/certificate-arn"] != (CERTIFICATE if protocol else self.studio_certificate),
                         f"incorrect HTTPS/TLS certificate ownership for {name}")
            self.fail_if(annotations["alb.ingress.kubernetes.io/group.name"] != self.product + "-platform", f"{name} is not on the platform's one ALB")
            self.fail_if(protocol and (
                annotations["alb.ingress.kubernetes.io/backend-protocol-version"] != protocol or
                annotations["alb.ingress.kubernetes.io/inbound-cidrs"] != "0.0.0.0/0" or
                annotations["alb.ingress.kubernetes.io/subnets"] != "subnet-00000000000000000" or
                annotations["alb.ingress.kubernetes.io/ip-address-type"] != "ipv4" or
                annotations["alb.ingress.kubernetes.io/healthcheck-path"] != ("/readyz" if protocol == "HTTP1" else "/AWS.ALB/healthcheck")
            ), f"unexpected public debug protocol/subnet boundary for {name}")

    def check_coding_profile(self):
        """One immutable, retained profile that the server selects, with intact baseline hashes and admission bindings."""
        profiles = [resource for (kind, _, name), resource in self.resources.items() if kind == "ConfigMap" and name.startswith("coding-sandbox-profile-")]
        self.fail_if(len(profiles) != 1, "expected one immutable coding profile")
        profile = profiles[0]
        metadata = json.loads(profile["data"]["contract.json"])
        self.fail_if(metadata.get("profile") != CODING_PROFILE or metadata.get("sandboxImageContractRevision") != 8 or not profile["metadata"]["name"].startswith(
            "coding-sandbox-profile-" + CODING_PROFILE + "-"), "the coding profile is not coding-v4 with image contract revision 8")
        self.fail_if(metadata.get("metadataRevision") != 10 or "databaseIdentifiers" in metadata or
                     metadata.get("ownerIdentifiers") != ["namespaceName", "namespaceUid", "sandboxName", "sandboxUid", "workspacePvcName", "workspacePvcUid", "profileVersion"],
                     "profile owner metadata revision differs")
        self.fail_if(not profile.get("immutable") or profile["metadata"].get("annotations", {}).get("helm.sh/resource-policy") != "keep", "admitted Session profile retention changed")
        self.fail_if(self.server_configuration().get("sandbox", {}).get("profile") != profile["metadata"]["name"],
                     "Server and immutable profile content identities differ")
        for baseline in metadata["namespaceBaseline"]["resources"]:
            self.fail_if(hashlib.sha256(profile["data"][baseline["key"]].encode()).hexdigest() != baseline["sha256"], f"coding baseline payload hash differs for {baseline['key']}")
        ingress = yaml.safe_load(profile["data"]["baseline-network-policy-superverse-v2-ingress.yaml"])["spec"]["ingress"][0]
        peer = ingress["from"][0]
        self.fail_if(peer["namespaceSelector"] != {"matchLabels": {"kubernetes.io/metadata.name": self.namespace}} or
                     peer["podSelector"] != {"matchLabels": {"app": self.product + "-server"}}, "coding ingress baseline does not target the catalog platform server")
        self.fail_if(sorted(port["port"] for port in ingress["ports"]) != sorted(SANDBOX_PLATFORM_PORTS), "the platform server must reach only sandboxd and the Preview Dex")
        egress = yaml.safe_load(profile["data"]["baseline-network-policy-sandbox-egress.yaml"])["spec"]["egress"]
        server_rules = [rule for rule in egress if rule["to"][0].get("podSelector") == peer["podSelector"]]
        self.fail_if(server_rules != [{"to": [peer], "ports": [{"protocol": "TCP", "port": CREDENTIAL_READ_PORT}]}],
                     "a Sandbox must reach the platform server only on the credential read endpoint")
        volume_claim = yaml.safe_load(profile["data"]["pvc.yaml"])["spec"]
        self.fail_if(volume_claim["accessModes"] != ["ReadWriteOncePod" if self.hosted else "ReadWriteOnce"], "the workspace volume must be single-writer where the storage enforces it")
        self.fail_if(volume_claim["resources"]["requests"]["storage"] != "1Gi", "the default workspace claim must request 1 GiB")
        pod = yaml.safe_load(profile["data"]["sandbox.yaml"])["spec"]["podTemplate"]["spec"]
        self.check_coding_pod(pod)
        identity_token = {"name": "eks-pod-identity-token", "projected": {"defaultMode": 420, "sources": [{"serviceAccountToken": {
            "audience": "pods.eks.amazonaws.com", "expirationSeconds": 3600, "path": "eks-pod-identity-token"}}]}}
        self.fail_if([volume for volume in pod["volumes"] if "projected" in volume] != ([identity_token] if self.hosted else []), "unexpected scoped application identity projection")
        rendered = json.dumps(list(self.resources.values()))
        self.fail_if("allow-preview-worker" in rendered or catalog(self.environment)["workload_prefix"] + "-preview-" in rendered,
                     "per-Session Preview namespaces and Worker policies are gone with coding-v4")
        if self.hosted:
            self.check_hosted_admission(pod)

    def check_coding_pod(self, pod):
        """coding-v4: tini as PID 1, the Preview engine state on the workspace volume, its preStop stop and the Preview Dex port."""
        container = pod["containers"][0]
        self.fail_if(container["resources"] != {"requests": {"cpu": "100m", "memory": "256Mi", "ephemeral-storage": "1Gi"},
                                                "limits": {"cpu": "1", "memory": "2Gi", "ephemeral-storage": "8Gi"}},
                     "the coding Sandbox must request 256 MiB without changing its CPU or resource limits")
        self.fail_if(pod["terminationGracePeriodSeconds"] != 45 or container["command"] != ["/usr/bin/tini", "--", "/usr/local/bin/sandboxd"],
                     "the Sandbox must run sandboxd under tini with the preStop grace period")
        self.fail_if(container["lifecycle"] != {"preStop": {"exec": {"command": ["/usr/local/bin/superverse-previewctl", "shutdown", "--timeout", "30s"]}}},
                     "the Sandbox preStop hook must stop the Preview engine")
        self.fail_if(container["env"] != [{"name": "HOME", "value": "/workspace"}, {"name": "TMPDIR", "value": "/tmp"},
                                          {"name": "SUPERVERSE_PREVIEW_STATE_DIR", "value": PREVIEW_STATE_DIRECTORY},
                                          {"name": "SUPERVERSE_POD_UID", "valueFrom": {"fieldRef": {"fieldPath": "metadata.uid"}}}],
                     "the Sandbox environment differs from coding-v4")
        self.fail_if({(port["name"], port["containerPort"]) for port in container["ports"]} != {("sandboxd-rest", 8080), ("sandboxd-grpc", 9090), ("preview-dex", 8801)},
                     "the Sandbox container ports differ from coding-v4")
        mounts = [(mount["name"], mount["mountPath"], mount.get("subPath")) for mount in container["volumeMounts"] if mount["name"] != "eks-pod-identity-token"]
        self.fail_if(mounts != [("workspace", "/workspace", "repository"), ("workspace", PREVIEW_STATE_DIRECTORY, "preview-state"), ("tmp", "/tmp", None)],
                     "the workspace volume must hold the repository and the Preview state in separate subPaths")

    def check_hosted_admission(self, sandbox_pod):
        """Hosted Sandbox admission binds the profile's exact image and coding-v4 shape; the platform server reaches only sandboxd and the Preview Dex."""
        sandbox_policy = self.resources[("ValidatingAdmissionPolicy", "", self.product + "-project-session-sandbox")]
        expressions = "\n".join(item["expression"] for item in sandbox_policy["spec"]["validations"])
        image_binding = "variables.container.image == " + json.dumps(sandbox_pod["containers"][0]["image"])
        self.fail_if(image_binding not in expressions, "Sandbox admission does not bind the immutable catalog image")
        self.fail_if(any(fragment not in expressions for fragment in (
            "['/usr/bin/tini', '--', '/usr/local/bin/sandboxd']", "'/usr/local/bin/superverse-previewctl', 'shutdown', '--timeout', '30s'",
            "m.subPath == 'preview-state'", "e.name == 'SUPERVERSE_POD_UID'", "p.name == 'preview-dex'")),
                     "Sandbox admission does not bind the coding-v4 command, preStop, Preview state mount, Pod UID and Preview Dex port")
        coordinator = self.get("NetworkPolicy", self.product + "-project-coordinator")["spec"]["egress"]
        sandbox_rule = [rule for rule in coordinator if rule["to"][0]["podSelector"]["matchLabels"].get("superdurable.io/sandbox-profile") == CODING_PROFILE]
        self.fail_if(len(sandbox_rule) != 1 or sorted(port["port"] for port in sandbox_rule[0]["ports"]) != sorted(SANDBOX_PLATFORM_PORTS),
                     "the platform server egress to Sandboxes must be exactly sandboxd and the Preview Dex")


if __name__ == "__main__":
    main()
