# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Reclaim the local lanes' Docker data while they run: superseded images, old build cache and the Kind node's unused images.

Each image is judged on its own, so running Native instances and a Kind deployment never block the command: it removes
only images no container uses, no lane records (the Kind deployment, its dependencies and each Native instance's coding
image) and that are older than the age floor, and it refuses while a Release build may still run. Images go first,
then the build cache by age: BuildKit keeps the cache records an image still references, so a size limit alone frees
nothing. The Kind node is pruned only under the Kind action lock and is skipped while a Kind action or acceptance holds
it. Project containers, volumes and networks stay: their Project's deletion owns them.
"""
import argparse
from datetime import datetime, timedelta, timezone
import json
import re
import shutil
import subprocess

from commands import run
from kind.cluster import CLUSTER
from kind.locks import kind_action_lock
from operator_errors import require
from paths import STATE

DEFAULT_IMAGE_AGE_HOURS = 72
DEFAULT_CACHE_AGE_HOURS = 24
# The repositories of the images the lanes build: components, coding images, dependency tags and Release images.
LANE_REPOSITORIES = ("superverse-v2/", "docker.io/superverse-v2/", "sv2-")
# Release images of Native carry their instance's registry, localhost:<port>/sv2-<environment>-<project>.
NATIVE_RELEASE_PREFIX = "/sv2-"
# The Kind node's names for the images it holds whose owner is a lane deployment, never a Release.
NODE_REPOSITORIES = ("docker.io/superverse-v2/", "docker.io/library/import-")
# `kind load` names each image it imports by the day of the load, which is the only age the node records.
NODE_IMPORT = re.compile(r"^docker\.io/library/import-(\d{4}-\d{2}-\d{2})@")


def recorded_references():
    """Every image reference and ID a lane records: the Kind state and each Native instance's coding image."""
    found = set()

    def collect(value):
        if isinstance(value, dict):
            for item in value.values():
                collect(item)
        elif isinstance(value, list):
            for item in value:
                collect(item)
        elif isinstance(value, str):
            found.add(value)
    for path in [STATE / "state.json", *STATE.glob("native*/coding-image.json")]:
        if path.is_file():
            collect(json.loads(path.read_text()))
    return {"names": {familiar(value) for value in found},
            "digests": {value.rsplit("@", 1)[1] for value in found if "@sha256:" in value} | {value for value in found if value.startswith("sha256:")}}


def familiar(reference):
    """A reference as Docker displays it, without the default registry and library namespace."""
    return re.sub(r"^(docker\.io/)?(library/)?", "", reference)


def protected(references, recorded):
    """Whether a lane records one of an image's names, IDs or digests."""
    named = {familiar(reference) for reference in references}
    digests = {reference.rsplit("@", 1)[1] for reference in references if "@sha256:" in reference}
    return bool(named & recorded["names"] or digests & recorded["digests"])


def created_at(value):
    """Docker's creation time, whose fraction has up to nine digits."""
    return datetime.fromisoformat(re.sub(r"(\.\d{6})\d*", r"\1", value).replace("Z", "+00:00"))


# A build that is still RUNNING or UNKNOWN after this long belongs to a builder that stopped mid-build: one build
# command is bounded to 30 minutes and ProjectReleaseFlow waits at most 60 minutes for a build.
UNSETTLED_BUILD_AGE = timedelta(hours=2)


def running_builds():
    """The Release builds of the Kind builder and each Native instance that may still run: not settled, and their state
    changed within UNSETTLED_BUILD_AGE."""
    recent = datetime.now(timezone.utc).timestamp() - UNSETTLED_BUILD_AGE.total_seconds()
    return [path for root in [STATE / "release-builds", *STATE.glob("native*/release-builds")]
            for path in root.glob("**/state.json")
            if path.stat().st_mtime > recent and json.loads(path.read_text()).get("state") in ("RUNNING", "UNKNOWN")]


def lane_image(names):
    return all(name.startswith(LANE_REPOSITORIES) or NATIVE_RELEASE_PREFIX in name for name in names)


def host_candidates(age, recorded):
    """The host images to remove: dangling or the lanes' own, older than age, used by no container, recorded by no lane."""
    container_ids = run(["docker", "ps", "--all", "--quiet", "--no-trunc"], capture=True, timeout=300).split()
    used = {familiar(reference) for reference in run(["docker", "container", "inspect", "--format", "{{.Image}} {{.Config.Image}}", *container_ids], capture=True, timeout=300).split()} if container_ids else set()
    cutoff = datetime.now(timezone.utc) - age
    candidates = []
    image_ids = sorted(set(run(["docker", "image", "ls", "--all", "--quiet", "--no-trunc"], capture=True, timeout=300).split()))
    for image in json.loads(run(["docker", "image", "inspect", *image_ids], capture=True, timeout=300)) if image_ids else []:
        names = image.get("RepoTags") or []
        references = set(names) | set(image.get("RepoDigests") or []) | {image["Id"]}
        if (created_at(image["Created"]) < cutoff and not {familiar(reference) for reference in references} & used and not protected(references, recorded)
                and (not names or lane_image(names))):
            candidates.append((image["Id"], names, image.get("Size", 0)))
    return candidates


