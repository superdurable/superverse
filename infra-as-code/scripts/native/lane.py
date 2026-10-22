# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The private state and effective topology of a named Local Native instance.

The default instance preserves the original ``.local/superverse-v2/native``
directory and every port from native.yaml. A named instance owns a sibling
directory, a stable 100-port offset and a separate /16 Docker network range.
"""
import base64
import json
import re
import secrets
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlsplit

import yaml

import config_files
from coding_image import CodingImage
from git_host import LaneGit
from kind.cluster import host_ports as kind_host_ports
import local_platform
from operator_errors import require
from paths import STATE
from private_files import private_lock, retained_secret, write_private, write_private_json

DEFAULT_INSTANCE = "default"
INSTANCE_NAME = re.compile(r"[a-z0-9]{1,12}")
# A non-default instance adds this once-declared stride to every Native port.
INSTANCE_PORT_STRIDE = 100
# The default occupies 10.233.0.0/16; subsequent instances occupy the next
# private 10/16s. The range makes 23 slots; an index whose ports reach Kind's
# or the dynamic range is skipped.
MAX_INSTANCE_INDEX = 22
BASE_NETWORK_SECOND_OCTET = 233
# Keycloak's last two digits are those of no other port of either lane, so no instance's
# offset port lands on another instance's or on the Kind lane's.
TEMPORAL_PORT, TEMPORAL_UI_PORT, DEX_WEB_PORT, KEYCLOAK_PORT = 25233, 26233, 26812, 26170
DYNAMIC_PORTS = 32768
CONFIG = config_files.DIRECTORY / "native.yaml"
INSTANCES = STATE / "native-instances.json"
INSTANCES_LOCK = STATE / "native-instances.lock"


def validate_instance(name):
    require(bool(INSTANCE_NAME.fullmatch(name)), "--instance must be 1 to 12 lowercase letters or digits")
    return name


def instance_ports(document, index):
    """Every loopback port of the instance at index, from native.yaml's document."""
    offset = index * INSTANCE_PORT_STRIDE

    def port(address): return int(address.rsplit(":", 1)[1]) + offset
    def url_port(url): return urlsplit(url).port + offset
    return {
        "studio": url_port(document["studio"]["publicOrigin"]), "grpc": port(document["server"]["grpcAddress"]),
        "health": port(document["server"]["healthAddress"]), "worker": port(document["dex"]["workerAddress"]),
        "git": port(document["localgit"]["listen"]), "routes": port(document["native"]["routesListen"]),
        "dex": port(document["dex"]["serverAddress"]), "dex_web": DEX_WEB_PORT + offset,
        "minio": url_port(document["storage"]["s3Endpoint"]), "temporal": TEMPORAL_PORT + offset,
        "temporal_ui": TEMPORAL_UI_PORT + offset, "gateway": url_port(document["routes"]["gatewayURL"]),
        "gateway_api": url_port(document["native"]["gatewayAPI"]), "registry": port(document["native"]["registry"]),
        "builder": url_port(document["builds"]["localURL"]), "postgres": document["postgres"]["port"] + offset,
        "credential_read": port(document["connections"]["readListen"]), "keycloak": KEYCLOAK_PORT + offset,
        "slack_events": port(document["connections"]["slackRelay"]["listenAddress"]),
        "remote_agent": port(document["remoteAgent"]["listen"]),
    }


def instance_index(name):
    """Return the stable index of name; only native delete releases it."""
    validate_instance(name)
    with private_lock(INSTANCES_LOCK, message="Another Native command assigns an instance topology"):
        values = json.loads(INSTANCES.read_text()) if INSTANCES.exists() else {DEFAULT_INSTANCE: 0}
        require(values.get(DEFAULT_INSTANCE) == 0 and all(isinstance(key, str) and isinstance(value, int) for key, value in values.items()),
                "Native instance topology registry is invalid; reconcile it privately")
        if name not in values:
            used = set(values.values())
            document = config_files.read_layer(CONFIG)
            taken = set(kind_host_ports().values()).union(*(instance_ports(document, value).values() for value in used))

            def servable(index):
                # Topology would refuse such an index on every start, or it would share a port with a registered instance.
                ports = instance_ports(document, index).values()
                return taken.isdisjoint(ports) and max(ports) < DYNAMIC_PORTS
            available = next((value for value in range(1, MAX_INSTANCE_INDEX + 1) if value not in used and servable(value)), None)
            require(available is not None, "All Local Native instance topology slots are allocated; stop using a stale instance name before adding another")
            values[name] = available
            write_private_json(INSTANCES, values)
        index = values[name]
    require(0 <= index <= MAX_INSTANCE_INDEX, "Native instance topology index is out of range")
    return index


@dataclass(frozen=True)
class Instance:
    """One independently owned Native instance and its state paths."""
    name: str
    index: int
    root: Path

    @property
    def runs(self): return self.root / "runs"

    @property
    def git(self): return LaneGit(self.root)

    @property
    def build_token(self): return self.root / "release-build-token"

    @property
    def release_builds(self): return self.root / "release-builds"

    @property
    def profiles(self): return self.root / "sandbox-profiles"

    @property
    def coding(self):
        # Each instance tags its own build: moving a shared tag drops the digest reference a running instance pinned.
        tag = "native" if self.name == DEFAULT_INSTANCE else "native-" + self.name
        return CodingImage("docker.io/superverse-v2/coding-sandbox:" + tag, self.root / "sandbox-build")

    @property
    def coding_record(self): return self.root / "coding-image.json"

    @property
    def receipt(self): return self.root / "e2e-acceptance.json"

    @property
    def compose_project(self): return "superverse-v2-native" if self.name == DEFAULT_INSTANCE else "superverse-v2-native-" + self.name


