# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The owned Kind cluster: identity, ownership marker, private state and kubeconfig-scoped commands.

Every Kubernetes call uses the private kubeconfig and the kind-superverse-v2 context, never the
operator's current context. An existing cluster is used only when this checkout's owner marker matches.
"""
import json
import re
import shutil
from typing import NamedTuple
from urllib.parse import urlsplit

from commands import run
from config_files import DIRECTORY, read_layer
from git_host import LaneGit
from operator_errors import require
from paths import MAIN_CHECKOUT, STATE
from pins import LOCK
from private_files import write_private_json

CLUSTER = "superverse-v2"
NAMESPACE = "superverse-v2-system"
# Envoy Gateway's controller, the project Gateway and its proxies (templates/project-gateway.yaml).
GATEWAY_NAMESPACE = "superverse-v2-gateway"
KUBECONFIG = STATE / "kubeconfig"
STATE_FILE = STATE / "state.json"
KIND_VERSION = LOCK["kind"]
KIND_NODE_IMAGE = LOCK["kindNodeImage"]


class Forward(NamedTuple):
    service: str
    local_port: int
    remote_port: int
    dependency: bool  # Also forwarded by `kind ports --dependencies-only`.
    namespace: str = NAMESPACE


# The loopback port catalog of `kind ports`. Local Native serves ports of its own (native.yaml).
FORWARDS = {
    "temporal": Forward("temporal", 27233, 7233, True),
    "temporal_ui": Forward("temporal", 28233, 8233, True),
    "minio": Forward("minio", 29000, 9000, True),
    # The server's database, for the integration tests (internal/platform/postgres/postgrestest).
    "postgres": Forward("postgres", 28432, 5432, True),
    "dex": Forward("superverse-v2-dex", 28811, 8801, True),
    "dex_web": Forward("superverse-v2-dex", 28812, 8802, True),
    "studio": Forward("superverse-v2-studio", 23000, 3000, False),
    "grpc": Forward("superverse-v2-server", 28080, 8080, False),
    # The local Git host of the server, for the host builder and the end-to-end tests.
    "git": Forward("superverse-v2-server", 28085, 8085, False),
    # The project Gateway's proxies, which serve every Live and Preview host of the lane.
    "preview": Forward("projects-gateway", 23080, 80, False, GATEWAY_NAMESPACE),
    # The Slack event relay's events listener, which the relay tests post signed events to.
    "slack_events": Forward("superverse-v2-server", 28088, 8087, False),
    # The credential read listener, whose Slack trigger registrations the relay tests post to.
    "credential_read": Forward("superverse-v2-server", 28087, 8086, False),
    "remote_agent": Forward("superverse-v2-server", 28089, 8088, False),
    # The OAuth provider of the connection tests; environments/local-kind/values.yaml names this port as its issuer.
    "keycloak": Forward("keycloak", 28190, 8080, True),
}
PORTS = {name: forward.local_port for name, forward in FORWARDS.items()}


def host_ports():
    """Every loopback port of the Kind lane on this machine: the catalog of `kind ports` and the host builder's port."""
    return {**PORTS, "builder": urlsplit(read_layer(DIRECTORY / "local-kind.yaml")["builds"]["localURL"]).port}


# The server's non-secret configuration document, rendered by the chart; the Pod mounts it.
SERVER_CONFIG = "superverse-v2-server-config"
SERVER_CONFIG_KEY = "server.yaml"
# The Kind lane's local Git host: its repositories, which the node mounts at NODE_PATH, and its signing key.
GIT = LaneGit(STATE)
# The directory of the Kind node that GIT.repositories is mounted at; the chart's v2.localGit.nodePath mounts it into the Pod.
NODE_PATH = "/var/lib/superverse-git"


KUBECTL = ["kubectl", "--kubeconfig", str(KUBECONFIG), "--context", "kind-" + CLUSTER]


