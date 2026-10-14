#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Keep the Studio E2E coverage map, the Studio source files whose code each browser spec runs, and name the specs
that run the files a change touches.

`merge DIRECTORY` replaces the map's entries of the specs recorded in DIRECTORY (one JSON file per passing spec,
written by web/studio/tests/e2e/support/coverage when SUPERVERSE_E2E_COVERAGE_DIR names it) and keeps the others,
so a run of some specs refreshes only theirs; it keeps only existing specs and existing Studio sources, the files
the check asks about. `native e2e` merges the
coverage of its three cases itself. The map, web/studio/tests/e2e/coverage-map.json, is committed.

`affected` lists, for each Studio source file the change touches, the specs of the map that run it. Each such file
needs one of them in the pull request's `Specs:` field (scripts/check-local-full-evidence.py); a file no spec of the
map runs, such as server-side code, a style sheet, type declarations or code no spec reaches, needs none.
"""
import argparse
import importlib.util
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
STUDIO = "web/studio/"
SPECS = ROOT / STUDIO / "tests/e2e"
MAP = SPECS / "coverage-map.json"
SOURCE_SUFFIXES = (".ts", ".tsx", ".mjs", ".js", ".css")
NOT_SOURCE = ("tests/", "mock/", "lib/generated/", "scripts/", "proto/", "public/", "node_modules/", ".next/")

_spec = importlib.util.spec_from_file_location("required_lanes", ROOT / "scripts/required-lanes.py")
required_lanes_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(required_lanes_module)


def load():
    """The map: spec path, relative to web/studio/tests/e2e -> the Studio source files whose code it runs."""
    return json.loads(MAP.read_text())["specs"] if MAP.exists() else {}


def save(specs):
    document = {"specs": {spec: sorted(set(files)) for spec, files in sorted(specs.items())}}
    MAP.write_text(json.dumps(document, indent=2) + "\n")


def spec_exists(name):
    return name.endswith(".spec.ts") and (SPECS / name).is_file()


def studio_sources(paths):
    """The changed Studio source files, relative to web/studio, that still exist."""
    found = []
    for path in paths:
        if not path.startswith(STUDIO) or not path.endswith(SOURCE_SUFFIXES):
            continue
        relative = path[len(STUDIO):]
        if relative.startswith(NOT_SOURCE) or ".config." in relative or "/" not in relative and relative.endswith(".mjs"):
            continue
        if (ROOT / path).exists():
            found.append(relative)
    return sorted(found)


def covering(files, specs):
    """For each file, the specs that run it."""
    return {file: sorted(spec for spec, executed in specs.items() if file in executed) for file in files}


def merge(directory):
    specs = load()
    records = sorted(Path(directory).glob("*.json"))
    if not records:
        sys.exit(f"{directory} holds no spec coverage; run the specs with SUPERVERSE_E2E_COVERAGE_DIR set")
    for record in records:
        document = json.loads(record.read_text())
        if document["scripts"] and not document["files"]:
            sys.exit(f"{document['spec']} ran {document['scripts']} scripts but none maps to a Studio source file; "
                     "the Studio build lacks browser source maps (SUPERVERSE_STUDIO_SOURCE_MAPS=true, as Native --studio build sets)")
        specs[document["spec"]] = document["files"]
    # The map keeps only what the check can ask about: existing specs and existing Studio sources.
    specs = {spec: studio_sources(STUDIO + file for file in files) for spec, files in specs.items() if spec_exists(spec)}
    for record in records:
        spec = json.loads(record.read_text())["spec"]
        print(f"{spec}: {len(specs.get(spec, []))} Studio files")
    save(specs)
    print(f"Updated {MAP.relative_to(ROOT)} ({len(specs)} specs)")


def affected(base):
    files = studio_sources(required_lanes_module.changed_paths(base))
    if not files:
        print("The change touches no Studio source; `Specs:` may stay empty.")
        return
    for file, specs in covering(files, load()).items():
        print(f"{STUDIO}{file}: " + (f"run by {', '.join(specs)}" if specs else "no spec of the coverage map runs it"))
    print("Run at least one spec that runs each file and list its name in the `Specs:` field.")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("merge").add_argument("directory")
    subparsers.add_parser("affected").add_argument("--base", default="origin/main",
                                                   help="the branch the change merges into (default origin/main)")
    args = parser.parse_args()
    if args.command == "merge":
        merge(args.directory)
    else:
        affected(args.base)


if __name__ == "__main__":
    main()
