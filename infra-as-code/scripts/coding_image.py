# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Build the coding Sandbox image of a local lane under its own local tag, from a frozen context of the checkout.

Kind loads the build into its nodes; Native runs it with the local Docker Engine. This lane-neutral build does not
claim hosted gVisor validation.
"""
import hashlib
import io
import json
from pathlib import Path
import shutil
import stat
import subprocess
import tarfile
import urllib.request
import uuid

import build_inputs
from commands import run
from image_identity import docker_image_id
from operator_errors import require
from paths import ROOT
from private_files import write_private, write_private_json

AGENT_SANDBOX_REPOSITORY = "kubernetes-sigs/agent-sandbox"

# The template's sources from the frozen context, in /workspace as a new project has them: with the image's build
# cache seed restored, make generate's Go half and a build compile none of its packages outside the template module.
TEMPLATE_BUILD_CHECK = """set -euo pipefail
cp -R /opt/superverse-template-source/. /workspace/
cd /workspace
cp -R -n "$SUPERVERSE_GO_BUILD_CACHE_SEED/." "$(go env GOCACHE)/"
go mod download
go -C tools/openapi mod download
go -C tools/openapi tool ogen --target ../../internal/api/generated --package generated ../../openapi/openapi.yaml > /dev/null 2>&1
module="$(go list -m)"
compiled="$(go build -v ./... 2>&1 | grep -v -e "^${module}\\$" -e "^${module}/" || true)"
if [ -n "${compiled}" ]; then
  echo "the template build compiled packages that the image's build cache seed should hold:" >&2
  echo "${compiled}" >&2
  exit 1
fi
"""


class CodingImage:
    """One lane's build of the coding image: the local tag it builds and the private directory of its context and receipt."""

    def __init__(self, tag, directory):
        self.tag, self.directory = tag, directory
        self.source_manifest = directory / "source-manifest.json"

    def reusable(self, previous):
        """An accepted build is reused only while its source, receipt and Docker image are unchanged."""
        return bool(previous and previous["sourceFingerprint"] == build_inputs.component_fingerprint("coding") and
                    self.source_manifest.exists() and docker_image_id(self.tag) == previous["id"])

    def build(self):
        self.directory.mkdir(parents=True, exist_ok=True)
        settings = build_inputs.component_settings("coding")
        commit = settings["agentSandboxCommit"]
        # A pin-specific directory keeps an earlier accepted cache valid across an upgrade.
        agent_sandbox = self.directory / ("agent-sandbox-" + commit)
        agent_sandbox_source = fetch_external_context(AGENT_SANDBOX_REPOSITORY, commit, agent_sandbox)
        base_images = json.loads((ROOT / "internal/dexai/delivery/releases/build/build-profiles/go-react-v1/profile.json").read_text())["buildArgs"]
        context = self.directory / "context"
        receipt = build_inputs.freeze("coding", context)
        receipt["externalContexts"] = {"agent-sandbox": agent_sandbox_source}
        write_private_json(self.source_manifest, receipt)
        run(["docker", "build", "--platform", "linux/" + build_inputs.architecture(), "--target", settings["target"], "-f", context / settings["dockerfile"],
             "--build-context", "agent_sandbox=" + str(agent_sandbox),
             "--build-arg", "GO_IMAGE=" + base_images["GO_BUILD_IMAGE"], "--build-arg", "NODE_IMAGE=" + base_images["NODE_BUILD_IMAGE"],
             "--tag", self.tag, context], timeout=3600)
        receipt["imageID"] = docker_image_id(self.tag)
        write_private_json(self.source_manifest, receipt)
        self.verify_tools()

    def accepted(self):
        """The record of the current build: its tag, Docker image ID, source fingerprint and external contexts."""
        receipt = json.loads(self.source_manifest.read_text())
        require(receipt["sourceFingerprint"] == build_inputs.component_fingerprint("coding"), "Coding source changed after build")
        image_id = docker_image_id(self.tag)
        require(receipt["imageID"] == image_id, "Coding image tag changed after build")
        return {"ref": self.tag, "id": image_id, "sourceFingerprint": receipt["sourceFingerprint"],
                "externalContexts": {name: {"commit": value["commit"], "archiveSHA256": value["archiveSHA256"]} for name, value in receipt["externalContexts"].items()}}

    def verify_tools(self):
        """Run the image's tool smoke test and its template build check, each in a container this call alone owns and removes."""
        # A unique label owns only this validation container, never a port or name match.
        owner = uuid.uuid4().hex
        label = "superverse-v2.coding-verification=" + owner
        receipt = self.directory / ("verification-" + owner + ".json")
        write_private(receipt, json.dumps({"owner": owner, "image": self.tag}) + "\n")
        # The template check builds the default example's composed tree, the same archive the image seeds from.
        template = self.directory / ("verification-template-" + owner)
        template.mkdir()
        with tarfile.open(self.directory / "context" / "internal/dexai/app/apptemplate/app-template.tar") as archive:
            archive.extractall(template, filter="data")

        def owned_containers():
            ids = run(["docker", "ps", "--all", "--quiet", "--no-trunc", "--filter", "label=" + label], capture=True).splitlines()
            require(len(ids) <= 1 and all(len(value) == 64 and not set(value) - set("0123456789abcdef") for value in ids),
                    "Coding validation ownership is ambiguous; retain its receipt for reconciliation")
            return ids
        try:
            run(["docker", "run", "--rm", "--name", "sv2-coding-verification-" + owner, "--label", label, "--entrypoint", "/bin/bash", self.tag, "-lc",
                 "go version && node --version && dexcli version && temporal --version && test -x /usr/local/bin/sandboxd && test -x /usr/local/bin/superverse-previewctl"
                 " && test -x /usr/local/bin/superverse-exec && test -x /usr/local/bin/superverse-files"
                 " && test -x /usr/local/libexec/superverse/dexcli && test -x /usr/local/libexec/superverse/temporal && /usr/bin/tini --version"
                 " && test -d /var/lib/superverse/preview"],
                timeout=180)
            run(["docker", "run", "--rm", "--name", "sv2-coding-verification-" + owner + "-template", "--label", label, "--entrypoint", "/bin/bash",
                 "--volume", str(template) + ":/opt/superverse-template-source:ro", self.tag, "-c", TEMPLATE_BUILD_CHECK],
                timeout=300)
        finally:
            for container_id in owned_containers():
                try:
                    run(["docker", "rm", "--force", "--volumes", container_id], timeout=30)
                except subprocess.CalledProcessError:
                    if owned_containers():  # --rm may have raced this removal; only a fresh read settles it.
                        raise
            require(not owned_containers(), "Coding validation container cleanup is incomplete")
            shutil.rmtree(template)
            receipt.unlink()


