#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Build the real app template twice across loss of this check's execution caches."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
PROFILE = ROOT / "internal/dexai/delivery/releases/build/build-profiles/go-react-v1"


def run(arguments, *, log=None):
    print("+ " + " ".join(str(value) for value in arguments), flush=True)
    return subprocess.run(arguments, check=True, text=True, stdout=log or subprocess.PIPE,
                          stderr=subprocess.STDOUT, timeout=900).stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--builder", help="use an existing builder with uniquely owned cache mounts instead of creating one")
    args = parser.parse_args()
    directory = Path(tempfile.mkdtemp(prefix="superverse-release-cache-"))
    context = directory / "context"
    context.mkdir()
    paths = run(["git", "-C", ROOT, "ls-files", "-z", "app-template"]).split("\0")
    for name in filter(None, paths):
        source = ROOT / name
        target = context / source.relative_to(ROOT / "app-template")
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, target)
    manifest = json.loads((PROFILE / "profile.json").read_text())
    cache_prefix = "sv2-release-cache-" + uuid.uuid4().hex[:12]
    builder = args.builder or cache_prefix
    # Only cache identities differ; commands, network isolation and image pins
    # remain the product recipe's. Never prune another build's cache mounts.
    dockerfile = directory / "Dockerfile"
    dockerfile.write_text((PROFILE / "Dockerfile").read_text().replace("id=go-react-v1-", "id=" + cache_prefix + "-"))
    build_arguments = []
    for name, value in manifest["buildArgs"].items():
        build_arguments.extend(["--build-arg", name + "=" + value])
    print("Evidence directory: " + str(directory), flush=True)
    if not args.builder:
        run(["docker", "buildx", "create", "--name", builder, "--driver", "docker-container"])
    evidence = {"builder": builder, "sourceCommit": run(["git", "-C", ROOT, "rev-parse", "HEAD"]).strip(),
                "profileDigest": hashlib.sha256((PROFILE / "profile.json").read_bytes() + b"\0" +
                                                 (PROFILE / "Dockerfile").read_bytes()).hexdigest(),
                "cachePrefix": cache_prefix, "builds": []}
    def owned_mounts():
        output = run(["docker", "buildx", "du", "--builder", builder,
                      "--filter", "type=exec.cachemount", "--format", "{{json .}}"])
        return [row["ID"] for line in output.splitlines() if cache_prefix in (row := json.loads(line))["Description"]]

    def prune_mounts(mounts):
        with (directory / "prune.log").open("a") as log:
            for identity in mounts:
                run(["docker", "buildx", "prune", "--builder", builder, "--force",
                     "--filter", "type=exec.cachemount", "--filter", "id=" + identity], log=log)
    try:
        for attempt in (1, 2):
            if attempt == 2:
                before = owned_mounts()
                if not before:
                    raise RuntimeError("First build produced no execution cache mounts to evict")
                prune_mounts(before)
                after = owned_mounts()
                if after:
                    raise RuntimeError("Execution cache mounts survived the forced prune")
                evidence["cacheLoss"] = {"before": before, "after": []}
            started = time.monotonic()
            with (directory / f"build-{attempt}.log").open("w") as log:
                run(["docker", "buildx", "build", "--builder", builder, "--progress=plain",
                     "--no-cache-filter", "go-build", "--file", dockerfile,
                     "--metadata-file", directory / f"build-{attempt}.json",
                     "--output", f"type=oci,dest={directory / f'image-{attempt}.tar'}",
                     *build_arguments, context], log=log)
            metadata = json.loads((directory / f"build-{attempt}.json").read_text())
            if attempt == 2:
                lines = (directory / "build-2.log").read_text().splitlines()
                downloads = [line.split()[0] for line in lines if "[go-modules " in line and "RUN go " in line]
                if len(downloads) != 2 or any(identity + " CACHED" not in lines for identity in downloads):
                    raise RuntimeError("Second build did not reuse both module-download layers")
            result = {"attempt": attempt, "seconds": round(time.monotonic() - started, 2),
                      "imageDigest": metadata["containerimage.digest"]}
            evidence["builds"].append(result)
            print(json.dumps(result), flush=True)
        print("Both Release builds passed across forced execution-cache loss.", flush=True)
    finally:
        (directory / "evidence.json").write_text(json.dumps(evidence, indent=2) + "\n")
        if args.builder:
            prune_mounts(owned_mounts())
        else:
            run(["docker", "buildx", "rm", builder])


if __name__ == "__main__":
    main()
