# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Fixed repository locations and the private local state directory."""
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
INFRA = ROOT / "infra-as-code"
CHART = INFRA / "kubernetes/charts/superverse-platform"
LOCAL_KIND_VALUES = INFRA / "environments/local-kind/values.yaml"
# The repository's main checkout. Its local lanes are shared by every worktree: the Kind
# cluster belongs to the repository, and each worktree builds and deploys its own source.
MAIN_CHECKOUT = Path(subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--path-format=absolute", "--git-common-dir"],
                                    check=True, capture_output=True, text=True).stdout.strip()).parent
# Ignored, owner-only state of the local lanes; never uploaded or committed.
STATE = MAIN_CHECKOUT / ".local/superverse-v2"


def project_env_value(key):
    """A value of the main checkout's ignored .env, which every worktree shares; None when absent."""
    path = MAIN_CHECKOUT / ".env"
    if not path.is_file():
        return None
    for line in path.read_text().splitlines():
        name, separator, value = line.partition("=")
        if separator and name.strip() == key:
            return value.strip() or None
    return None
