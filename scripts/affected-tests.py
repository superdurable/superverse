#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Name the tests that cover the Go packages a change touches.

A test covers a package when the test's own package is that package, or imports it, directly or through other
packages of this module, from its test files or its other files. The graph comes from the import declarations of the
root module's Go files, build tags included, so it slightly overstates coverage; no Go toolchain runs. Packages of
other modules (app-template) are outside it.

A change touches the package of every changed Go file of the root module, test files included. Each touched package
that some test covers needs one of those tests in the pull request's `Tests:` field
(scripts/check-local-full-evidence.py); a touched package no test covers needs none. The suggested commands never
name the Makefile's DEVELOPER_ONLY_TESTS, which only the developer runs, each through its own target.
"""
import argparse
import importlib.util
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
MODULE = re.search(r"^module\s+(\S+)", (ROOT / "go.mod").read_text(), re.M).group(1)
OTHER_MODULES = tuple(str(path.parent.relative_to(ROOT)) + "/" for path in ROOT.glob("**/go.mod")
                      if path.parent != ROOT and "node_modules" not in path.parts)
IMPORT_BLOCK = re.compile(r"^import\s*\((.*?)^\)", re.M | re.S)
IMPORT_LINE = re.compile(r'^import\s+(?:[\w.]+\s+)?"([^"]+)"', re.M)
QUOTED = re.compile(r'"([^"]+)"')
TEST_FUNCTION = re.compile(r"^func (Test\w+)\(\w+ \*testing\.T\)", re.M)
DEVELOPER_ONLY_TESTS = re.compile(r"^DEVELOPER_ONLY_TESTS := (.*)$", re.M)
SUGGESTED_TESTS = 5

_spec = importlib.util.spec_from_file_location("required_lanes", ROOT / "scripts/required-lanes.py")
required_lanes_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(required_lanes_module)


def in_root_module(path):
    return path.endswith(".go") and not path.startswith(OTHER_MODULES) and "/node_modules/" not in path


def imports(text):
    """The import paths a Go file declares."""
    found = IMPORT_LINE.findall(text)
    for block in IMPORT_BLOCK.findall(text):
        found += QUOTED.findall(block)
    return found


class PackageGraph:
    """The root module's packages, by directory: their imports and their test functions."""

    def __init__(self):
        self.imports = {}       # directory -> module directories its non-test files import
        self.test_imports = {}  # directory -> module directories its test files import
        self.tests = {}         # directory -> test function names
        files = sorted(set(required_lanes_module.git("ls-files", "*.go").splitlines() +
                           required_lanes_module.git("ls-files", "--others", "--exclude-standard", "*.go").splitlines()))
        files = [path for path in files if in_root_module(path) and (ROOT / path).is_file()]
        for path in files:
            directory, text = str(Path(path).parent), (ROOT / path).read_text(encoding="utf-8", errors="replace")
            local = {name[len(MODULE) + 1:] for name in imports(text) if name.startswith(MODULE + "/")}
            is_test = path.endswith("_test.go")
            (self.test_imports if is_test else self.imports).setdefault(directory, set()).update(local)
            if is_test:
                self.tests.setdefault(directory, []).extend(TEST_FUNCTION.findall(text))

    def reached_from_tests(self, directory):
        """Every package directory the tests in directory reach, with its import distance: their own package (0) and
        what it and its test files import, transitively."""
        distance, frontier = {directory: 0}, [directory]
        while frontier:
            following = []
            for current in frontier:
                imported = set(self.imports.get(current, ()))
                if current == directory:
                    imported |= self.test_imports.get(current, set())
                for name in imported - distance.keys():
                    distance[name] = distance[current] + 1
                    following.append(name)
            frontier = following
        return distance

    def covering(self, packages):
        """For each package directory, the directories with tests that cover it, nearest first."""
        coverage = {package: [] for package in packages}
        for directory, names in self.tests.items():
            if not names:
                continue
            reached = self.reached_from_tests(directory)
            for package in packages & reached.keys():
                coverage[package].append((reached[package], directory))
        return {package: [directory for _, directory in sorted(found)] for package, found in coverage.items()}

    def test_directories(self, name):
        """The package directories that define the test function name."""
        return {directory for directory, names in self.tests.items() if name in names}


def developer_only_tests():
    """The Makefile's DEVELOPER_ONLY_TESTS: tests that cost real-model calls, which test-integration skips."""
    return set(DEVELOPER_ONLY_TESTS.search((ROOT / "Makefile").read_text()).group(1).split())


def touched_packages(paths):
    """The package directories of the changed Go files of the root module."""
    return {str(Path(path).parent) for path in paths if in_root_module(path)}


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--base", default="origin/main", help="the branch the change merges into (default origin/main)")
    args = parser.parse_args()
    graph = PackageGraph()
    paths = required_lanes_module.changed_paths(args.base)
    coverage = graph.covering(touched_packages(paths))
    if required_lanes_module.bridge_race_required(paths):
        print(f"Required Orca bridge check: go test -count=1 ./cmd/superverse-bridge/relay -run '^{required_lanes_module.BRIDGE_RACE_TEST}$'")
    if not coverage:
        print("The change touches no Go package of the root module; `Tests:` may stay empty.")
        return
    developer_only = developer_only_tests()
    for package, directories in sorted(coverage.items()):
        if not directories:
            print(f"{package}: no test covers it")
            continue
        print(f"{package}: covered by tests in {len(directories)} packages; the nearest")
        suggestions = [(directory, [name for name in graph.tests[directory] if name not in developer_only]) for directory in directories]
        for directory, names in [suggestion for suggestion in suggestions if suggestion[1]][:3]:
            print(f"  go test -tags=live -count=1 ./{directory} -run '^({'|'.join(names[:SUGGESTED_TESTS])})$'")
    print("Run at least one test that covers each package and list its name in the `Tests:` field.")


if __name__ == "__main__":
    sys.exit(main())
