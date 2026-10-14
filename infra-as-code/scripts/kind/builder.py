# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Run the Kind lane's local release builder, which loads Release images into the owned cluster."""
import secrets

import build_inputs
from commands import run
import git_host
from kind.cluster import verify_owner
from operator_errors import require
from paths import STATE
from private_files import retained_secret, write_private
from release_builder import builder_command

BUILD_TOKEN = STATE / "release-build-token"


def build_token():
    """The private token the Kind builder and the server it serves share."""
    return retained_secret(BUILD_TOKEN, lambda: secrets.token_hex(32))


def builder(_args):
    value = verify_owner()
    inputs = value.get("deploymentInputs", {}).get("config", {}).get("input")
    require(inputs, "Deploy the server (kind deploy) before the local builder: its repository host is the one the deployment chose")
    coding = value.get("images", {}).get("coding")
    require(coding and coding["sourceFingerprint"] == build_inputs.component_fingerprint("coding"), "Build the accepted coding image before starting the local builder")
    # Only the secret: the builder's address is the builds.localURL setting of the server configuration profile.
    write_private(STATE / "release-build.env", f"SUPERVERSE_LOCAL_BUILD_TOKEN={build_token()}\n")
    # The service lives until operator cancellation; individual builds own their deadlines.
    run(builder_command(token_file=BUILD_TOKEN, tool_image=coding["id"], repository_host=git_host.repository_host(inputs), local_git_url=git_host.HOST_URL),
        timeout=None)
