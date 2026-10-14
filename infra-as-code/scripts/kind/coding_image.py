# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The Kind lane's coding Sandbox image: the shared build under the Kind tag, loaded into every Kind node."""
from coding_image import CodingImage
from commands import run
from kind.cluster import CLUSTER
from kind.node_images import CODING_REPOSITORY
from paths import STATE

CODING = CodingImage(CODING_REPOSITORY + ":local", STATE / "sandbox-build")


def load():
    """Load the accepted build into every Kind node; return its record for the caller to alias."""
    record = CODING.accepted()
    run(["kind", "load", "docker-image", CODING.tag, "--name", CLUSTER], timeout=600)
    return record
