#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Check the committed business RPC coverage map or merge methods observed by a real local lane."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
PROTO = ROOT / "api/superverse/v2/superverse.proto"
OPERATIONS = ROOT / "web/studio/lib/server/flow/flow-api-operations.ts"
MAP = ROOT / "docs/runbooks/rpc-coverage-map.json"
BACKLOG = ROOT / "docs/runbooks/rpc-coverage-backlog.json"
INITIAL_BACKLOG = ROOT / "docs/runbooks/rpc-coverage-initial-backlog.json"
SOURCES = ("api-integration", "native-e2e", "kind-e2e")


def declared():
    source = PROTO.read_text()
    package = re.search(r"^package\s+([\w.]+);", source, re.M).group(1)
    methods = {}
    for service, body in re.findall(r"^service\s+(\w+)\s*\{(.*?)^\}", source, re.M | re.S):
        for method in re.findall(r"^\s+rpc\s+(\w+)\s*\(", body, re.M):
            if method in methods:
                sys.exit(f"duplicate RPC name: {method}")
            methods[method] = f"/{package}.{service}/{method}"
    flow_names = set(re.findall(r'\brpc\("(\w+)"', OPERATIONS.read_text()))
    missing = flow_names - methods.keys()
    if missing:
        sys.exit(f"Flow API names absent from proto: {', '.join(sorted(missing))}")
    if not flow_names:
        sys.exit("Flow API operation mapping has no RPCs")
    return set(methods.values()), {methods[name] for name in flow_names}


def read(path, key):
    return json.loads(path.read_text())[key]


def save(path, key, value):
    path.write_text(json.dumps({key: value}, indent=2) + "\n")


def check():
    declared_methods, flow_methods = declared()
    coverage = read(MAP, "methods")
    backlog = read(BACKLOG, "methods")
    if set(coverage) != declared_methods:
        sys.exit(f"RPC map differs from proto: missing={sorted(declared_methods-set(coverage))}, removed={sorted(set(coverage)-declared_methods)}")
    for method, sources in coverage.items():
        if sources != sorted(set(sources)) or set(sources) - set(SOURCES):
            sys.exit(f"invalid coverage sources for {method}: {sources}")
    if backlog != sorted(set(backlog)):
        sys.exit("RPC backlog must be sorted and unique")
    initial = read(INITIAL_BACKLOG, "methods")
    if initial != sorted(set(initial)):
        sys.exit("initial RPC backlog must be sorted and unique")
    if not set(backlog) <= set(initial):
        sys.exit(f"RPC backlog contains a new uncovered method: {sorted(set(backlog)-set(initial))}")
    uncovered = sorted(method for method, sources in coverage.items() if not sources)
    if backlog != uncovered:
        sys.exit(f"RPC backlog differs from observed coverage: missing={sorted(set(uncovered)-set(backlog))}, now covered={sorted(set(backlog)-set(uncovered))}")
    previous = subprocess.run(["git", "show", "origin/main:docs/runbooks/rpc-coverage-backlog.json"], cwd=ROOT, capture_output=True, text=True)
    if previous.returncode == 0:
        older = set(json.loads(previous.stdout)["methods"])
        if not set(backlog) <= older:
            sys.exit(f"RPC backlog grew: {sorted(set(backlog)-older)}")
    print(f"RPC coverage: {len(coverage)-len(backlog)}/{len(coverage)} methods observed; {len(flow_methods)} Flow API methods included; backlog {len(backlog)}")


def merge(source, path):
    if source not in SOURCES:
        sys.exit(f"unknown RPC coverage source: {source}")
    declared_methods, _ = declared()
    observed = set(read(path, "methods"))
    if not observed or observed - declared_methods:
        sys.exit(f"invalid or empty RPC observation: {sorted(observed-declared_methods)}")
    coverage = read(MAP, "methods") if MAP.exists() else {method: [] for method in declared_methods}
    if set(coverage) != declared_methods:
        sys.exit("RPC map differs from proto; add new RPCs with empty sources before merging")
    previously_covered = {method for method, sources in coverage.items() if sources}
    for method in coverage:
        coverage[method] = sorted((set(coverage[method]) - {source}) | ({source} if method in observed else set()))
    lost = sorted(method for method in previously_covered if not coverage[method])
    if lost:
        sys.exit(f"RPC coverage regressed in {source}: {lost}")
    save(MAP, "methods", dict(sorted(coverage.items())))
    save(BACKLOG, "methods", sorted(method for method, sources in coverage.items() if not sources))
    print(f"RPC coverage: {source} observed {len(observed)} methods; backlog {len(read(BACKLOG, 'methods'))}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("check")
    merged = subparsers.add_parser("merge")
    merged.add_argument("source", choices=SOURCES)
    merged.add_argument("file", type=Path)
    args = parser.parse_args()
    if args.command == "check":
        check()
    else:
        merge(args.source, args.file)


if __name__ == "__main__":
    main()
