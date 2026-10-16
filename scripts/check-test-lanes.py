#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Keep the lane guard of the Dex integration tests in step with the lanes.

internal/comm/dexflow/dexflowtest refuses a test whose Dex, Worker or API addresses do not all
belong to the lane SUPERVERSE_TEST_LANE names. It reads Local Native's addresses from native.yaml,
their owner. Local Kind's are declared once as constants there, because their owners are scripts
and a chart; this check fails when a constant differs from its owner, and when a test builds a Dex
client or Worker without going through the guard."""
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GUARD = ROOT / "internal/comm/dexflow/dexflowtest"
CLUSTER = ROOT / "infra-as-code/scripts/kind/cluster.py"
CHART_CONFIG = ROOT / "infra-as-code/kubernetes/charts/superverse-platform/templates/_server-config.tpl"
HOST_FROM_CONTAINERS = ROOT / "internal/dexai/projects/infra/addresses.go"
# A Dex client or Worker that a test builds itself skips the guard.
DIRECT = re.compile(r"\bdex\.(NewClient|NewWorker)\(")


def one(pattern, path, text=None):
    """The only capture group of the only match of pattern in the file."""
    matches = re.findall(pattern, path.read_text() if text is None else text, re.MULTILINE)
    if len(matches) != 1:
        sys.exit(f"{path.relative_to(ROOT)}: expected exactly one match of {pattern!r}, found {len(matches)}")
    return matches[0]


def kind_constants():
    guard = (GUARD / "lane_endpoints.go").read_text()
    return {name: one(rf'^\s*{name}\s*=\s*"([^"]*)"', GUARD / "lane_endpoints.go", guard)
            for name in ("kindDex", "kindAPI", "kindServerWorker", "kindWorkerHost", "kindLocalGit", "kindPostgres")}


def expected_kind_constants():
    namespace = one(r'^NAMESPACE = "([^"]+)"', CLUSTER)
    worker = one(r'"workerTarget" \(printf "([^"]+)"', CHART_CONFIG)
    dex_port = one(r'^\s*"dex": Forward\("superverse-v2-dex", (\d+), 8801,', CLUSTER)
    grpc_port = one(r'^\s*"grpc": Forward\("superverse-v2-server", (\d+), 8080,', CLUSTER)
    git_port = one(r'^\s*"git": Forward\("superverse-v2-server", (\d+), 8085,', CLUSTER)
    postgres_port = one(r'^\s*"postgres": Forward\("postgres", (\d+), 5432,', CLUSTER)
    return {
        "kindDex": f"127.0.0.1:{dex_port}",
        "kindAPI": f"127.0.0.1:{grpc_port}",
        "kindLocalGit": f"127.0.0.1:{git_port}",
        "kindPostgres": f"127.0.0.1:{postgres_port}",
        "kindServerWorker": worker % namespace,
        "kindWorkerHost": one(r'^const HostFromContainers = "([^"]+)"', HOST_FROM_CONTAINERS),
    }


def direct_clients():
    """Test code outside the guard's package that builds a Dex client or Worker itself."""
    for path in sorted((ROOT / "internal").rglob("*.go")):
        relative = path.relative_to(ROOT)
        in_test_package = any(part.endswith("test") for part in relative.parts[:-1])
        if (path.name.endswith("_test.go") or in_test_package) and GUARD not in path.parents:
            for number, line in enumerate(path.read_text().splitlines(), 1):
                if DIRECT.search(line):
                    yield f"{relative}:{number}: builds a Dex client or Worker itself; use dexflowtest.Client or dexflowtest.WorkerClient, which check SUPERVERSE_TEST_LANE"


def main():
    problems = []
    declared, expected = kind_constants(), expected_kind_constants()
    for name, value in expected.items():
        if declared[name] != value:
            problems.append(f"{GUARD.relative_to(ROOT)}/lane_endpoints.go: {name} is {declared[name]!r}, but its owner says {value!r}")
    problems.extend(direct_clients())
    if problems:
        sys.exit("\n".join(problems))
    print("Test lanes: the Kind addresses of dexflowtest match their owners; every test Dex client goes through the guard")


main()
