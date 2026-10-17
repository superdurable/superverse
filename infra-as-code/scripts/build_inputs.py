# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Exact component build inputs and the behavioral identity of each local acceptance lane."""
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import stat

from commands import run
from operator_errors import require
from paths import INFRA, ROOT

COMPONENTS = ("server", "studio", "coding")
SCRIPTS = "infra-as-code/scripts"
EXCLUDED_DIRS = {".git", ".local", ".cache", ".next", "node_modules", "dist", "__pycache__", "test-results", "playwright-report", ".terraform"}


def files_under(relative, *, tests=False, documents=False):
    base = ROOT / relative
    require(base.exists(), "Required input is missing: " + relative)
    if base.is_file():
        return {relative}
    result = set()
    for path in base.rglob("*"):
        parts = path.relative_to(ROOT).parts
        if not path.is_file() or any(part in EXCLUDED_DIRS for part in parts):
            continue
        name = path.relative_to(ROOT).as_posix()
        if not tests and (any(part in {"tests", "test", "e2e", "__tests__"} for part in path.relative_to(base).parts) or
                          path.name.endswith(("_test.go", ".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx"))):
            continue
        # next-env.d.ts is generated, ignored by Git and rewritten differently by next dev and next build.
        if path.name in {"tsconfig.tsbuildinfo", "next-env.d.ts", ".DS_Store"} or path.name.startswith(".env"):
            continue
        if path.suffix == ".md" and "/public/" not in name and not documents:
            continue
        result.add(name)
    return result


def architecture():
    return "arm64" if platform.machine() in ("arm64", "aarch64") else "amd64"


def go_inputs(target, *, target_os="linux"):
    """Every repository file `go list` names as a dependency of one Go target."""
    output = run(["go", "list", "-deps", "-json", target], env=dict(os.environ, GOOS=target_os, GOARCH=architecture(), CGO_ENABLED="0", GOWORK="off", GOFLAGS=""), capture=True, timeout=120)
    decoder = json.JSONDecoder()
    raw, result = output, {"go.mod", "go.sum"}
    while raw.strip():
        package, end = decoder.raw_decode(raw.lstrip())
        raw = raw.lstrip()[end:]
        directory = Path(package.get("Dir", "/"))
        if not directory.is_relative_to(ROOT):
            continue
        for key in ("GoFiles", "CgoFiles", "CFiles", "HFiles", "SFiles", "EmbedFiles"):
            for name in package.get(key, []):
                result.add((directory / name).relative_to(ROOT).as_posix())
    return result


def component_inputs(component):
    if component == "server":
        return go_inputs("./cmd/superverse-server") | {"Dockerfile", ".dockerignore"}
    if component == "studio":
        return files_under("web/studio") | {"api/superverse/v2/superverse.proto", ".dockerignore"}
    if component == "coding":
        # Skills are Markdown read by the Agent; the template manifests prewarm its modules and its
        # sources seed the image's build caches; the BuildProfile selects the Go and Node base images.
        return (files_under("sandbox/coding") | files_under("dex-skills", documents=True) | template_cache_inputs()
                | {"scripts/install-pinned-binary.sh", "internal/dexai/delivery/releases/build/build-profiles/go-react-v1/profile.json"})
    raise ValueError("Unknown image component: " + component)


def template_cache_inputs():
    """The application template's files that its module prewarm and its Go build cache and npm cache seeds are built from.

    The seeds compile the default example's composed tree, which make generate-app-template packs into the embedded
    archive, so the archive stands for every file of it; the base's module manifests prewarm the module graphs.
    """
    return {"internal/dexai/app/apptemplate/app-template.tar", "app-template/go.mod", "app-template/go.sum",
            "app-template/tools/openapi/go.mod", "app-template/tools/openapi/go.sum"}


def fingerprint(names):
    digest = hashlib.sha256()
    for name in sorted(names):
        path = ROOT / name
        require(path.is_file() and not path.is_symlink(), "Build inputs must be present regular files: " + name)
        digest.update(name.encode() + b"\0" + format(stat.S_IMODE(path.stat().st_mode), "04o").encode() + b"\0" + path.read_bytes() + b"\0")
    return digest.hexdigest()


def component_settings(component):
    return json.loads((INFRA / "build-inputs.json").read_text())[component]


def component_identity(component, names):
    value = {"source": fingerprint(names), "build": component_settings(component), "platform": "linux/" + architecture()}
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def component_fingerprint(component):
    return component_identity(component, component_inputs(component))


def app_manifest_tool_inputs():
    return go_inputs("./cmd/tools/app-manifest", target_os=platform.system().lower())


def native_server_inputs():
    return go_inputs("./cmd/superverse-server", target_os=platform.system().lower())


def script_modules(*packages):
    """Every shared top-level script module plus whole lane packages, so no module can drop out by name."""
    shared = {f"{SCRIPTS}/{path.name}" for path in (ROOT / SCRIPTS).glob("*.py")}
    return shared.union(*(files_under(f"{SCRIPTS}/{package}") for package in packages))


def common_behavior_inputs():
    """Inputs exercised by both local lanes. Native renders its coding profile from the same chart and Local Kind values."""
    names = component_inputs("studio") | component_inputs("coding")
    names |= files_under("infra-as-code/kubernetes") | files_under("infra-as-code/environments/local-kind")
    names |= files_under("web/studio/tests/e2e", tests=True) | files_under("internal/dexai/delivery/releases/build/localbuilder")
    names |= app_manifest_tool_inputs() | script_modules("acceptance")
    names |= {"web/studio/playwright.e2e.config.ts", "infra-as-code/sv2", "infra-as-code/build-inputs.json",
              "infra-as-code/source-lock.json", "infra-as-code/validation-requirements.txt"}
    # The coverage map is what an E2E run writes, not what it runs.
    return names - {"web/studio/tests/e2e/coverage-map.json"}


def common_behavior_fingerprint():
    return fingerprint(common_behavior_inputs())


def kind_behavior_fingerprint():
    """Hosted Terraform, HCP and release sources are bound by their reviewed plan instead."""
    return fingerprint(common_behavior_inputs() | component_inputs("server") | script_modules("kind") | files_under("infra-as-code/dependencies/published"))


def native_behavior_fingerprint():
    """The host-OS server, the Native launcher with its Compose file and the Native profile."""
    return fingerprint(common_behavior_inputs() | native_server_inputs() | script_modules("native") | {"cmd/superverse-server/config/native.yaml"})


def freeze(component, destination):
    names = component_inputs(component)
    before = component_identity(component, names)
    if destination.exists():
        shutil.rmtree(destination)
    destination.mkdir(parents=True)
    manifest = {}
    for name in sorted(names):
        output = destination / name
        output.parent.mkdir(parents=True, exist_ok=True)
        source = ROOT / name
        mode = stat.S_IMODE(source.stat().st_mode)
        shutil.copyfile(source, output)
        output.chmod(mode)
        manifest[name] = {"sha256": hashlib.sha256(output.read_bytes()).hexdigest(), "mode": format(mode, "04o")}
    require(component_identity(component, names) == before, "Component inputs changed while freezing: " + component)
    return {"component": component, "sourceFingerprint": before, "build": component_settings(component), "platform": "linux/" + architecture(), "files": manifest}
