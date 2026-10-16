#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0


"""Update the Dex SDK and dexcli pins, and optionally the platform Dex image, together.

Every copy that scripts/check-dex-versions.py verifies is rewritten; nothing is
discovered or upgraded implicitly. Immutable Release BuildProfiles are never edited.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import re
import subprocess
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
SEMVER = re.compile(r"[0-9]+\.[0-9]+\.[0-9]+")
DIGEST = re.compile(r"sha256:[a-f0-9]{64}")
PLATFORMS = ("darwin_amd64", "darwin_arm64", "linux_amd64", "linux_arm64")
SERVER_REPOSITORY = "docker.io/superdurable/dex-server"


class DexVersionError(RuntimeError):
    """A requested Dex component version or release asset is invalid."""


def require(condition: bool, message: str) -> None:
    if not condition:
        raise DexVersionError(message)


def cli_checksums(version: str, content: bytes) -> dict[str, str]:
    """Map each release platform to its archive checksum from the release checksums.txt."""
    checksums = {}
    for line in content.decode("utf-8").splitlines():
        match = re.fullmatch(r"([0-9a-f]{64})\s+\*?\.?/?dexcli_v([^/\s]+)_([a-z0-9]+_[a-z0-9]+)\.tar\.gz", line)
        if match is not None and match.group(2) == version:
            checksums[match.group(3)] = match.group(1)
    require(set(checksums) == set(PLATFORMS), "dexcli checksums.txt does not contain the four release archives")
    return checksums


def download_cli_checksums(version: str) -> bytes:
    url = f"https://github.com/superdurable/dex/releases/download/cli-v{version}/checksums.txt"
    request = urllib.request.Request(url, headers={"User-Agent": "superverse-v2-dex-upgrade/1"})
    with urllib.request.urlopen(request, timeout=30) as response:
        return response.read()


def resolve_server_digest(version: str) -> str:
    """The chart and Terraform pin the multi-platform index digest, never a per-platform manifest."""
    output = subprocess.run(["docker", "buildx", "imagetools", "inspect", f"{SERVER_REPOSITORY}:v{version}", "--format", "{{json .Manifest}}"],
                            capture_output=True, text=True, check=True, timeout=120).stdout
    digest = json.loads(output).get("digest", "")
    require(DIGEST.fullmatch(digest) is not None, "the platform Dex release did not resolve to an immutable digest")
    return digest


def update_repository(sdk_version: str, cli_version: str, cli_commit: str, checksums: dict[str, str],
                      server_version: str | None, server_digest: str | None) -> None:
    require(SEMVER.fullmatch(sdk_version) is not None and SEMVER.fullmatch(cli_version) is not None, "invalid Dex SDK or dexcli version")
    require(re.fullmatch(r"[a-f0-9]{40}", cli_commit) is not None, "the exact reviewed dexcli commit is required")
    if server_version is not None:
        require(SEMVER.fullmatch(server_version) is not None and DIGEST.fullmatch(server_digest or "") is not None,
                "invalid platform Dex version or digest")
    # Stage every edit first so a missing pin leaves the repository unchanged.
    edits: dict[Path, str] = {}

    def replace(relative: str, pattern: str, replacement: str) -> None:
        path = ROOT / relative
        content = edits.get(path, path.read_text(encoding="utf-8"))
        require(len(re.findall(pattern, content, flags=re.MULTILINE)) == 1, f"expected one pin in {relative}")
        edits[path] = re.sub(pattern, replacement, content, count=1, flags=re.MULTILINE)

    replace("go.mod", r"(github\.com/superdurable/dex/sdk-go\s+)v[^\s]+", rf"\g<1>v{sdk_version}")
    replace("Makefile", r"^DEXCLI_VERSION := v[^\s]+$", f"DEXCLI_VERSION := v{cli_version}")
    replace("scripts/install-pinned-binary.sh", r"^dexcli_commit=[a-f0-9]{40}$", "dexcli_commit=" + cli_commit)
    for platform in PLATFORMS:
        replace("scripts/install-pinned-binary.sh", rf'^    "dexcli v[^" ]+ {platform}"\) echo [a-f0-9]{{64}} ;;$',
                f'    "dexcli v{cli_version} {platform}") echo {checksums[platform]} ;;')
    replace("sandbox/coding/Dockerfile", r"^ARG DEXCLI_VERSION=[^\s]+$", f"ARG DEXCLI_VERSION={cli_version}")
    tools = "internal/dexai/delivery/releases/build/hosted/Dockerfile.tools"
    for architecture in ("amd64", "arm64"):
        replace(tools, rf"({architecture}\) digest=)[a-f0-9]{{64}}", r"\g<1>" + checksums["linux_" + architecture])
    replace(tools, r"cli-v[0-9.]+/dexcli_v[0-9.]+_linux_", f"cli-v{cli_version}/dexcli_v{cli_version}_linux_")
    replace(tools, r'io\.superdurable\.dexcli="[0-9.]+"', f'io.superdurable.dexcli="{cli_version}"')
    replace("internal/dexai/delivery/releases/build/hosted/runner.py", r'(labels\.get\("io\.superdurable\.dexcli"\) != ")[0-9.]+', r"\g<1>" + cli_version)

    lock_path = ROOT / "infra-as-code/source-lock.json"
    lock = json.loads(lock_path.read_text(encoding="utf-8"))
    lock["dex"].update(goSDK=sdk_version, cli=cli_version, cliCommit=cli_commit)
    if server_version is not None:
        lock["dex"].update(server=server_version, serverImageDigest=server_digest)
        replace("infra-as-code/kubernetes/charts/superverse-platform/values.yaml",
                r'(image: \{repository: docker.io/superdurable/dex-server, tag: ")v[^"\n]+(", digest: ")sha256:[a-f0-9]{64}',
                rf"\g<1>v{server_version}\g<2>{server_digest}")
        replace("infra-as-code/terraform/kubernetes/variables.tf",
                r'(repository = "docker\.io/superdurable/dex-server"\s+digest\s+= ")sha256:[a-f0-9]{64}', r"\g<1>" + server_digest)
    edits[lock_path] = json.dumps(lock, indent=2) + "\n"
    for path, content in edits.items():
        path.write_text(content, encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--sdk-go-version", required=True)
    parser.add_argument("--dexcli-version", required=True)
    parser.add_argument("--dexcli-commit", required=True, help="Exact source commit reported by the reviewed release binary")
    parser.add_argument("--checksums-file", type=Path, help="Reviewed checksums.txt; downloaded from the release when omitted")
    parser.add_argument("--server-version", help="Optional platform Dex release; needs --server-digest or --resolve-server-digest")
    server = parser.add_mutually_exclusive_group()
    server.add_argument("--server-digest", help="Reviewed immutable platform image index digest, never a Docker config ID")
    server.add_argument("--resolve-server-digest", action="store_true", help="Resolve the index digest of --server-version with docker buildx")
    args = parser.parse_args()
    require((args.server_version is None) == (args.server_digest is None and not args.resolve_server_digest),
            "--server-version requires exactly one of --server-digest or --resolve-server-digest")
    content = args.checksums_file.read_bytes() if args.checksums_file else download_cli_checksums(args.dexcli_version)
    digest = resolve_server_digest(args.server_version) if args.resolve_server_digest else args.server_digest
    update_repository(args.sdk_go_version, args.dexcli_version, args.dexcli_commit, cli_checksums(args.dexcli_version, content),
                      args.server_version, digest)
    print(f"Updated Dex Go SDK to {args.sdk_go_version} and dexcli to {args.dexcli_version}"
          + (f"; platform Dex to {args.server_version} at {digest}" if args.server_version else ""))


if __name__ == "__main__":
    main()
