# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Places an agent review's export in the workspace for place_evolve_export.

The platform wrote the export's runs in chunks, and manifest.json, metrics.json
and goals.md, under <folder>/.parts/. One process finishes the folder and
answers one JSON object on stdout: it concatenates the chunks into runs.jsonl
while hashing them and refuses a digest other than the expected one, moves the
other files beside it, makes the four files read-only, removes .parts/ and
keeps the newest two review folders. Exit code 1 with {"error": ...} is a
failure the caller may repeat; a folder already finished with the expected
digest is answered again.
"""
import hashlib
import json
import os
import re
import shutil
import sys

FILES = ("manifest.json", "metrics.json", "goals.md")
KEPT_FOLDERS = 2
REVIEW = re.compile(r"^r-([1-9][0-9]{0,17})$")


def digest(path):
    hashed = hashlib.sha256()
    with open(path, "rb") as file:
        for block in iter(lambda: file.read(1 << 20), b""):
            hashed.update(block)
    return hashed.hexdigest()


def finished(folder, expected):
    names = [os.path.join(folder, name) for name in ("runs.jsonl",) + FILES]
    return (not os.path.exists(os.path.join(folder, ".parts")) and all(os.path.isfile(name) for name in names)
            and digest(names[0]) == expected)


def place(folder, request):
    expected, chunks = request["runsSha256"], request["chunks"]
    if finished(folder, expected):
        return {"runs_sha256": expected, "bytes": os.path.getsize(os.path.join(folder, "runs.jsonl")), "pruned": prune(folder)}
    parts = os.path.join(folder, ".parts")
    runs = os.path.join(folder, "runs.jsonl.tmp")
    hashed, size = hashlib.sha256(), 0
    with open(runs, "wb") as out:
        for index in range(chunks):
            with open(os.path.join(parts, "runs-%04d" % index), "rb") as chunk:
                data = chunk.read()
            hashed.update(data)
            size += len(data)
            out.write(data)
    if hashed.hexdigest() != expected:
        os.remove(runs)
        return {"error": "the placed runs have SHA-256 %s, not the export's %s" % (hashed.hexdigest(), expected)}
    for name in FILES:
        target = os.path.join(folder, name)
        if os.path.exists(target):
            os.remove(target)
        os.replace(os.path.join(parts, name), target)
        os.chmod(target, 0o444)
    target = os.path.join(folder, "runs.jsonl")
    if os.path.exists(target):
        os.remove(target)
    os.replace(runs, target)
    os.chmod(target, 0o444)
    shutil.rmtree(parts)
    return {"runs_sha256": expected, "bytes": size, "pruned": prune(folder)}


def prune(folder):
    """Removes the review folders beyond the newest two, this one kept."""
    parent, current = os.path.dirname(folder), os.path.basename(folder)
    reviews = []
    for name in os.listdir(parent):
        match = REVIEW.match(name)
        if match and name != current and os.path.isdir(os.path.join(parent, name)) and not os.path.islink(os.path.join(parent, name)):
            reviews.append((int(match.group(1)), name))
    pruned = [name for _, name in sorted(reviews, reverse=True)[KEPT_FOLDERS - 1:]]
    for name in pruned:
        shutil.rmtree(os.path.join(parent, name))
    return pruned


def main():
    try:
        folder = sys.argv[1]
        if not REVIEW.match(os.path.basename(folder)):
            raise ValueError("%s is no review folder" % folder)
        answer = place(folder, json.loads(sys.stdin.read() or "{}"))
    except (OSError, ValueError, KeyError, IndexError) as error:
        json.dump({"error": "%s: %s" % (type(error).__name__, error)}, sys.stdout)
        return 1
    json.dump(answer, sys.stdout)
    return 1 if "error" in answer else 0


if __name__ == "__main__":
    raise SystemExit(main())
