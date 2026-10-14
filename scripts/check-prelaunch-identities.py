#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Pre-launch identity rules of AGENTS.md: protobuf declares no `reserved` fields, and Dex Flow and
Step identities derive from Go type names, with no hand-written GetFlowType or GetStepType."""
from pathlib import Path
import re
import subprocess
import sys

repository = Path(__file__).resolve().parents[1]

# Validate owned sources, excluding ignored dependencies and generated Go getters.
paths = subprocess.check_output(
    ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "*.proto", "*.go"],
    cwd=repository,
).decode().split("\0")
for name in sorted(set(filter(None, paths))):
    path = repository / name
    if not path.is_file():
        continue
    source = path.read_text()
    if path.suffix == ".proto":
        # Preserve strings as one token while discarding comments. A reserved
        # declaration is a keyword outside string literals and comments.
        tokens = re.findall(r'"(?:\\.|[^"\\])*"|/\*.*?\*/|//[^\n]*|\b\w+\b', source, re.DOTALL)
        if "reserved" in tokens:
            sys.exit(f"pre-launch protobuf must not declare reserved fields: {name}")
    elif not re.search(r"^// Code generated .* DO NOT EDIT\.$", source, re.MULTILINE):
        # Defaults are provided by the SDK. Generated protobuf field accessors
        # are not Dex identity overrides and must not be edited by hand.
        stripped = re.sub(r'/\*.*?\*/|//[^\n]*|"(?:\\.|[^"\\])*"|`[^`]*`', "", source, flags=re.DOTALL)
        if re.search(r"\bfunc\s*\([^)]*\)\s*(GetFlowType|GetStepType)\s*\(", stripped):
            sys.exit(f"Dex identity must derive from its Go type: {name}")
