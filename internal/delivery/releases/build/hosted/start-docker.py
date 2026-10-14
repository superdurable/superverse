#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Start the Docker daemon owned by one ephemeral CodeBuild host."""
import json
import os
from pathlib import Path
import subprocess
import time


def main():
    if os.geteuid() != 0 or os.environ.get("DOCKER_HOST") != "unix:///var/run/docker.sock":
        raise ValueError("the dedicated privileged Unix-socket host is required")
    # A reused/unknown daemon is not the reviewed fresh job boundary. The host
    # owns this daemon until CodeBuild disposes the execution environment; the
    # runner separately owns and joins every application/tool/BuildKit container.
    if Path("/var/run/docker.sock").exists() or Path("/var/run/sv2-dockerd.pid").exists():
        raise ValueError("fresh Docker host required")
    process = subprocess.Popen(["/usr/local/bin/dockerd", "--config-file", "/usr/local/lib/sv2/daemon.json"],
                               stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                               start_new_session=True)
    ready = False
    try:
        deadline = time.monotonic() + 30
        while process.poll() is None and time.monotonic() < deadline:
            try:
                result = subprocess.run(["docker", "info", "--format", "{{json .}}"],
                                        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=2, check=False)
                if result.returncode == 0:
                    info = json.loads(result.stdout)
                    if info.get("ServerVersion") != "29.8.1" or info.get("OSType") != "linux" or info.get("Architecture") != "aarch64" or info.get("DockerRootDir") != "/var/lib/sv2-docker" or info.get("Driver") != "overlay2":
                        raise ValueError("unexpected Docker host identity")
                    ready = True
                    return
            except subprocess.TimeoutExpired:
                pass
            time.sleep(0.25)
        raise ValueError("Docker host startup did not become ready")
    finally:
        if not ready:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        raise SystemExit("Hosted Docker initialization did not complete") from None
