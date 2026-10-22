# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Build, load and record the exact component and dependency images of the owned Kind cluster.

Images go straight from Docker into the Kind nodes: no registry, mirror or push.
"""
import json

import build_inputs
from commands import run
from image_identity import DIGEST, docker_image_id, require_docker_config
from kind import coding_image
from kind.cluster import CLUSTER, kind_nodes, save, verify_owner
from kind.node_images import CODING_REPOSITORY, ctr, loaded_manifest_digest, observe
from operator_errors import require
from paths import STATE
from pins import local_dependency_image
from private_files import write_private_json

# The MinIO image the current local cluster was created with before the pin moved to
# ghcr.io/coollabsio; kind up never reselects an existing cluster's dependency.
ACCEPTED_LEGACY_MINIO = "quay.io/minio/minio:RELEASE.2025-07-23T15-54-02Z@sha256:d249d1fb6966de4d8ad26c04754b545205ff15a62e4fd19ebd0f26fa5baacbc0"


def build(args):
    value = verify_owner()
    for name in build_inputs.COMPONENTS if args.component == "all" else (args.component,):
        if name == "coding":
            if not coding_image.CODING.reusable(value["images"].get(name)):
                coding_image.CODING.build()
            loaded = coding_image.load()
            record_image(value, name, dict(loaded, manifestRef=alias_loaded_image(name, loaded["ref"], loaded["id"], CODING_REPOSITORY)))
            continue
        previous = value["images"].get(name)
        if previous and previous["sourceFingerprint"] == build_inputs.component_fingerprint(name) and docker_image_id(previous["ref"]) == previous["id"]:
            run(["kind", "load", "docker-image", previous["ref"], "--name", CLUSTER], timeout=600)
            record_image(value, name, dict(previous, manifestRef=alias_loaded_image(name, previous["ref"], previous["id"])))
            print("Reused unchanged component:", name)
            continue
        context = STATE / "build" / name / "context"
        receipt = build_inputs.freeze(name, context)
        image = f"superverse-v2/{name}:local-{receipt['sourceFingerprint'][:16]}"
        settings = build_inputs.component_settings(name)
        node_image = ["--build-arg", "NODE_IMAGE=" + settings["nodeImage"]] if "nodeImage" in settings else []
        run(["docker", "build", "--platform", "linux/" + build_inputs.architecture(), "-f", context / settings["dockerfile"], "-t", image, *node_image, context], timeout=3600)
        image_id = docker_image_id(image)
        run(["kind", "load", "docker-image", image, "--name", CLUSTER], timeout=600)
        receipt.update({"ref": image, "id": image_id, "manifestRef": alias_loaded_image(name, image, image_id)})
        write_private_json(STATE / "build" / name / "receipt.json", receipt)
        record_image(value, name, {key: receipt[key] for key in ("ref", "id", "sourceFingerprint", "manifestRef")})
    print("Built and loaded selected V2 components; no registry publication.")


def record_image(value, name, record):
    """A build record is not deployment evidence, even with identical source."""
    record["identity"] = observe(name, record)
    previous = value["images"].get(name, {})
    if any(previous.get(key) != record.get(key) for key in ("id", "identity", "sourceFingerprint", "manifestRef")):
        for key in ("deployedSourceFingerprint", "runtimeIdentity", "deployedImages"):
            value.pop(key, None)
    value["images"][name] = record
    save(value)


def alias_loaded_image(name, image, docker_id, repository=None):
    """Alias the Kind-loaded manifest of a Docker image by digest under `repository`; return the alias.

    The repository defaults to docker.io/superverse-v2/live-source-<name>; the coding profile names
    CODING_REPOSITORY. The alias is the loaded manifest digest, never a different registry digest.
    Third-party images may be loaded as multi-platform indexes, so only the selected platform's
    filesystem and runtime config are compared with Docker.
    """
    repository = repository or "docker.io/superverse-v2/live-source-" + name
    accepted = json.loads(run(["docker", "image", "inspect", image], capture=True))[0]
    require(accepted.get("Id") == docker_id, "Local image changed since acceptance: " + name)
    reference = image if image.startswith(("docker.io/", "ghcr.io/", "public.ecr.aws/")) else "docker.io/" + image
    aliases = set()
    for node in kind_nodes():
        manifest_digest = loaded_manifest_digest(node, reference)
        manifest = json.loads(ctr(node, "content", "get", manifest_digest, capture=True))
        if "manifests" in manifest:
            selected = [m for m in manifest["manifests"] if m.get("platform", {}).get("architecture") == build_inputs.architecture() and m.get("platform", {}).get("os") == "linux"]
            require(len(selected) == 1, "Loaded image platform is ambiguous")
            manifest = json.loads(ctr(node, "content", "get", selected[0]["digest"], capture=True))
        config = json.loads(ctr(node, "content", "get", manifest.get("config", {}).get("digest", ""), capture=True))
        require_docker_config(config, accepted, "Loaded image filesystem/config does not match accepted source: " + name)
        alias = repository + "@" + manifest_digest
        ctr(node, "images", "tag", "--force", reference, alias)
        aliases.add(alias)
    require(len(aliases) == 1, "Kind nodes disagree about " + name)
    return aliases.pop()


def load_dependency(name, reference):
    """Load a pinned third-party dependency under a local tag named by its Docker image ID; return its record."""
    # Old released images may stay in a verified local Docker cache after the registry stops serving them.
    canonical = reference.split(":", 1)[0] + "@" + reference.split("@", 1)[1]
    identity = docker_image_id(canonical)
    if identity is None:
        run(["docker", "pull", reference], timeout=900)
        canonical = reference
        identity = docker_image_id(canonical)
    local = f"superverse-v2/dependency-{name}:{identity.removeprefix('sha256:')[:16]}"
    run(["docker", "tag", canonical, local])
    archive = STATE / ("dependency-" + name + ".tar")
    try:
        run(["docker", "image", "save", "--platform", "linux/" + build_inputs.architecture(), "-o", archive, local], timeout=600)
        run(["kind", "load", "image-archive", archive, "--name", CLUSTER], timeout=600)
    finally:
        archive.unlink(missing_ok=True)
    return {"source": reference, "image": local, "id": identity}


def dependency_reference(name, record):
    """Verify the cached dependency and name its loaded digest in its canonical repository."""
    # Kubelet 1.35 tracks pull credentials by image ID and repository. After a real pull,
    # the same bytes under a synthetic repository may require a nonexistent registry pull.
    repository = record["source"].split("@", 1)[0].rsplit(":", 1)[0]
    return alias_loaded_image(name, record["image"], record["id"], repository)


def resolve_project_dependency_images(value, project_dex_image):
    """Project Dex, Temporal and storage images for project scopes, verified on every Kind node."""
    require(project_dex_image, "The reviewed dependency approval must name the project Dex engine image")
    architecture = build_inputs.architecture()
    run(["docker", "pull", "--platform", "linux/" + architecture, project_dex_image], timeout=900)
    pulled = json.loads(run(["docker", "image", "inspect", project_dex_image], capture=True))[0]
    require(DIGEST.fullmatch(pulled.get("Id", "")) and pulled.get("Os") == "linux" and pulled.get("Architecture") == architecture,
            "Project Dex engine image identity/platform changed")
    # `kind load` would import every manifest of the multi-architecture index and fail on the
    # missing foreign-platform blobs, so each node pulls the approved index for its own platform.
    for node in kind_nodes():
        ctr(node, "images", "pull", "--platform", "linux/" + architecture, project_dex_image, timeout=900, capture=True)
        require(loaded_manifest_digest(node, project_dex_image) == project_dex_image.split("@", 1)[1],
                "Kind node did not retain the approved project Dex digest")
    minio = value.get("dependencies", {}).get("minio")
    require(minio and minio["source"] in (local_dependency_image("minio", architecture), ACCEPTED_LEGACY_MINIO),
            "No accepted pinned MinIO dependency; run up")
    require(docker_image_id(minio["image"]) is not None, f"The recorded MinIO dependency image {minio['image']} is missing from Docker; run up, which restores it")
    return {
        "dexImage": project_dex_image,
        "temporalImage": local_dependency_image("temporal", architecture),
        "s3Image": dependency_reference("minio", minio),
    }
