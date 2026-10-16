# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The local Git host of a local lane: its files on this machine and the credentials its tooling needs.

The local Git host (internal/comm/platform/repohost/localgit) runs inside the server. Each lane keeps its own bare
repositories and signing key in its private state directory, because each lane has its own Dex data and Projects:
Kind in .local/superverse-v2 (the Kind node mounts its git directory into the server Pod), Native in
.local/superverse-v2/native. The end-to-end runner derives its administrator token from the lane's key, and the host
builder fetches Project source from the host's address on this machine.
"""
import base64
import hashlib
import hmac
import json
import secrets
import time

from operator_errors import require
from private_files import retained_secret, write_private

# The server configuration chooses the repository host with the repohost key; the default is GitHub.
LOCAL_GIT = "localgit"
GITHUB = "github"

# Where this machine reaches the Kind lane's host: `kind ports` forwards it to the server Service. Native's is its
# localgit.listen.
HOST_URL = "http://127.0.0.1:28085"
# Clone origin of the hosts the builder fetches from.
GITHUB_URL = "https://github.com"
TOKEN_PREFIX = "sv2lg1"
# Longer than the longest browser case.
ADMINISTRATOR_SECONDS = 4 * 3600


def repository_host(document):
    """The repository host of a server configuration document (merged profile or rendered ConfigMap)."""
    return document.get("repohost", GITHUB)


def builder_base_url(host, local_url):
    """The origin from which the host builder fetches Project source: the lane's local Git host at local_url, or GitHub."""
    return local_url if host == LOCAL_GIT else GITHUB_URL


# The connectors library's module (connectorlibrary.ModulePath) and the local Git host's copy of it (localgit's
# connectorLibraryName below its repositoryOwner), which holds the release candidates of a lane's contributions.
CONNECTOR_LIBRARY_MODULE = "github.com/superdurable/dex-connectors-library"
CONNECTOR_LIBRARY_COPY = "superverse-local/dex-connectors-library"


def builder_module_redirect(host, local_url):
    """The builder's redirect of the connectors library to the lane's copy, MODULE=CLONE_URL, or none on GitHub.

    The builder's containers download modules; a container of this machine's Docker Engine reaches the host's
    loopback listener at local_url through host.docker.internal, as the Sandbox reaches Native's host.
    """
    if host != LOCAL_GIT:
        return None
    require(local_url.startswith("http://127.0.0.1:"), "The local Git host must listen on this machine's loopback")
    return CONNECTOR_LIBRARY_MODULE + "=" + local_url.replace("http://127.0.0.1:", "http://host.docker.internal:", 1) + "/" + CONNECTOR_LIBRARY_COPY + ".git"


class LaneGit:
    """The local Git host's files in one lane's private state directory."""

    def __init__(self, state):
        self.repositories = state / "git"
        self.signing_key_path = state / "git-signing-key"
        self.administrator_token_path = state / "git-administrator-token"

    def signing_key(self):
        """The retained key that signs repository credentials: standard base64 of 32 random bytes, private to this machine."""
        return retained_secret(self.signing_key_path, lambda: base64.b64encode(secrets.token_bytes(32)).decode())

    def administrator_token(self, now=None):
        """A token of the administrator scope in the format of the host's credentials (see token.go), signed with the lane's key."""
        claims = {"scope": "administrator", "expiresAt": int((now if now is not None else time.time()) + ADMINISTRATOR_SECONDS)}
        payload = base64.urlsafe_b64encode(json.dumps(claims, separators=(",", ":")).encode()).decode().rstrip("=")
        signed = (TOKEN_PREFIX + "." + payload).encode()
        mac = hmac.new(base64.b64decode(self.signing_key(), validate=True), signed, hashlib.sha256).digest()
        return TOKEN_PREFIX + "." + payload + "." + base64.urlsafe_b64encode(mac).decode().rstrip("=")

    def write_administrator_token(self):
        """Write a fresh administrator token into its private file and return the file; the end-to-end tests read it."""
        write_private(self.administrator_token_path, self.administrator_token())
        return self.administrator_token_path
