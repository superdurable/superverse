#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0


"""Verify that every copy of the Dex SDK, dexcli and platform Dex image pins agrees."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
SEMVER = r"[0-9]+\.[0-9]+\.[0-9]+"
PLATFORMS = ("darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64")


class DexVersionError(RuntimeError):
    """A Dex component pin is missing or inconsistent."""


def read(relative: str) -> str:
    return (ROOT / relative).read_text(encoding="utf-8")


def pin(pattern: str, relative: str, label: str) -> re.Match[str]:
    match = re.search(pattern, read(relative), flags=re.MULTILINE)
    if match is None:
        raise DexVersionError(f"{label} pin is missing")
    return match


def check_pins() -> tuple[str, str, str]:
    """Return the SDK version, dexcli version and dexcli commit after checking every copy."""
    source_lock = json.loads(read("infra-as-code/source-lock.json"))
    lock = source_lock["dex"]
    sdk = pin(rf"^\s*github\.com/superdurable/dex/sdk-go\s+v({SEMVER})\s*$", "go.mod", "Dex Go SDK").group(1)
    cli = pin(rf"^DEXCLI_VERSION := v({SEMVER})$", "Makefile", "Makefile dexcli").group(1)
    commit = lock["cliCommit"]
    if sdk != lock["goSDK"] or cli != lock["cli"] or not re.fullmatch(r"[a-f0-9]{40}", commit):
        raise DexVersionError("go.mod and Makefile Dex pins differ from the source-lock")

    installer = read("scripts/install-pinned-binary.sh")
    entries = re.findall(rf'"dexcli v({SEMVER}) ([a-z0-9_]+)"\) echo ([a-f0-9]{{64}})', installer)
    checksums = {platform: checksum for _, platform, checksum in entries}
    if {version for version, _, _ in entries} != {cli} or sorted(platform for _, platform, _ in entries) != sorted(PLATFORMS):
        raise DexVersionError("installer dexcli checksums do not cover exactly the four platforms of the pinned version")
    if pin(r"^dexcli_commit=([a-f0-9]{40})$", "scripts/install-pinned-binary.sh", "installer dexcli commit").group(1) != commit:
        raise DexVersionError("installer dexcli commit differs from the source-lock")
    if pin(rf"^ARG DEXCLI_VERSION=({SEMVER})$", "sandbox/coding/Dockerfile", "coding image dexcli").group(1) != cli:
        raise DexVersionError("coding image dexcli version differs from the source-lock")

    tools = read("internal/dexai/delivery/releases/build/hosted/Dockerfile.tools")
    hosted = dict(re.findall(r"(amd64|arm64)\) digest=([a-f0-9]{64})", tools))
    if any(hosted.get(architecture) != checksums["linux_" + architecture] for architecture in ("amd64", "arm64")):
        raise DexVersionError("hosted dexcli archive checksums differ from the installer")
    if ('cli-v${DEXCLI_VERSION}/dexcli_v${DEXCLI_VERSION}_linux_' not in tools or
            'io.superdurable.dexcli="$DEXCLI_VERSION"' not in tools or
            'io.superdurable.hosted-build-tools="$HOSTED_BUILD_TOOLS_VERSION"' not in tools):
        raise DexVersionError("hosted toolchain and admission do not match the dexcli pin")
    contract = {"hosted-build-tools": source_lock["hostedBuildTools"]["version"], "dexcli": cli}
    if not re.fullmatch(r"v[0-9]+", contract["hosted-build-tools"]):
        raise DexVersionError("invalid hosted build-tools contract version")
    if read("internal/dexai/delivery/releases/build/hosted/build-contract.generated.json") != json.dumps(contract, indent=2) + "\n":
        raise DexVersionError("embedded hosted build-tools contract differs from the source-lock")

    image = pin(rf'  image: \{{repository: docker.io/superdurable/dex-server, tag: "v({SEMVER})", digest: "(sha256:[a-f0-9]{{64}})"',
                "infra-as-code/kubernetes/charts/superverse-platform/values.yaml", "platform Dex chart image")
    if image.groups() != (lock["server"], lock["serverImageDigest"]):
        raise DexVersionError("platform Dex chart version or digest differs from the source-lock")
    terraform_path = "infra-as-code/terraform/kubernetes/variables.tf"
    # The private Terraform layer is absent from the public mirror.
    if (ROOT / terraform_path).exists():
        terraform = pin(r'repository = "docker\.io/superdurable/dex-server"\s+digest\s+= "(sha256:[a-f0-9]{64})"',
                        terraform_path, "platform Dex Terraform default")
        if terraform.group(1) != lock["serverImageDigest"]:
            raise DexVersionError("platform Dex Terraform default differs from the source-lock")
    return sdk, cli, commit


def check_binary(binary: Path, cli: str, commit: str) -> None:
    """Never download or contact a server; only ask an existing binary for its identity."""
    output = subprocess.run([str(binary.resolve(strict=True)), "version"], capture_output=True, text=True, check=True, timeout=10).stdout
    if not re.match(rf"^dexcli v?{re.escape(cli)} \(commit {re.escape(commit)}[, )]", output):
        raise DexVersionError("dexcli binary version or commit differs from the reviewed pin")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dexcli-binary", type=Path, help="Also verify an existing dexcli binary's version and commit")
    parser.add_argument("--docker-build-args", action="store_true", help="Emit reviewed Docker build arguments from source-lock.json")
    args = parser.parse_args()
    sdk, cli, commit = check_pins()
    if args.dexcli_binary:
        check_binary(args.dexcli_binary, cli, commit)
    if args.docker_build_args:
        hosted = json.loads(read("infra-as-code/source-lock.json"))["hostedBuildTools"]["version"]
        print(f"--build-arg DEXCLI_VERSION={cli} --build-arg HOSTED_BUILD_TOOLS_VERSION={hosted}")
    else:
        print(f"Dex SDK {sdk}; dexcli {cli}, its commit and checksums, and the platform Dex image pins agree")


if __name__ == "__main__":
    main()