def native_instance(name=DEFAULT_INSTANCE):
    name = validate_instance(name)
    index = instance_index(name)
    return Instance(name, index, STATE / ("native" if name == DEFAULT_INSTANCE else "native-" + name))


class Topology:
    """The effective loopback addresses and isolated Docker range of an instance."""

    def __init__(self, instance):
        self.instance = instance
        document = config_files.read_layer(CONFIG)
        self.document = document
        self.ports = instance_ports(document, instance.index)
        self.studio_origin = "http://localhost:" + str(self.ports["studio"])
        require(len(set(self.ports.values())) == len(self.ports), "The ports of the Native lane overlap")
        shared = sorted(set(self.ports.values()) & set(kind_host_ports().values()))
        require(not shared, f"The Native lane serves ports of the Kind lane {shared}; each lane needs ports of its own to run beside the other")
        require(max(self.ports.values()) < DYNAMIC_PORTS, f"The Native lane's ports must be below {DYNAMIC_PORTS}, where the engine draws the ports it publishes")
        self.local_git_url = f"http://127.0.0.1:{self.ports['git']}"
        self.registry = f"localhost:{self.ports['registry']}"
        self.worker_target = f"host.docker.internal:{self.ports['worker']}"
        self.s3_endpoint = f"http://127.0.0.1:{self.ports['minio']}"
        self.network_prefix = f"10.{BASE_NETWORK_SECOND_OCTET + instance.index}.0.0/16"
        # The browser's origin of Keycloak, which its tokens carry as their issuer.
        self.keycloak_origin = f"http://localhost:{self.ports['keycloak']}"

    def layer(self):
        """Settings whose effective value is this instance's rather than native.yaml's default."""
        git_target = f"host.docker.internal:{self.ports['git']}"
        return {
            "studio": {"publicOrigin": self.studio_origin},
            "remoteAgent": {"listen": f"127.0.0.1:{self.ports['remote_agent']}"},
            "server": {"grpcAddress": f"127.0.0.1:{self.ports['grpc']}", "healthAddress": f"127.0.0.1:{self.ports['health']}"},
            "dex": {"serverAddress": f"127.0.0.1:{self.ports['dex']}", "workerAddress": f"127.0.0.1:{self.ports['worker']}", "workerTarget": self.worker_target},
            # The browser on the host reaches the instance's MinIO at the same loopback address.
            "storage": {"s3Endpoint": self.s3_endpoint, "browserEndpoint": self.s3_endpoint},
            "builds": {"localURL": f"http://127.0.0.1:{self.ports['builder']}"},
            "postgres": {"host": "127.0.0.1", "port": self.ports["postgres"]},
            "localgit": {"listen": f"127.0.0.1:{self.ports['git']}", "cloneBaseURL": "http://" + git_target},
            "connections": {"readListen": f"127.0.0.1:{self.ports['credential_read']}",
                            "readURL": f"http://host.docker.internal:{self.ports['credential_read']}/internal/connection-credentials/read",
                            "slackRelay": {"listenAddress": f"127.0.0.1:{self.ports['slack_events']}"}},
            "routes": {"localPort": self.ports['gateway'], "gatewayURL": f"http://127.0.0.1:{self.ports['gateway']}"},
            "native": {"instance": self.instance.name, "networkPrefix": self.network_prefix, "registry": self.registry,
                       "gatewayAPI": f"http://127.0.0.1:{self.ports['gateway_api']}", "routesListen": f"127.0.0.1:{self.ports['routes']}"},
        }


def platform_credentials(instance):
    return json.loads(retained_secret(instance.root / "platform-credentials.json", lambda: json.dumps({"access": secrets.token_hex(12), "secret": secrets.token_urlsafe(36)})))


def postgres_credentials(instance):
    return local_platform.postgres_credentials(instance.root)


def keycloak_credentials(instance):
    return local_platform.keycloak_credentials(instance.root)


def cookie_signing_key(instance):
    return retained_secret(instance.root / "cookie-signing-key", lambda: base64.b64encode(secrets.token_bytes(32)).decode())


def slack_signing_secret(instance):
    """The test Signing Secret of the Slack event relay, which the relay tests sign events with."""
    return retained_secret(instance.root / "slack-signing-secret", lambda: secrets.token_hex(32))


def browser_signing_key(instance):
    key = retained_secret(instance.root / "browser-signing-key", lambda: base64.urlsafe_b64encode(secrets.token_bytes(32)).decode().rstrip("="))
    require(re.fullmatch(r"[A-Za-z0-9_-]{43}", key) and base64.urlsafe_b64decode(key + "=") != base64.b64decode(cookie_signing_key(instance)),
            "Native Studio requires its own browser signing key")
    return key


def build_token(instance):
    return retained_secret(instance.build_token, lambda: secrets.token_hex(32))


def run_layer(run_directory, values):
    path = run_directory / "native-run.yaml"
    write_private(path, yaml.safe_dump(values))
    return path


def release_instance_index(name):
    """Unregister a deleted instance, whose network range and ports the next new instance may take."""
    require(name != DEFAULT_INSTANCE, "The default instance keeps index 0")
    with private_lock(INSTANCES_LOCK, message="Another Native command assigns an instance topology"):
        values = json.loads(INSTANCES.read_text())
        values.pop(name, None)
        write_private_json(INSTANCES, values)
