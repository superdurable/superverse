# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Server configuration files: the checked-in profiles next to cmd/superverse-server/config/config.go and their layering.

The server and `make check-config` load a list of YAML files in order, each over the previous one. Both lanes merge
documents the way the loader does and validate their final layers with the server's own loader before anything starts;
secrets are never part of a configuration file.
"""
import copy
import hashlib
import os
from pathlib import Path
import subprocess

import yaml

from operator_errors import require
from paths import ROOT

DIRECTORY = ROOT / "cmd/superverse-server/config"


def read_layer(path):
    """One YAML layer as a mapping."""
    document = yaml.safe_load(Path(path).read_text())
    require(document is None or isinstance(document, dict), Path(path).name + " must be a YAML mapping")
    return document or {}


def merge(base, overlay):
    """`overlay` over `base`, section by section, the way the server's loader applies a later file."""
    result = copy.deepcopy(base)
    for key, value in overlay.items():
        result[key] = merge(result[key], value) if isinstance(value, dict) and isinstance(result.get(key), dict) else copy.deepcopy(value)
    return result


def file_digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def preflight_files(*paths):
    """Load and validate configuration files, layered in order, with the server's own loader before anything is applied."""
    result = subprocess.run(["go", "run", "./cmd/tools/check-config", *map(str, paths)], cwd=ROOT, capture_output=True, text=True, timeout=600,
                            env=dict(os.environ, GOWORK="off", GOFLAGS="", CGO_ENABLED="0"), check=False)
    # `go run` appends its own exit line to the violations.
    require(result.returncode == 0, "The server configuration is invalid:\n" + "\n".join(line for line in result.stderr.splitlines() if line != "exit status 1"))


def includes(rendered, expected):
    """Every key of `expected` has the same value in `rendered`, section by section."""
    return all(isinstance(rendered.get(key), dict) and includes(rendered[key], value) if isinstance(value, dict) else rendered.get(key) == value
               for key, value in expected.items())
