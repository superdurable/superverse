# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""The server configuration of a Kind deploy: checked-in profile layers merged into one document.

The profiles are the files next to cmd/superverse-server/config/config.go, which the server and `make check-config`
load with the same layering: local-kind.yaml first, then the chosen profile over it. A deploy merges the values it
computes into the document and the chart renders the result as the server ConfigMap; secrets are never part of it.
"""
from pathlib import Path

import yaml

from config_files import DIRECTORY, file_digest, merge, preflight_files, read_layer
from operator_errors import require
from paths import ROOT, STATE
from private_files import write_private

BASE = DIRECTORY / "local-kind.yaml"
# The only profile whose receipt authorizes publication, deployed from exactly the checked-in files. It offers the mock model
# and the Claude models: which one the acceptance runs is the platform developer's decision.
ACCEPTANCE = "local-kind-acceptance"
PREFLIGHT = STATE / "server-config.yaml"
# The developer's decision, named before the run in this variable of `kind e2e`, which the browser specs read too: `mock`, or a Claude model.
MODEL_TARGET = "SUPERVERSE_E2E_MODEL_TARGET"
MOCK_TARGET = "mock"
MODEL_TARGETS = (MOCK_TARGET, "anthropic/claude-opus-5-5", "anthropic/claude-sonnet-5-5", "anthropic/claude-haiku-4-5")


def profile_layers(profile):
    """local-kind.yaml, and the chosen profile over it unless it is local-kind.yaml itself."""
    path = Path(profile).resolve()
    require(path.is_file() and path.suffix == ".yaml" and path.parent == DIRECTORY.resolve(),
            "A server configuration profile is a .yaml file in " + str(DIRECTORY.relative_to(ROOT)) + ": " + str(profile))
    return [BASE] if path == BASE.resolve() else [BASE, path]


def profile_record(profile):
    """The profile name and the digests of the layers it is made of, as the deploy records them."""
    layers = profile_layers(profile)
    return {"profile": layers[-1].stem, "files": [{"name": layer.name, "sha256": file_digest(layer)} for layer in layers]}


def load_profile(profile):
    """The merged document of a profile and its record."""
    document = {}
    for layer in profile_layers(profile):
        document = merge(document, read_layer(layer))
    return document, profile_record(profile)


def preflight(document):
    """The final document of a deploy, validated as the server will validate it."""
    write_private(PREFLIGHT, yaml.safe_dump(document))
    preflight_files(PREFLIGHT)


def require_kind_profile(document):
    """A deployed profile configures the Local Kind server: the Kubernetes runtime, never Local Native's Docker runtime."""
    require(document.get("localKind") is True and document.get("runtime", "kubernetes") == "kubernetes" and "native" not in document,
            "kind deploy accepts only a Local Kind profile: it must set localKind, run the kubernetes runtime and have no native section; "
            "Local Native runs native.yaml with `native start`")


def require_unchanged(record):
    """The checked-in layers a deployment recorded still have the digests it recorded."""
    require(all(DIRECTORY.joinpath(layer["name"]).is_file() and file_digest(DIRECTORY / layer["name"]) == layer["sha256"] for layer in record["files"]),
            "A server configuration profile changed since the deploy; redeploy before acceptance")


def model_target(config, environment):
    """The model target of a browser run on a deployment: required for the acceptance profile, optional elsewhere (None when unset)."""
    target = environment.get(MODEL_TARGET, "")
    require(target or config["profile"] != ACCEPTANCE,
            "The final acceptance runs the model the platform developer decides: ask the developer whether it runs the mock model or a real "
            "Claude model, and which, never choose it yourself, then set " + MODEL_TARGET + " to one of " + ", ".join(MODEL_TARGETS) + " and run kind e2e again")
    if target:
        require_offered_target(target, config["document"], MODEL_TARGET)
    return target or None


def require_offered_target(target, document, name):
    """A model target the deployed server can run: one of MODEL_TARGETS, and the mock model only where the server offers it."""
    require(target in MODEL_TARGETS, name + " must be one of " + ", ".join(MODEL_TARGETS) + ": " + repr(target))
    require(target != MOCK_TARGET or document.get("agent", {}).get("mockModel") is True,
            name + " is mock, but the deployed server does not offer the mock model (agent.mockModel)")
