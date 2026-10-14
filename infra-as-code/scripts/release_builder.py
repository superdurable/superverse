# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The local release builder of either lane: its trusted AppManifest tool and its command.

The builder (internal/delivery/releases/build/localbuilder/server.py) runs the coding image by Docker's immutable ID,
not a mutable tag, so no tag can swap its toolchain. It fetches Project source from the origin of the repository host
the server uses: the local Git host on this machine, or GitHub. With the local Git host it also redirects the
connectors library to the host's copy, so a release candidate of a lane's contribution builds. Each lane passes its
own token, build state and publication target.
"""
import os
import sys

import build_inputs
from commands import run
from git_host import builder_base_url, builder_module_redirect
from image_identity import docker_image_id
from operator_errors import require
from paths import ROOT, STATE

SERVER = ROOT / "internal/delivery/releases/build/localbuilder/server.py"


def builder_command(*, token_file, tool_image, repository_host, local_git_url, arguments=()):
    """The builder's command for the coding image `tool_image` (a Docker image ID), the lane's local Git host and its other `arguments`."""
    require(docker_image_id(tool_image) == tool_image, "The accepted coding image is unavailable to the host builder")
    tool = compile_app_manifest_tool()
    redirect = builder_module_redirect(repository_host, local_git_url)
    return [sys.executable, SERVER, "--token-file", token_file, "--tool-image", tool_image, "--app-manifest-tool", tool,
            "--git-base-url", builder_base_url(repository_host, local_git_url), *(["--module-redirect", redirect] if redirect else []), *arguments]


def compile_app_manifest_tool():
    """Compile the trusted AppManifest tool into a directory named by its source fingerprint; return the tool.

    Only this trusted owner is compiled; application repositories are data inputs. Both lanes share the compiled tools,
    and their builders may start together, so each compiles under a name of its own and renames the tool into place.
    """
    fingerprint = build_inputs.fingerprint(build_inputs.app_manifest_tool_inputs())
    directory = STATE / "app-manifest-tools" / fingerprint
    directory.mkdir(parents=True, exist_ok=True)
    tool, building = directory / "app-manifest", directory / f"app-manifest.building-{os.getpid()}"
    run(["go", "build", "-trimpath", "-o", building, "./cmd/tools/app-manifest"], env=dict(os.environ, GOWORK="off", GOFLAGS="", CGO_ENABLED="0"), timeout=900)
    if build_inputs.fingerprint(build_inputs.app_manifest_tool_inputs()) != fingerprint:
        building.unlink(missing_ok=True)
        raise RuntimeError("AppManifest tool source changed while compiling")
    building.replace(tool)
    return tool
