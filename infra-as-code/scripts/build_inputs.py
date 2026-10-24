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
import re
import shutil
import stat

from commands import run
from operator_errors import require
from paths import INFRA, ROOT

COMPONENTS = ("server", "studio", "coding")
SCRIPTS = "infra-as-code/scripts"
APPLICATION_TEMPLATE = "internal/dexai/app/apptemplate"
EXAMPLE_CATALOG = APPLICATION_TEMPLATE + "/examples/catalog.json"
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
        # Skills are Markdown read by the Agent; the template manifests and every example's archive prewarm the
        # modules and seed the image's build caches; the BuildProfile selects the Go and Node base images.
        return (files_under("sandbox/coding") | files_under("dex-skills", documents=True) | template_cache_inputs() | evolve_measurement_sources()
                | {"scripts/install-pinned-binary.sh", "internal/dexai/delivery/releases/build/build-profiles/go-react-v1/profile.json"})
    raise ValueError("Unknown image component: " + component)


# A COPY line of the image-commands stage of sandbox/coding/Dockerfile that copies one package's Go files from the
# source tree; scripts/check-package-layers.py keeps these lines to its IMAGE_BUILT packages.
IMAGE_SOURCE_COPY = re.compile(r"^COPY (internal/\S+)/\*\.go \./\1/$")


def evolve_measurement_sources():
    """The Go files of the source-tree packages the image's superverse-evolve compiles (the goals evaluator, the review
    folder's reader, the run record and the engine's search index names), without the Dex history reader below
    runexport: every package the image-commands stage of sandbox/coding/Dockerfile copies, so the image's identity
    follows each file its build reads."""
    dockerfile = (ROOT / "sandbox/coding/Dockerfile").read_text(encoding="utf-8")
    directories = [match.group(1) for match in map(IMAGE_SOURCE_COPY.match, dockerfile.splitlines()) if match]
    require(directories, "sandbox/coding/Dockerfile copies no Go package of the source tree")
    return {path.relative_to(ROOT).as_posix() for directory in directories for path in (ROOT / directory).glob("*.go")
            if not path.name.endswith("_test.go")}


def template_cache_inputs():
    """The application template's files that its module prewarm and its Go build cache and npm cache seeds are built from.

    The seeds compile every example's composed tree, which make generate-app-template packs into the embedded
    archives, so the catalog and its examples' archives stand for every file of them; the base's module manifests
    prewarm the module graphs.
    """
    return {EXAMPLE_CATALOG, *example_archives(ROOT).values(), "app-template/go.mod", "app-template/go.sum",
            "app-template/tools/openapi/go.mod", "app-template/tools/openapi/go.sum"}


def example_archives(root):
    """The archive of each example the embedded catalog under `root` lists, by ID in catalog order: the first, the
    default, is app-template.tar and every other one examples/<id>.tar. The coding image seeds exactly these, so an
    archive the catalog does not list is no input."""
    ids = [entry["id"] for entry in json.loads((root / EXAMPLE_CATALOG).read_text())]
    require(ids and len(set(ids)) == len(ids), "The example catalog must list each example once: " + EXAMPLE_CATALOG)
    return {example: f"{APPLICATION_TEMPLATE}/app-template.tar" if index == 0 else f"{APPLICATION_TEMPLATE}/examples/{example}.tar"
            for index, example in enumerate(ids)}


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
    inputs = go_inputs("./cmd/superverse-server", target_os=platform.system().lower())
    if (ROOT / "cmd/dexengine-server/main.go").exists():
        inputs |= go_inputs("./cmd/dexengine-server", target_os=platform.system().lower())
    return inputs


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
    profiles = {"cmd/superverse-server/config/native.yaml"}
    if (ROOT / "cmd/dexengine-server/config/native.yaml").exists():
        profiles.add("cmd/dexengine-server/config/native.yaml")
    return fingerprint(common_behavior_inputs() | native_server_inputs() | script_modules("native") | profiles)


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