def node_candidates(age, recorded):
    """The Kind node's images to remove: those of earlier lane deployments, loaded on a day before age, that no
    container uses and no lane records. An image loaded more recently may belong to a deployment in progress."""
    node = CLUSTER + "-control-plane"
    if subprocess.run(["docker", "container", "inspect", "--format", "{{.State.Running}}", node], capture_output=True, text=True).stdout.strip() != "true":
        return node, []
    used = {container["imageRef"] for container in json.loads(run(["docker", "exec", node, "crictl", "ps", "--all", "-o", "json"], capture=True, timeout=300))["containers"]}
    cutoff = (datetime.now(timezone.utc) - age).date().isoformat()
    candidates = []
    for image in json.loads(run(["docker", "exec", node, "crictl", "images", "-o", "json"], capture=True, timeout=300))["images"]:
        names = (image.get("repoTags") or []) + (image.get("repoDigests") or [])
        loaded = [match.group(1) for match in map(NODE_IMPORT.match, names) if match]
        if (image["id"] not in used and loaded and max(loaded) < cutoff and all(name.startswith(NODE_REPOSITORIES) for name in names)
                and not protected(names + [image["id"]], recorded)):
            candidates.append((image["id"], names, int(image.get("size", 0))))
    return node, candidates


def gigabytes(size):
    return f"{size / 1e9:.1f} GB"


def prune(args):
    builds = running_builds()
    if builds:
        raise RuntimeError(f"{len(builds)} Release builds are not settled, such as {builds[0].parent}; prune after they end")
    recorded = recorded_references()
    images = host_candidates(timedelta(hours=args.image_age_hours), recorded)
    print(f"Host images: {len(images)} unused lane images older than {args.image_age_hours} h, {gigabytes(sum(size for _, _, size in images))}")
    if args.dry_run:
        node, node_images = node_candidates(timedelta(hours=args.image_age_hours), recorded)
        report_node(args, node_images)
        for identity, names, _ in images + node_images:
            print("  would remove", identity[:19], " ".join(names[:2]))
        return
    free = shutil.disk_usage("/").free
    removed = sum(remove(["docker", "image", "rm", "--force", identity]) for identity, _, _ in images)
    run(["docker", "builder", "prune", "--force", "--filter", f"until={args.cache_age_hours}h"], timeout=1800)
    try:
        with kind_action_lock("prune"):
            # A deployment may have recorded new images since the first read.
            node, node_images = node_candidates(timedelta(hours=args.image_age_hours), recorded_references())
            report_node(args, node_images)
            for identity, _, _ in node_images:
                remove(["docker", "exec", node, "crictl", "rmi", identity])
    except RuntimeError as busy:
        print(f"Kind node: skipped, {busy}")
    print(f"Removed {removed} of {len(images)} host images and the build cache unused for {args.cache_age_hours} h.")
    print(f"Host free space: {gigabytes(free)} before, {gigabytes(shutil.disk_usage('/').free)} now (the engine may return space with a delay).")


def remove(command):
    """Remove one image; another prune or a container that started meanwhile may have claimed it, which only skips it."""
    try:
        run(command, capture=True, timeout=120)
        return True
    except subprocess.CalledProcessError as error:
        print("  skipped:", (error.stderr or error.stdout or "").strip()[-200:])
        return False


def after_run(lane):
    """The reclamation that ends a lane run: best effort, so a failure is reported and never changes the run's result."""
    try:
        prune(argparse.Namespace(image_age_hours=DEFAULT_IMAGE_AGE_HOURS, cache_age_hours=DEFAULT_CACHE_AGE_HOURS, dry_run=False))
    except Exception as error:  # noqa: BLE001 - the lane's result must not depend on its clean-up
        print(f"Disk clean-up after the {lane} run did not complete: {error}; run `sv2 local prune` later")


def report_node(args, node_images):
    print(f"Kind node: {len(node_images)} unused images of earlier deployments loaded before {args.image_age_hours} h, "
          f"{gigabytes(sum(size for _, _, size in node_images))}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("prune",))
    parser.add_argument("--image-age-hours", type=int, default=DEFAULT_IMAGE_AGE_HOURS, help="Remove only lane images created before this many hours ago")
    parser.add_argument("--cache-age-hours", type=int, default=DEFAULT_CACHE_AGE_HOURS, help="Remove only build cache unused for this many hours")
    parser.add_argument("--dry-run", action="store_true", help="List what would be removed and remove nothing")
    args = parser.parse_args()
    require(args.image_age_hours >= 1 and args.cache_age_hours >= 1, "The ages must be at least one hour")
    prune(args)
