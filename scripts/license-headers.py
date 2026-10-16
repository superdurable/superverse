#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Check or add the fixed Sustainable Use License header to owned source files."""

import argparse
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parent.parent
HEADER_TEXT = """Copyright (c) 2026 Super Durable, Inc.

Licensed under the Sustainable Use License 1.0.
You may not use this file except in compliance with the License.
See the LICENSE file in the repository root.

SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0"""
SUFFIXES = {".css", ".go", ".html", ".proto", ".py", ".ts", ".tsx", ".yaml", ".yml", ".mjs", ".js", ".sh"}

# Documentation retains its existing licensing status.
# Generated user applications are MIT licensed and must not inherit this header.
# Dex skills are MIT licensed and must not inherit this header.
# Google RPC protos retain Google's Apache-2.0 license.
# The vendored controller chart retains its upstream license.
# Generated Go and Studio contracts are covered by the root license without editing output.
# Generated Flow graphs are documentation, and must remain byte-exact.
# GitHub and agent configuration are outside the owned source inventory.
# Verification receipts are evidence, not licensed source.
# Dependencies in node_modules retain their own licenses.
EXCLUDED_PREFIXES = (
    "docs/", "app-template/", "dex-skills/", "web/studio/proto/google/rpc/",
    "infra-as-code/kubernetes/charts/superverse-platform/charts/controller/",
    "gen/", "web/studio/lib/generated/", ".github/", ".claude/", ".cursor/",
    "node_modules/", "internal/dexai/app/apptemplate/app-template.tar",
)


def excluded(name: str) -> bool:
    parts = Path(name).parts
    if any(name.startswith(prefix) for prefix in EXCLUDED_PREFIXES):
        return True
    if "testdata" in parts:  # Test inputs have byte-exact assertions or embedded contents.
        return True
    if "node_modules" in parts:  # Installed third-party dependencies have their own licenses.
        return True
    if "generated" in parts:  # Generated source is checked against its generator output.
        return True
    if name.endswith(".sql"):  # Migration checksums depend on exact SQL bytes.
        return True
    if name.endswith((".pb.go", "_gen.go")):  # Generated Go is checked for drift.
        return True
    if Path(name).name.startswith("zz_generated"):  # Generated source is checked for drift.
        return True
    return False


def render(suffix: str) -> str:
    lines = HEADER_TEXT.split("\n")
    if suffix == ".css":
        return "\n".join(["/*", *(" * " + line if line else " *" for line in lines), " */"])
    if suffix == ".html":
        return "\n".join(["<!--", *lines, "-->"])
    prefix = "#" if suffix in {".py", ".yaml", ".yml", ".sh"} else "//"
    return "\n".join(prefix + (" " + line if line else "") for line in lines)


def files() -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        cwd=ROOT, check=True, stdout=subprocess.PIPE,
    )
    return sorted(set(result.stdout.decode().strip("\0").split("\0")))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--fix", action="store_true")
    mode.add_argument("--check", action="store_true")
    args = parser.parse_args()
    missing = []
    for name in files():
        path = ROOT / name
        if path.suffix not in SUFFIXES or excluded(name) or not path.is_file():
            continue
        content = path.read_bytes()
        if content.startswith(b"#!"):
            newline = content.find(b"\n")
            shebang, body = (content[:newline + 1], content[newline + 1:]) if newline >= 0 else (content + b"\n", b"")
        else:
            shebang, body = b"", content
        header = (render(path.suffix) + "\n\n").encode()
        if body.startswith(header):
            continue
        missing.append(name)
        if args.fix:
            mode_bits = path.stat().st_mode
            with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as output:
                output.write(shebang + header + body)
                temporary = output.name
            os.chmod(temporary, mode_bits)
            os.replace(temporary, path)
    if missing:
        verb = "Added" if args.fix else "Missing"
        for name in missing:
            print(f"{verb} Sustainable Use License header: {name}", file=sys.stderr)
    print(f"{len(missing)} source files {'updated' if args.fix else 'missing headers'}")
    return 1 if missing and args.check else 0


if __name__ == "__main__":
    raise SystemExit(main())