def kubectl(*args, **kwargs):
    return run([*KUBECTL, *args], **kwargs)


def helm(*args, **kwargs):
    return run(["helm", *args, "--kubeconfig", str(KUBECONFIG), "--kube-context", "kind-" + CLUSTER], **kwargs)


def read_object(kind, name, *, private=False):
    return json.loads(kubectl("--request-timeout=30s", "get", kind, name, "-n", NAMESPACE, "-o", "json", capture=True, private=private))


def port_forward_argv(service, *mappings, namespace=NAMESPACE):
    return [*KUBECTL, "-n", namespace, "port-forward", "--address", "127.0.0.1", "service/" + service, *mappings]


def kind_nodes():
    nodes = run(["kind", "get", "nodes", "--name", CLUSTER], capture=True).splitlines()
    require(nodes and all(node.startswith(CLUSTER + "-") for node in nodes), "Unexpected Kind node identity")
    return nodes


def apply(*objects):
    kubectl("apply", "-f", "-", input=json.dumps({"apiVersion": "v1", "kind": "List", "items": list(objects)}))


def object_meta(name):
    return {"name": name, "namespace": NAMESPACE, "labels": {"app.kubernetes.io/part-of": "superverse-v2"}}


def state(required=True):
    if not STATE_FILE.exists():
        require(not required, "No V2 ownership state. Run up; existing unowned clusters are never adopted.")
        return None
    value = json.loads(STATE_FILE.read_text())
    require(value.get("checkout") == str(MAIN_CHECKOUT) and value.get("cluster") == CLUSTER, "V2 state belongs to another checkout; refusing to reuse it")
    return value


def save(value):
    write_private_json(STATE_FILE, value)


def verify_owner():
    value = state()
    marker = read_object("configmap", "superverse-v2-owner")
    require(marker["data"] == {"checkout": str(MAIN_CHECKOUT), "owner": value["owner"]}, "Cluster ownership marker differs; refusing to use this cluster")
    return value


def require_tools(*names):
    missing = [name for name in names if not shutil.which(name)]
    require(not missing, "Missing tools: " + ", ".join(missing))


def require_kind_version():
    require(re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", KIND_VERSION) and
            re.fullmatch(r"kindest/node:v[0-9]+\.[0-9]+\.[0-9]+@sha256:[a-f0-9]{64}", KIND_NODE_IMAGE),
            "Kind version and node image require exact reviewed pins")
    fields = run(["kind", "version"], capture=True, timeout=10).split()
    require(fields[:2] == ["kind", "v" + KIND_VERSION], "Kind requires the reviewed version " + KIND_VERSION + " from source-lock.json")


def prepare_repositories():
    """Create the repository directory that the Kind node mounts.

    The server Pod's non-root user must write it, so the directory is writable by anyone; its parent, the private state
    directory, keeps every other user out.
    """
    GIT.repositories.mkdir(parents=True, exist_ok=True)
    GIT.repositories.chmod(0o777)


def missing_node_mounts():
    """The Kind nodes that do not mount the repository directory.

    A cluster created before the local Git host has no such mount, and the server Pod would not start. Recreating the
    cluster once adds it: delete the cluster, then run `up` again.
    """
    missing = []
    for node in kind_nodes():
        mounts = json.loads(run(["docker", "inspect", node, "--format", "{{json .Mounts}}"], capture=True))
        if not any(mount.get("Destination") == NODE_PATH for mount in mounts):
            missing.append(node)
    return missing


def recreation_advice(nodes):
    return ("The Kind node " + ", ".join(nodes) + " does not mount the local Git repositories at " + NODE_PATH + ". Recreate the cluster "
            "(kind delete --confirm superverse-v2, then up) so that it is created with the mount of " + str(GIT.repositories))


def require_node_mount():
    """Refuse a Kind cluster whose node does not mount the repository directory."""
    missing = missing_node_mounts()
    require(not missing, recreation_advice(missing))
