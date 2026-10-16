# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The coding image that Native Sandboxes run, and the coding profile the server runs them from.

The image is the shared build of the checkout's coding source under the Native tag; the engine runs it by the digest its
containerd image store records, so no registry or Kind load is involved. The profile is the chart's own rendering of the
coding profile, the immutable ConfigMap that Kind installs, written where the server reads it; the Docker runtime runs
its Pod template's container (internal/dexai/agents/sandbox/docker).
"""
import hashlib
import json
import re

import yaml

from commands import run
from operator_errors import require
from paths import CHART, LOCAL_KIND_VALUES
from pins import names_for
from private_files import write_private, write_private_json

REPOSITORY = "docker.io/superverse-v2/coding-sandbox"
PROFILE_NAME = re.compile(r"coding-sandbox-profile-coding-v4-[0-9a-f]{12}")
# The chart renders its other local dependencies only with images; the coding profile does not use them.
SHAPE = "@sha256:" + "0" * 64


def prepare(instance):
    """Build the coding image when its source changed, then render its profile; return the image record and the profile name."""
    previous = json.loads(instance.coding_record.read_text()) if instance.coding_record.exists() else None
    if not instance.coding.reusable(previous):
        instance.coding.build()
    record = instance.coding.accepted()
    write_private_json(instance.coding_record, {"id": record["id"], "sourceFingerprint": record["sourceFingerprint"]})
    record["reference"] = engine_reference(instance)
    name, document = render_profile(record["reference"])
    write_private(instance.profiles / (name + ".yaml"), document)
    return record, name


def engine_reference(instance):
    """The coding image pinned by the one digest that the engine records for the Native tag's repository."""
    found = json.loads(run(["docker", "image", "inspect", instance.coding.tag, "--format", "{{json .RepoDigests}}"], capture=True)) or []
    digests = {entry.split("@", 1)[1] for entry in found if entry.split("@", 1)[0] in (REPOSITORY, REPOSITORY.removeprefix("docker.io/"))}
    require(len(digests) == 1, "The engine records no digest of the coding image. Local Native runs it by that digest: use the engine's containerd "
                               "image store (Docker Desktop: Use containerd for pulling and storing images; OrbStack records digests by default)")
    return REPOSITORY + "@" + digests.pop()


def render_profile(image):
    """The chart's coding profile ConfigMap for image, and its name."""
    repository, digest = image.split("@", 1)
    rendered = run(["helm", "template", "superverse-v2", CHART, "--namespace", names_for("test")["platform_namespace"], "-f", LOCAL_KIND_VALUES,
                    "--set", "server.enabled=false", "--set", "studio.enabled=false",
                    "--set-string", "localDependencies.temporalImage=temporal" + SHAPE, "--set-string", "localDependencies.minioImage=minio" + SHAPE,
                    "--set-string", "localDependencies.postgresImage=postgres" + SHAPE, "--set-string", "localDependencies.keycloakImage=keycloak" + SHAPE,
                    "--set", "sandboxProfiles.codingProfile.enabled=true", "--set-string", "sandboxProfiles.codingProfile.image.repository=" + repository,
                    "--set-string", "sandboxProfiles.codingProfile.image.digest=" + digest, "--show-only", "templates/coding-profile.yaml"], capture=True, timeout=120)
    profiles = [document for document in rendered.split("\n---\n") if (yaml.safe_load(document) or {}).get("kind") == "ConfigMap"]
    require(len(profiles) == 1, "The chart did not render one coding profile")
    name = yaml.safe_load(profiles[0])["metadata"]["name"]
    require(PROFILE_NAME.fullmatch(name), "The chart rendered an unexpected coding profile name")
    return name, profiles[0].strip() + "\n"


def profile_digest(instance, name):
    """The SHA-256 of the rendered profile the server reads."""
    return hashlib.sha256((instance.profiles / (name + ".yaml")).read_bytes()).hexdigest()
