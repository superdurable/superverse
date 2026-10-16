# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Exact OCI descriptor, platform manifest and config identities, and their Docker counterparts."""
import hashlib
import json
import re
import subprocess

from commands import run
from operator_errors import require

DIGEST = re.compile(r"sha256:[a-f0-9]{64}\Z")
INDEX_TYPES = {"application/vnd.oci.image.index.v1+json", "application/vnd.docker.distribution.manifest.list.v2+json"}
MANIFEST_TYPES = {"application/vnd.oci.image.manifest.v1+json", "application/vnd.docker.distribution.manifest.v2+json"}
CONFIG_TYPES = {"application/vnd.oci.image.config.v1+json", "application/vnd.docker.container.image.v1+json"}
# BuildKit gives an attestation manifest the OCI empty descriptor ({}) as its config; an image config never is.
ATTESTATION_CONFIG_TYPES = CONFIG_TYPES | {"application/vnd.oci.empty.v1+json"}
MAX_METADATA = 16 * 1024 * 1024
# The runtime fields a Docker image must share with the platform config blob it was accepted as.
RUNTIME_CONFIG_KEYS = ("Env", "Entrypoint", "Cmd", "WorkingDir", "User", "Labels")


def descriptor(value):
    require(isinstance(value, dict) and DIGEST.fullmatch(value.get("digest", "")) and
            type(value.get("size")) is int and value["size"] > 0 and isinstance(value.get("mediaType"), str),
            "Invalid image content descriptor")
    return {key: value[key] for key in ("digest", "size", "mediaType")}


def graph(read, root_digest, platform):
    """Verify raw OCI descriptors, the platform manifest and attestation metadata.

    `read(digest, kind)` returns the exact bytes of one manifest or blob from any store.
    """
    def content(expected, kind):
        require(DIGEST.fullmatch(expected["digest"]), "Invalid image content digest")
        require(expected.get("size", 0) <= MAX_METADATA, "Image metadata descriptor exceeds its bound")
        raw = read(expected["digest"], kind)
        require(len(raw) <= MAX_METADATA and hashlib.sha256(raw).hexdigest() == expected["digest"][7:] and
                ("size" not in expected or len(raw) == expected["size"]), "Image metadata bytes differ from their descriptor")
        value = json.loads(raw)
        require(isinstance(value, dict), "Image metadata must be a JSON object")
        return value, len(raw)

    root, size = content({"digest": root_digest}, "manifest")
    root_ref = descriptor({"digest": root_digest, "size": size, "mediaType": root.get("mediaType")})
    require(root.get("schemaVersion") == 2, "Unsupported image schema")
    attestations = []
    if root_ref["mediaType"] in INDEX_TYPES:
        children = root.get("manifests", [])
        require(isinstance(children, list) and 1 <= len(children) <= 16, "Image index exceeds its child bound")
        selected = [c for c in children if c.get("platform", {}).get("os", "") + "/" + c.get("platform", {}).get("architecture", "") == platform]
        require(len(selected) == 1, "Image index must have one exact image platform")
        platform_manifest_ref = descriptor(selected[0])
        for child in children:
            if child is selected[0]:
                continue
            annotations = child.get("annotations", {})
            require(child.get("platform") == {"architecture": "unknown", "os": "unknown"} and
                    annotations.get("vnd.docker.reference.type") == "attestation-manifest" and
                    annotations.get("vnd.docker.reference.digest") == platform_manifest_ref["digest"],
                    "Only attestations for the selected platform manifest may accompany an image index")
            attestations.append(descriptor(child))
        require(len({descriptor(c)["digest"] for c in children}) == len(children), "Duplicate image index children")
        platform_manifest, _ = content(platform_manifest_ref, "manifest")
    else:
        platform_manifest_ref, platform_manifest = root_ref, root
    require(platform_manifest_ref["mediaType"] in MANIFEST_TYPES and platform_manifest.get("mediaType") == platform_manifest_ref["mediaType"] and
            platform_manifest.get("schemaVersion") == 2, "Unsupported platform image manifest")
    config_ref = descriptor(platform_manifest.get("config"))
    require(config_ref["mediaType"] in CONFIG_TYPES, "Unsupported image config type")
    config, _ = content(config_ref, "blob")
    require(config.get("os", "") + "/" + config.get("architecture", "") == platform, "Image config platform differs from accepted platform")
    layers = platform_manifest.get("layers", [])
    require(isinstance(layers, list) and 1 <= len(layers) <= 256, "Image layers exceed their bound")
    for layer in layers:
        descriptor(layer)  # Compressed layer identities are bound by the hashed manifest.
    for attestation_ref in attestations:
        attestation, _ = content(attestation_ref, "manifest")
        require(attestation_ref["mediaType"] in MANIFEST_TYPES and attestation.get("schemaVersion") == 2 and
                attestation.get("mediaType") == attestation_ref["mediaType"], "Unsupported attestation manifest")
        attestation_config = descriptor(attestation.get("config"))
        require(attestation_config["mediaType"] in ATTESTATION_CONFIG_TYPES, "Unsupported attestation config")
        content(attestation_config, "blob")
        attestation_layers = attestation.get("layers", [])
        require(isinstance(attestation_layers, list) and 1 <= len(attestation_layers) <= 16, "Attestation content exceeds its bound")
        for layer in attestation_layers:
            ref = descriptor(layer)
            require(ref["mediaType"] == "application/vnd.in-toto+json", "Unexpected attestation content type")
            content(ref, "blob")
    return {"descriptor": root_ref, "platformManifest": platform_manifest_ref, "config": config_ref,
            "platform": platform, "attestations": sorted(attestations, key=lambda a: a["digest"])}, config


