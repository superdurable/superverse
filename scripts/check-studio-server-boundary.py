#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Static architecture drift checks; this does not prove runtime acceptance."""
from pathlib import Path
import re
import subprocess

root = Path(__file__).resolve().parents[1]
failures = []
for path in (root / "internal").rglob("*.go"):
    if path.name.endswith("_test.go"):
        continue
    content = path.read_text()
    if re.search(r'"github\.com/superdurable/dex/(web|server)(?:/|\")', content):
        failures.append(f"{path.relative_to(root)}: Dex Web/Server implementation import")
    if re.search(r'\b8802\b', content):
        failures.append(f"{path.relative_to(root)}: server Go must not address Dex Web (8802); project Dex is API-only")
# The images are part of the product boundary too.
for name in ("Dockerfile", "web/studio/Dockerfile"):
    if re.search(r"\b8802\b", (root / name).read_text()):
        failures.append(f"{name}: the server and Studio images must not address Dex Web (8802); project Dex is API-only")
modules = subprocess.check_output(["go", "list", "-m", "all"], cwd=root, text=True)
for module in modules.splitlines():
    if module.split()[0] in {"github.com/superdurable/dex/server", "github.com/superdurable/dex/web"}:
        failures.append(f"forbidden module: {module.split()[0]}")
if failures:
    raise SystemExit("\n".join(failures))
print("Studio server/image boundary: public SDK protocols, no Dex Web imports or port")