def context_files(directory):
    result = {}
    for path in directory.rglob("*"):
        if path.is_file():
            result[path.relative_to(directory).as_posix()] = {"sha256": hashlib.sha256(path.read_bytes()).hexdigest(), "mode": format(stat.S_IMODE(path.stat().st_mode), "04o"),
                                                              **({"symlink": str(path.readlink())} if path.is_symlink() else {})}
    return result


def fetch_external_context(repository, commit, destination):
    """Return the source record of a pinned GitHub archive extracted at `destination`, fetching it when absent."""
    manifest_path = destination.parent / (destination.name + "-source.json")
    if manifest_path.exists() and destination.exists():
        recorded = json.loads(manifest_path.read_text())
        require(recorded["commit"] == commit and context_files(destination) == recorded["files"], "Pinned external build context changed: " + repository)
        return recorded
    # A cached directory without its manifest is verified against a freshly fetched immutable archive.
    data = urllib.request.urlopen(f"https://codeload.github.com/{repository}/tar.gz/{commit}", timeout=60).read()
    stage = destination.parent / (destination.name + "-verified")
    if stage.exists():
        shutil.rmtree(stage)
    stage.mkdir(parents=True)
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as archive:
        for member in archive.getmembers():
            parts = Path(member.name).parts
            if len(parts) < 2:
                continue
            member.name = str(Path(*parts[1:]))
            archive.extract(member, path=stage, filter="data")
    files = context_files(stage)
    if destination.exists():
        require(context_files(destination) == files, "Existing external context differs from the pinned archive: " + repository)
        shutil.rmtree(stage)
    else:
        stage.rename(destination)
    recorded = {"repository": repository, "commit": commit, "archiveSHA256": hashlib.sha256(data).hexdigest(), "files": files}
    manifest_path.write_text(json.dumps(recorded, sort_keys=True))
    return recorded
