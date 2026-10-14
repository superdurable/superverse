#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Keep every internal/ package small: at most 10 hand-written Go files, at most 10 sub-packages,
and at most 10 entries directly in internal/. A package over a limit is split along a responsibility
seam; there are no exceptions."""
from collections import defaultdict
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys

LIMIT = 10
GENERATED = re.compile(r"^// Code generated .* DO NOT EDIT\.$", re.MULTILINE)
repository = Path(__file__).resolve().parents[1]


def main():
    listing = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", "internal"], cwd=repository, text=True)
    files = [PurePosixPath(name) for name in sorted(set(filter(None, listing.split("\0")))) if (repository / name).is_file()]
    hand_written = defaultdict(int)
    go_directories = set()
    for file in files:
        if file.suffix != ".go":
            continue
        go_directories.update(file.parents)
        if not file.name.endswith("_test.go") and not GENERATED.search((repository / file).read_text()):
            hand_written[file.parent] += 1
    # A sub-package is a child directory with Go code at any depth, so a grouping
    # directory of several packages counts like the packages it groups. The
    # entries of internal/ itself have their own limit below.
    sub_packages = defaultdict(set)
    for directory in go_directories:
        if len(directory.parts) > 2:
            sub_packages[directory.parent].add(directory.name)
    failures = [f"{directory}: {count} non-test, non-generated Go files, limit {LIMIT}"
                for directory, count in sorted(hand_written.items()) if count > LIMIT]
    failures += [f"{directory}: {len(children)} sub-packages, limit {LIMIT}"
                 for directory, children in sorted(sub_packages.items()) if len(children) > LIMIT]
    entries = sorted({file.relative_to("internal").parts[0] for file in files})
    if len(entries) > LIMIT:
        failures.append(f"internal: {len(entries)} entries ({' '.join(entries)}), limit {LIMIT}")
    if failures:
        print("\n".join(failures + ["split the package along a responsibility seam; there are no exceptions"]),
              file=sys.stderr)
        return 1
    print(f"Package limits: at most {LIMIT} Go files and {LIMIT} sub-packages per package, {LIMIT} entries in internal/")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