def validate(identity):
    """Check the shape of a recorded identity before trusting it as evidence."""
    require(isinstance(identity, dict) and set(identity) == {"descriptor", "platformManifest", "config", "platform", "attestations"},
            "Image evidence lacks a complete descriptor/platform/config mapping")
    root, manifest, config = [descriptor(identity.get(key)) for key in ("descriptor", "platformManifest", "config")]
    require(root["mediaType"] in INDEX_TYPES | MANIFEST_TYPES and manifest["mediaType"] in MANIFEST_TYPES and
            config["mediaType"] in CONFIG_TYPES and identity["platform"] in ("linux/arm64", "linux/amd64"), "Unsupported accepted image types/platform")
    require(isinstance(identity["attestations"], list) and len(identity["attestations"]) <= 15, "Invalid accepted attestation children")
    for child in identity["attestations"]:
        require(descriptor(child)["mediaType"] in MANIFEST_TYPES, "Invalid accepted attestation manifest")
    if root["mediaType"] in MANIFEST_TYPES:
        require(root == manifest and not identity["attestations"], "Single-manifest image evidence has index-only fields")
    return identity


def docker_image_id(reference):
    """Docker's image ID for a reference, or None when Docker does not hold it."""
    try:
        return run(["docker", "image", "inspect", reference, "--format", "{{.Id}}"], capture=True)
    except subprocess.CalledProcessError:
        return None


def verify_docker_root(docker_id, identity, config=None):
    """An exact Docker root descriptor binds all accepted content without a rebuild.

    With the platform config blob, the Docker filesystem and runtime config are compared too.
    """
    observed = json.loads(run(["docker", "image", "inspect", docker_id], capture=True))[0]
    require(observed.get("Id") == docker_id and observed.get("Os", "") + "/" + observed.get("Architecture", "") == identity["platform"],
            "Accepted Docker image is unavailable or changed")
    if observed.get("Descriptor"):
        require(descriptor(observed["Descriptor"]) == identity["descriptor"], "Docker root descriptor differs from the accepted content")
    else:
        require(docker_id == identity["config"]["digest"], "Docker config ID differs from the accepted content")
    if config is not None:
        require_docker_config(config, observed, "Docker filesystem/config differs from the accepted platform manifest")


def require_docker_config(config, observed, message):
    """Docker's inspected layers and runtime config equal those of an image's platform config blob."""
    require(config.get("rootfs", {}).get("diff_ids") == observed.get("RootFS", {}).get("Layers") and
            all(config.get("config", {}).get(key) == observed.get("Config", {}).get(key) for key in RUNTIME_CONFIG_KEYS), message)
