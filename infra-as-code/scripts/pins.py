# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Reviewed dependency pins and static deployment names; nothing here discovers or upgrades a version."""
import json

from paths import INFRA, ROOT

LOCK = json.loads((INFRA / "source-lock.json").read_text())
BROWSER_NODE_VERSIONS = tuple("v" + version for version in LOCK["localDependencies"]["browserNodeVersions"])
DEXCLI_VERSION = LOCK["dex"]["cli"]
DEXCLI_COMMIT = LOCK["dex"]["cliCommit"]


def local_dependency_image(name, architecture):
    """The digest-pinned Temporal or MinIO image for one architecture."""
    pin = LOCK["localDependencies"][name]
    tag = pin["release"] if name == "minio" else pin["version"]
    return pin["repository"] + ":" + tag + "@" + pin[architecture]


def local_image(name):
    """A multi-platform image of the local lanes (traefik, registry, postgres, keycloak) pinned by its index digest."""
    pin = LOCK["localDependencies"][name]
    return pin["repository"] + ":" + pin["version"] + "@" + pin["index"]


def names_for(environment):
    """Static resource names of one environment, shared with Go and Terraform."""
    return json.loads((ROOT / "internal/platform/deploymentnames/environments.json").read_text())[environment]
