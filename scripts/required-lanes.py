#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Name the local lane a change runs its E2E cases and Dex integration tests on: Local Kind when the change touches a
runtime path of scripts/kind-required-paths.txt, excluding documentation and test files, Local Native otherwise,
and both when it touches a Kind path and a path of scripts/native-required-paths.txt, which only Local Native exercises.

The change is every path that differs between the merge base of HEAD and --base (origin/main by default) and the
working tree, staged or not, plus untracked files. The answer goes into the pull request's Tests section; before an
AWS wave, the Local Kind acceptance on local-kind-acceptance.yaml is required whatever it says, with the model the platform developer decides.
"""
import argparse
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
KIND_LIST = ROOT / "scripts/kind-required-paths.txt"
NATIVE_LIST = ROOT / "scripts/native-required-paths.txt"


def patterns(path):
    """The globs of a list, in order."""
    return [line.strip() for line in path.read_text().splitlines() if line.strip() and not line.lstrip().startswith("#")]


def matcher(pattern):
    """A glob as a regular expression: ** spans path segments, * stays within one."""
    expression = "".join(".*" if part == "**" else "[^/]*" if part == "*" else re.escape(part) for part in re.split(r"(\*\*|\*)", pattern))
    return re.compile(expression + r"\Z")


def git(*arguments):
    return subprocess.run(["git", "-C", str(ROOT), *arguments], check=True, capture_output=True, text=True).stdout


def changed_paths(base):
    """The repository paths the change touches."""
    merge_base = git("merge-base", base, "HEAD").strip()
    changed = git("diff", "--name-only", merge_base).splitlines()
    changed += git("ls-files", "--others", "--exclude-standard").splitlines()
    return sorted(set(changed))


def touched(paths, list_path):
    """Each changed path that a glob of the list matches, with the glob."""
    matchers = [(pattern, matcher(pattern)) for pattern in patterns(list_path)]
    return [(path, next(pattern for pattern, expression in matchers if expression.match(path)))
            for path in paths if any(expression.match(path) for _, expression in matchers)]


def runtime_path(path):
    """Documentation and tests do not change the Kubernetes runtime."""
    parts = Path(path).parts
    name = parts[-1].lower()
    return not (
        any(part.lower() in {"docs", "test", "tests", "__tests__", "testdata"} for part in parts[:-1])
        or name.startswith("readme") or name.endswith((".md", ".markdown", ".mdx", ".rst", ".adoc"))
        or name.endswith(("_test.go", "_test.py", ".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx"))
        or name.startswith("test_") and name.endswith(".py")
    )


def required_lanes(paths):
    """The lanes the paths require, as `native`, `kind` or `native, kind`, with the Kind and Native paths touched."""
    kind = touched([path for path in paths if runtime_path(path)], KIND_LIST)
    native = touched(paths, NATIVE_LIST)
    return "native, kind" if kind and native else "kind" if kind else "native", kind, native


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", default="origin/main", help="the branch the change merges into (default origin/main)")
    args = parser.parse_args()
    try:
        paths = changed_paths(args.base)
    except subprocess.CalledProcessError as error:
        sys.exit(f"Cannot compare with {args.base}: {error.stderr.strip()}")
    lanes, kind, native = required_lanes(paths)
    print(f"Required lanes for {len(paths)} changed paths against {args.base}: {lanes}")
    for path, pattern in kind:
        print(f"  kind: {path} ({pattern})")
    if kind:
        for path, pattern in native:
            print(f"  native: {path} ({pattern})")



if __name__ == "__main__":
    main()
