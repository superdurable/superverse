# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The Native lane's platform services: one Docker Compose project of compose.yaml.

Platform Temporal, MinIO, PostgreSQL, Dex and Keycloak run with the pinned images and the configuration of Local Kind,
beside the gateway and the local registry. The launcher passes every value, secrets included, through the environment of the Compose command,
never its arguments, and never prints Compose output, which can quote the interpolated configuration.
"""
import json
import os
from pathlib import Path
import socket
import subprocess
import urllib.request

import build_inputs
from commands import run, wait_until
from local_platform import POSTGRES_DATABASE, POSTGRES_SUPERUSER, READ_MODEL_STORES, create_buckets, dex_config, engine_roles_sql, keycloak_secret_environment, postgres_roles_sql
from native.callback import dex_answers, dexcli
from native.lane import engine_postgres_credentials, keycloak_credentials, platform_credentials, postgres_credentials
from operator_errors import require
from paths import ROOT
from pins import LOCK, local_dependency_image, local_image, names_for

COMPOSE_FILE = Path(__file__).with_name("compose.yaml")
SERVICES = ("temporal", "minio", "postgres", "dex", "keycloak", "gateway", "registry")
# The host settings the Docker CLI needs to reach the same engine as this shell.
DOCKER_SETTINGS = ("PATH", "HOME", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG")


def dex_image():
    """The platform Dex release of source-lock.json, which the chart pins too (scripts/check-dex-versions.py)."""
    return "docker.io/superdurable/dex-server:v" + LOCK["dex"]["server"] + "@" + LOCK["dex"]["serverImageDigest"]


def images():
    """Every platform service's digest-pinned image for this machine's architecture."""
    architecture = build_inputs.architecture()
    return {"temporal": local_dependency_image("temporal", architecture), "minio": local_dependency_image("minio", architecture),
            "postgres": local_image("postgres"), "dex": dex_image(), "keycloak": local_image("keycloak"), "gateway": local_image("traefik"),
            "registry": local_image("registry")}


def project_images():
    """The images of every project's Live scope: the same Dex, Temporal and MinIO releases as the platform's."""
    pinned = images()
    return {"dexImage": pinned["dex"], "temporalImage": pinned["temporal"], "s3Image": pinned["minio"]}


def compose_environment(topology):
    """The values compose.yaml interpolates. They include the platform credentials, so they stay out of every argument and log."""
    credentials, postgres = platform_credentials(topology.instance), postgres_credentials(topology.instance)
    pinned, ports = images(), topology.ports
    environment = {key: os.environ[key] for key in DOCKER_SETTINGS if key in os.environ}
    environment.update(SV2_NATIVE_INSTANCE=topology.instance.name, SV2_TEMPORAL_IMAGE=pinned["temporal"], SV2_TEMPORAL_NAMESPACE=names_for("test")["temporal_namespace"],
                       SV2_MINIO_IMAGE=pinned["minio"], SV2_MINIO_ACCESS=credentials["access"], SV2_MINIO_SECRET=credentials["secret"],
                       SV2_POSTGRES_IMAGE=pinned["postgres"], SV2_POSTGRES_PASSWORD=postgres[POSTGRES_SUPERUSER],
                       SV2_DEX_IMAGE=pinned["dex"], SV2_DEX_CONFIG=json.dumps(dex_config(credentials, postgres,
                           native_read_model_stores(topology.instance) if engine_enabled() else None)),
                       SV2_KEYCLOAK_IMAGE=pinned["keycloak"], SV2_KEYCLOAK_ORIGIN=topology.keycloak_origin, SV2_STUDIO_ORIGIN=topology.studio_origin,
                       SV2_GATEWAY_IMAGE=pinned["gateway"], SV2_REGISTRY_IMAGE=pinned["registry"])
    environment.update(keycloak_secret_environment(keycloak_credentials(topology.instance)))
    for name, port in (("TEMPORAL", "temporal"), ("TEMPORAL_UI", "temporal_ui"), ("MINIO", "minio"), ("POSTGRES", "postgres"), ("DEX", "dex"), ("DEX_WEB", "dex_web"),
                       ("KEYCLOAK", "keycloak"), ("ROUTES", "routes"), ("GATEWAY", "gateway"), ("GATEWAY_API", "gateway_api"), ("REGISTRY", "registry")):
        environment["SV2_" + name + "_PORT"] = str(ports[port])
    return environment


def native_read_model_stores(instance):
    """Add the Engine store only in Native, whose Engine migration precedes Dex."""
    password = engine_postgres_credentials(instance)["dexengine_dex"]
    stores = dict(READ_MODEL_STORES)
    stores["dexengine"] = {
        "tableName": "public.organizations", "rowKey": "org-{organization_id}",
        "attributeMaps": {"Members": {"tableName": "public.organization_members", "instanceKeyColumn": "account_id"}},
        "dsn": f"postgres://dexengine_dex:{password}@postgres:5432/dexengine?sslmode=disable",
    }
    stores["dex_servers"] = {
        "tableName": "public.dex_servers", "rowKey": "dexserver-{server_id}",
        "attributeMaps": {"APIKeys": {"tableName": "public.dex_server_api_keys", "instanceKeyColumn": "key_id"}},
        "dsn": f"postgres://dexengine_dex:{password}@postgres:5432/dexengine?sslmode=disable",
    }
    return stores


def engine_enabled():
    return (ROOT / "cmd/dexengine-server/main.go").exists()


def compose(topology, *arguments, input=None, timeout=600):
    return run(["docker", "compose", "--project-name", topology.instance.compose_project, "--project-directory", COMPOSE_FILE.parent, "--file", COMPOSE_FILE, *arguments],
               env=compose_environment(topology), input=input, private=True, timeout=timeout)


def up(topology, cancelled, migrate):
    """Start the platform services and return once each answers on its loopback port.

    MinIO's buckets exist before Dex starts, because Dex keeps its blobs there; PostgreSQL's roles exist before `migrate`
    applies the server's migrations, and the migrations succeeded before Dex starts, because Dex refuses to start while a
    table of the read model's Attribute Store is missing. Dex is recreated on every start: Compose does not notice a
    changed inline configuration, so a Dex container that an earlier run left would keep the configuration it was created
    with, which may lack the Attribute Store. Keycloak is recreated too, so it imports the current realm into a new
    database and keeps no OAuth session of an earlier run. Data stays in the project's volumes,
    so a later start finds the Dex history, the Temporal executions, the database, the artifacts and the registry images
    of earlier runs.
    """
    ports = topology.ports
    compose(topology, "up", "--detach", "--remove-orphans", "temporal", "minio", "postgres", "gateway", "registry", timeout=1800)
    wait_until(lambda: connect(ports["minio"]), seconds=90, cancelled=cancelled)
    wait_until(lambda: buckets(topology), seconds=60, interval=2, cancelled=cancelled)
    wait_until(lambda: postgres_ready(topology), seconds=90, interval=1, cancelled=cancelled)
    compose(topology, "exec", "-T", "postgres", "psql", "-v", "ON_ERROR_STOP=1", "-U", POSTGRES_SUPERUSER, "-d", POSTGRES_DATABASE,
            input=postgres_roles_sql(postgres_credentials(topology.instance)), timeout=60)
    if engine_enabled():
        compose(topology, "exec", "-T", "postgres", "psql", "-v", "ON_ERROR_STOP=1", "-U", POSTGRES_SUPERUSER, "-d", POSTGRES_DATABASE,
                input=engine_roles_sql(engine_postgres_credentials(topology.instance)), timeout=60)
    migrate()
    # A published port accepts connections before its container listens, so each service must answer its own protocol.
    wait_until(lambda: answers(f"http://127.0.0.1:{ports['temporal_ui']}/"), seconds=180, cancelled=cancelled)
    compose(topology, "up", "--detach", "--force-recreate", "--no-deps", "dex", "keycloak", timeout=900)
    cli = dexcli()
    wait_until(lambda: dex_answers(cli, f"127.0.0.1:{ports['dex']}"), seconds=180, interval=1, cancelled=cancelled)
    wait_until(lambda: answers(f"http://127.0.0.1:{ports['gateway_api']}/api/overview"), seconds=60, cancelled=cancelled)
    wait_until(lambda: answers(f"http://127.0.0.1:{ports['registry']}/v2/"), seconds=60, cancelled=cancelled)
    # The realm's discovery document answers only once the import finished.
    wait_until(lambda: answers(f"http://127.0.0.1:{ports['keycloak']}/realms/superverse-e2e/.well-known/openid-configuration"), seconds=180, cancelled=cancelled)


def stop(topology):
    """Stop the platform services, which frees the fixed ports; their containers and volumes stay for the next run."""
    compose(topology, "stop", "--timeout", "30", timeout=300)


def identity(topology):
    """Each platform service's running container: its ID, image reference and image ID.

    Only these fields are kept; the inspected configuration also holds MinIO's credentials and never leaves this function.
    """
    listed = run(["docker", "ps", "--all", "--quiet", "--no-trunc", "--filter", "label=com.docker.compose.project=" + topology.instance.compose_project], capture=True).split()
    if not listed:
        return {}
    result = {}
    for container in json.loads(run(["docker", "container", "inspect", *listed], capture=True, private=True)):
        service = container["Config"]["Labels"].get("com.docker.compose.service")
        instance = container["Config"]["Labels"].get("superdurable.io/native-instance")
        compatible_default = topology.instance.name == "default" and instance in (None, "default")
        require(service in SERVICES and service not in result and (instance == topology.instance.name or compatible_default),
                "Unexpected container in the Native Compose project")
        result[service] = {"container": container["Id"], "image": container["Config"]["Image"], "imageId": container["Image"],
                           "running": container["State"]["Running"]}
    return result


def require_running(topology, expected=None):
    """The platform runs every service with its pinned image; with `expected`, still in the same containers."""
    current = identity(topology)
    pinned = images()
    require(set(current) == set(SERVICES) and all(current[name]["running"] and current[name]["image"] == pinned[name] for name in SERVICES),
            "The Native platform services are not all running with their pinned images")
    require(expected is None or current == expected, "A Native platform container changed during the run")
    return current


def buckets(topology):
    """Create the platform buckets; MinIO may accept connections before it serves requests, which is transient."""
    try:
        create_buckets(topology.s3_endpoint, platform_credentials(topology.instance))
    except subprocess.CalledProcessError:
        raise RuntimeError("The platform MinIO does not serve requests yet") from None


def postgres_ready(topology):
    """PostgreSQL accepts TCP connections. The first start initializes the volume with a socket-only server first, so the socket alone is not ready."""
    try:
        compose(topology, "exec", "-T", "postgres", "pg_isready", "-h", "127.0.0.1", "-U", POSTGRES_SUPERUSER, "-d", POSTGRES_DATABASE, timeout=15)
    except RuntimeError:
        raise RuntimeError("The platform PostgreSQL does not accept connections yet") from None


def connect(port):
    socket.create_connection(("127.0.0.1", port), timeout=1).close()


def answers(url):
    with urllib.request.urlopen(url, timeout=3) as response:
        require(response.status == 200, "Not ready: " + url)
