# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The secrets of the server Secret, kept apart from its configuration.

The server reads these from its environment and every other setting from the configuration document
(cmd/superverse-server/config/README.md). The private server env file may be shared with other layers and hold settings too,
so only the keys below are taken from it and the rest are ignored: a setting can never reach the server through two sources.
"""
from pathlib import Path
import re

from git_host import GITHUB, LOCAL_GIT
from operator_errors import require

# The secrets the server env file supplies. Deploy generates the cookie signing key and the MinIO credentials itself; the file may replace them.
SERVER_SECRET_KEYS = frozenset({"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "GITHUB_APP_PRIVATE_KEY", "GITHUB_APP_PRIVATE_KEY_BASE64",
                                "GITHUB_APP_PRIVATE_KEY_PATH", "SUPERVERSE_COOKIE_SIGNING_KEY", "SUPERVERSE_LOCAL_BUILD_TOKEN",
                                "SUPERVERSE_LOCAL_BUILD_TOKEN_FILE"})
# What a deployed server Secret may carry: those, the Claude key, which comes from the main checkout's .env and never from the env
# file, the signing key of the local Git host's credentials and the server's database password, which the deploy generates and
# retains (kind/cluster.py, local_platform.py), and the Slack relay's Signing Secret and the platform OAuth client secrets, which a
# manual test takes from the same .env (the Signing Secret is otherwise generated and retained).
DEPLOYED_SECRET_KEYS = SERVER_SECRET_KEYS | {"ANTHROPIC_API_KEY", "SUPERVERSE_LOCAL_GIT_SIGNING_KEY", "SUPERVERSE_POSTGRES_PASSWORD",
                                             "SUPERVERSE_SLACK_SIGNING_SECRET", "SUPERVERSE_CONNECTOR_OAUTH_CLIENT_SECRETS"}
# The secrets the server cannot work without whatever its repository host: any one key of each group is enough.
COMMON_REQUIRED_SERVER_SECRETS = (("SUPERVERSE_COOKIE_SIGNING_KEY",), ("SUPERVERSE_LOCAL_BUILD_TOKEN", "SUPERVERSE_LOCAL_BUILD_TOKEN_FILE"))
# The secrets of each repository host: GitHub needs its App private key, the local Git host the key that signs its credentials.
REPOSITORY_HOST_SECRETS = {
    GITHUB: (("GITHUB_APP_PRIVATE_KEY_BASE64", "GITHUB_APP_PRIVATE_KEY", "GITHUB_APP_PRIVATE_KEY_PATH"),),
    LOCAL_GIT: (("SUPERVERSE_LOCAL_GIT_SIGNING_KEY",),),
}
VARIABLE_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")


def read_server_secrets(path):
    """The secrets of a private server env file: its SERVER_SECRET_KEYS lines, with every other key ignored.

    The ignored keys are named once, never their values. A line must be KEY=value with an environment variable name; a line
    that is not is refused without being echoed, because it could be part of a secret. ANTHROPIC_API_KEY is refused: it comes from
    the main checkout's .env.
    """
    values, ignored = {}, set()
    for line in Path(path).read_text().splitlines():
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        key, separator, value = line.partition("=")
        key = key.strip()
        require(separator and VARIABLE_NAME.fullmatch(key), "Server env file lines must be KEY=value with an environment variable name")
        require(key != "ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY comes from the .env file at the root of the main checkout, not from the server env file")
        if key in SERVER_SECRET_KEYS:
            values[key] = value
        else:
            ignored.add(key)
    note_ignored("the server env file", ignored)
    return values


def note_ignored(source, keys):
    """The one notice about the keys of a secrets-only source that are ignored: names only, never values."""
    if keys:
        print(f"Ignored {len(keys)} key(s) of {source}: {', '.join(sorted(keys))}. Only secrets are taken from it; "
              "the server configuration profile (cmd/superverse-server/config) is the only source of every other setting.")


def require_server_secrets(server, repository_host):
    """Refuse a server Secret that lacks a secret the server cannot work without; the message names the keys, never a value.

    Deploy refuses a Secret that lacks a group before it applies or prunes anything, because the Secret is replaced as a whole.
    """
    for group in (*COMMON_REQUIRED_SERVER_SECRETS, *REPOSITORY_HOST_SECRETS[repository_host]):
        require(any(server.get(key) for key in group), "The server Secret lacks " + " or ".join(group) + ": add it to the server env file")
