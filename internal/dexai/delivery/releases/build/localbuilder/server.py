#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Authenticated local build service of Local Kind and Local Native; no public business or generic command API."""
from __future__ import annotations
import argparse
import contextlib
from datetime import datetime, timezone
import hashlib
import hmac
import http.server
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request


# Dex reports a Step group that is neither one run nor one choice as this warning; it never blocks.
ADVISORY_DIAGNOSTICS = frozenset({"v2_step_group_shape"})


def blocking_diagnostics(graph):
    return [d for d in graph.get("diagnostics") or []
            if not (d.get("severity") == "warning" and d.get("code") in ADVISORY_DIAGNOSTICS)]


ROOT = Path(__file__).resolve().parents[6]
# The main checkout's private state, shared by every worktree (infra-as-code/scripts/paths.py).
MAIN_CHECKOUT = Path(subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--path-format=absolute", "--git-common-dir"],
                                    check=True, capture_output=True, text=True).stdout.strip()).parent
# The private state of the lane's builds: the Kind lane's by default; Native passes its own with --state-dir.
STATE = MAIN_CHECKOUT / ".local/superverse-v2/release-builds"
CLUSTER = "superverse-v2"
# Where a build publishes its image: into the owned Kind cluster's nodes, or the Native lane's local registry.
TARGETS = ("kind", "docker")
# The manifest media types a registry may hold under a tag: an index of platform images, or one image's manifest.
MANIFEST_TYPES = ("application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, "
                  "application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json")
REGISTRY = re.compile(r"^localhost:([0-9]{1,5})$")
MAX_BODY = 16 * 1024
MAX_ARTIFACT = 32 * 1024 * 1024
DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
BUILD_REVISION = re.compile(r"^build-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")
MAX_CLEANUP_REVISIONS = 64
# The trusted recipes by version: bp1, and the local recipe bp3, which also takes the module redirect.
PROFILES = {"bp1": "internal/dexai/delivery/releases/build/build-profiles/go-react-v1",
            "bp3": "internal/dexai/delivery/releases/build/build-profiles/go-react-v1/local"}
# A module redirect: a module path, and the clone URL a container of this machine fetches its repository from.
MODULE_PATH = re.compile(r"[a-z0-9-]+(?:\.[a-z0-9-]+)+(?:/[A-Za-z0-9._-]+)+")
CLONE_URL = re.compile(r"(?:https://[A-Za-z0-9.-]+|http://host\.docker\.internal)(?::[0-9]+)?(?:/[A-Za-z0-9._-]+)+\.git")
SOURCE_TIMEOUT = 240
# Includes cold Docker container startup; source-only work still shares SOURCE_TIMEOUT.
DEPENDENCY_PREPARATION_TIMEOUT = 600
SDK_METADATA_LIMIT = 1024 * 1024
DEPENDENCY_PREPARATION_SCRIPT = r'''# Fixed Go commands only: no project script or application code executes.
# Download failures still emit declaration metadata so invalid/local replacements
# retain their application rejection instead of becoming dependency failures.
download=0
go -C /workspace mod download >&2 || download=$?
if [ "$download" -eq 0 ]; then
    go -C /workspace/tools/openapi mod download >&2 || download=$?
fi
declared=0
env GOPROXY=off GOSUMDB=off GOVCS='*:off' GOFLAGS=-mod=readonly GOTOOLCHAIN=local \
    go mod edit -json >/tmp/sv2-sdk-declared.json || declared=$?
resolved=125
: >/tmp/sv2-sdk-resolved.json
if [ "$download" -eq 0 ] && [ "$declared" -eq 0 ]; then
    resolved=0
    env GOPROXY=off GOSUMDB=off GOVCS='*:off' GOFLAGS=-mod=readonly GOTOOLCHAIN=local \
        go list -m -json -e -mod=readonly github.com/superdurable/dex/sdk-go \
        >/tmp/sv2-sdk-resolved.json || resolved=$?
fi
printf 'SV2_SDK_METADATA_V1 %s %s %s\n' "$download" "$declared" "$resolved"
head -c 1048577 /tmp/sv2-sdk-declared.json
printf '\nSV2_SDK_RESOLVED_V1\n'
head -c 1048577 /tmp/sv2-sdk-resolved.json
printf '\nSV2_SDK_END_V1\n'
'''


def dependency_metadata(output):
    """Read the fixed protocol; never retain or log dependency metadata."""
    if isinstance(output, bytes):
        output = output.decode()
    output = output.rstrip("\n")
    if len(output.encode()) > 2 * SDK_METADATA_LIMIT + 256:
        raise ValueError("dependency metadata exceeds its bound")
    header, body = output.split("\n", 1)
    match = re.fullmatch(r"SV2_SDK_METADATA_V1 ([0-9]{1,3}) ([0-9]{1,3}) ([0-9]{1,3})", header)
    if match is None or not body.endswith("\nSV2_SDK_END_V1"):
        raise ValueError("invalid dependency metadata protocol")
    declared, resolved = body[:-len("\nSV2_SDK_END_V1")].split("\nSV2_SDK_RESOLVED_V1\n")
    if any(len(value.encode()) > SDK_METADATA_LIMIT for value in (declared, resolved)):
        raise ValueError("dependency metadata exceeds its bound")
    return tuple(map(int, match.groups())), declared, resolved

MAX_FAILURE_STDERR_BYTES = 2048
# The job log keeps the end of the build commands' output for log-read pages of at most LOG_PAGE bytes.
MAX_JOB_LOG = 256 * 1024
LOG_PAGE = 64 * 1024
# A log read names its absolute byte offset, or asks for the log's end, in these headers.
LOG_OFFSET_HEADER, LOG_TAIL_HEADER, LOG_LIMIT_HEADER = "X-Superverse-Log-Offset", "X-Superverse-Log-Tail", "X-Superverse-Log-Limit"
# Held while a job log and its start offset change or are read together.
JOB_LOG_LOCK = threading.Lock()


def encoded(value):
    return json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode()


def private_json(path, value, *, durable=False):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_suffix(".tmp")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "wb") as output:
        output.write(encoded(value))
        if durable:
            output.flush()
            os.fsync(output.fileno())
    temporary.chmod(0o600)
    os.replace(temporary, path)
    if durable:
        descriptor = os.open(path.parent, os.O_RDONLY)
        try: os.fsync(descriptor)
        finally: os.close(descriptor)


def phase_timings(timings):
    """The phaseTimings of a build: each phase's first start and its work in
    milliseconds, at least one, so a measured phase never reads as running."""
    return [{"phase": code, "startedAt": datetime.fromtimestamp(started, timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z"),
             "durationMs": max(1, round(nanoseconds / 1e6))} for code, (started, nanoseconds) in timings.items()]


def profile(version):
    if version not in PROFILES:
        raise ValueError("unsupported build profile version")
    directory = ROOT / PROFILES[version]
    pieces = [(directory / name).read_bytes() for name in ("profile.json", "Dockerfile")]
    body = json.loads(pieces[0])
    if body.get("id") != "go-react-v1" or body.get("version", "bp1") != version or set(body.get("buildArgs", {})) != {"GO_BUILD_IMAGE", "NODE_BUILD_IMAGE", "STATIC_BASE_IMAGE"}:
        raise ValueError("invalid trusted build profile")
    if any(not re.fullmatch(r"[^\s@]+@sha256:[0-9a-f]{64}", value) for value in body["buildArgs"].values()):
        raise ValueError("build images must be digest pinned")
    return directory, body, hashlib.sha256(b"\0".join(pieces)).hexdigest()


def redirect_environment(redirect):
    """The environment of a module download with the module redirect (module, clone URL), as sandboxd's
    ToolProcessEnvironment gives the local lanes' Sandbox: Git fetches the module's repository from the clone URL,
    and the checksum database, which knows no version only the clone holds, is skipped for the module."""
    if redirect is None:
        return {}
    module, clone = redirect
    return {"GONOSUMDB": module, "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "url." + clone + ".insteadOf", "GIT_CONFIG_VALUE_0": "https://" + module}


def validate(value):
    required = {"contractVersion", "buildRevisionId", "requestId", "projectId", "releaseId", "repositoryFullName", "sourceCommitSha", "buildProfileVersion", "buildProfile", "buildProfileDigest", "environment", "imageTag", "attempt"}
    if not isinstance(value, dict) or type(value.get("contractVersion")) is not int or value["contractVersion"] != 2:
        raise ValueError("exact typed build request required")
    if value.get("sourceOnly") is True:
        required.add("sourceOnly")
        if value.get("buildRevisionId") != "source":
            raise ValueError("source preparation revision required")
    else:
        required.update(("configurationDigest", "configurationRevision"))
        if not isinstance(value.get("buildRevisionId"), str) or not BUILD_REVISION.fullmatch(value["buildRevisionId"]):
            raise ValueError("invalid immutable build revision")
        if not isinstance(value.get("configurationDigest"), str) or not DIGEST.fullmatch(value["configurationDigest"]):
            raise ValueError("invalid immutable configuration digest")
        revision = value.get("configurationRevision")
        if not isinstance(revision, int) or isinstance(revision, bool) or not 1 <= revision <= 2**64 - 1:
            raise ValueError("invalid immutable configuration revision")
    if set(value) != required:
        raise ValueError("exact typed build request required")
    if not re.fullmatch(r"[a-z0-9]{1,32}", value["projectId"]) or not re.fullmatch(r"[a-f0-9]{40}", value["sourceCommitSha"]):
        raise ValueError("invalid immutable source identity")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", value["repositoryFullName"]):
        raise ValueError("invalid repository identity")
    if value["environment"] not in ("test", "staging", "prod") or value["buildProfileVersion"] not in PROFILES or value["buildProfile"] != "go-react-v1":
        raise ValueError("unsupported build scope")
    if value["releaseId"] != value["sourceCommitSha"] or not re.fullmatch(r"[a-f0-9]{64}", value["requestId"]):
        raise ValueError("invalid Release identity")
    if not isinstance(value["attempt"], int) or isinstance(value["attempt"], bool) or not 1 <= value["attempt"] <= 10000:
        raise ValueError("invalid build attempt")
    _, _, digest = profile(value["buildProfileVersion"])
    tag = f"sv2-{value['environment']}-{value['projectId']}:{value['sourceCommitSha']}-{digest[:16]}-{value['buildRevisionId']}"
    if value["buildProfileDigest"] != digest or value["imageTag"] != tag:
        raise ValueError("build fingerprint does not match trusted profile")
    return value


def append_job_log(request, output):
    """Appends one command's output to the build's private job log and keeps
    only its last MAX_JOB_LOG bytes; build.log.start records the absolute
    offset of the first byte kept, so a log-read cursor survives the trim. The
    log is build output: it is read only through log-read, which the platform
    redacts before anyone sees it."""
    path = directory(request) / "build.log"
    with JOB_LOG_LOCK:
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
        with os.fdopen(descriptor, "ab") as log:
            log.write(output)
        size = path.stat().st_size
        if size > MAX_JOB_LOG:
            kept = path.read_bytes()[-MAX_JOB_LOG:]
            private_bytes(path.with_name("build.log.start"), str(job_log_start(request) + size - MAX_JOB_LOG).encode())
            private_bytes(path, kept)


def job_log_start(request):
    """The absolute offset of the first byte the job log keeps, 0 before a trim."""
    path = directory(request) / "build.log.start"
    if path.is_symlink() or (path.exists() and not path.is_file()):
        raise ValueError("invalid job log start")
    return int(path.read_bytes()) if path.exists() else 0


def private_bytes(path, data):
    temporary = path.with_suffix(".tmp")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "wb") as output:
        output.write(data)
    os.replace(temporary, path)


def release_directory(request):
    return STATE / request["environment"] / request["projectId"] / request["releaseId"]


def directory(request):
    return release_directory(request) / request["buildRevisionId"]


def cleanup_fence_path(request):
    # This admission record survives removal of the disposable build directory.
    root = directory(request)
    return root.with_name(root.name + ".cleanup.json")


def release_cleanup_fence_path(request):
    root = release_directory(request)
    return root.with_name(root.name + ".release-cleanup.json")


def release_scope(request):
    return {key: request[key] for key in ("contractVersion", "environment", "projectId", "releaseId", "repositoryFullName", "sourceCommitSha")}


def check_request_identity(previous, request):
    if {k: v for k, v in previous.items() if k != "attempt"} != {k: v for k, v in request.items() if k != "attempt"}:
        raise ValueError("build identity conflict")


def run(command, *, cwd=None, env=None, timeout=1800):
    result = subprocess.run(command, cwd=cwd, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, check=False)
    if result.returncode:
        raise ValueError("owned build command failed")
    return result.stdout.decode().strip()


def immutable_kind_ref(request):
    tag = "docker.io/library/" + request["imageTag"]
    descriptors = set()
    nodes = run(["kind", "get", "nodes", "--name", CLUSTER], timeout=20).splitlines()
    if not nodes:
        raise ValueError("owned Kind nodes are missing")
    for node in nodes:
        lines = run(["docker", "exec", node, "ctr", "--namespace", "k8s.io", "images", "ls", "name==" + tag], timeout=20).splitlines()
        matches = [line.split()[2] for line in lines[1:] if len(line.split()) >= 3 and line.split()[0] == tag]
        if len(matches) != 1 or not DIGEST.fullmatch(matches[0]):
            raise ValueError("image is not loaded in every owned Kind node")
        descriptors.add(matches[0])
    if len(descriptors) != 1:
        raise ValueError("Kind image descriptors disagree")
    digest = descriptors.pop()
    return nodes, tag.split(":", 1)[0] + "@" + digest, digest


class KindPublisher:
    """Publishes a build into the owned Kind cluster: loaded into every node and aliased by its descriptor digest."""

    def require_available(self):
        if run(["kind", "get", "nodes", "--name", CLUSTER], timeout=20) == "":
            raise ValueError("owned V2 Kind cluster is absent")

    def image_tag(self, request):
        return request["imageTag"]

    def publish(self, job, request):
        job.command(["kind", "load", "docker-image", request["imageTag"], "--name", CLUSTER])
        nodes, reference, digest = immutable_kind_ref(request)
        canonical = "docker.io/library/" + request["imageTag"]
        for node in nodes:
            job.command(["docker", "exec", node, "ctr", "--namespace", "k8s.io", "images", "tag", "--force", canonical, reference], timeout=20)
        return reference, digest

    def recheck(self, request):
        _, reference, digest = immutable_kind_ref(request)
        return reference, digest


class RegistryPublisher:
    """Publishes a build to the Native lane's local registry, localhost:<port>, which the engine pushes to and pulls from alike.

    The image is pushed under its build revision's tag; the digest the registry then holds under that tag, an index or a
    manifest, pins the reference, and READY is rechecked by asking the registry for the tag's manifest again.
    """

    def __init__(self, registry):
        self.registry = registry
        # The registry publishes its port on the loopback; the engine resolves localhost itself.
        self.origin = "http://127.0.0.1:" + REGISTRY.fullmatch(registry).group(1)

    def require_available(self):
        try:
            with urllib.request.urlopen(self.origin + "/v2/", timeout=10) as response:
                if response.status != 200:
                    raise ValueError("the local registry does not answer")
        except (OSError, urllib.error.URLError):
            raise ValueError("the local registry does not answer") from None

    def image_tag(self, request):
        return self.registry + "/" + request["imageTag"]

    def publish(self, job, request):
        job.command(["docker", "push", "--quiet", self.image_tag(request)], timeout=900)
        return self.recheck(request)

    def recheck(self, request):
        """The reference pinned by the digest that the registry holds under the build's tag."""
        repository, tag = request["imageTag"].split(":", 1)
        head = urllib.request.Request(self.origin + "/v2/" + repository + "/manifests/" + tag, method="HEAD", headers={"Accept": MANIFEST_TYPES})
        try:
            with urllib.request.urlopen(head, timeout=10) as response:
                digest = response.headers.get("Docker-Content-Digest", "")
        except (OSError, urllib.error.URLError):
            raise ValueError("the local registry holds no manifest of the build") from None
        if not DIGEST.fullmatch(digest):
            raise ValueError("the local registry reported no manifest digest")
        return self.registry + "/" + repository + "@" + digest, digest


def payloads(request):
    output = directory(request) / "artifacts"
    result = {}
    for key, name in (("flowDefinitionBundle", "flow-definitions.json"), ("connectorContract", "connector-contract.json"), ("environmentContract", "environment-contract.json"), ("dexAppManifest", "dex-app.yaml")):
        path = output / name
        if path.is_symlink() or not path.is_file() or not 0 < path.stat().st_size <= MAX_ARTIFACT:
            raise ValueError("immutable release artifact missing or oversized")
        result[key] = path.read_text()
    return result


def native_processes():
    result = subprocess.run(["ps", "-axo", "pid=,ppid=,pgid=,lstart="], capture_output=True, text=True, timeout=5, check=True)
    return {int(parts[0]): {"pid": int(parts[0]), "parent": int(parts[1]), "group": int(parts[2]), "started": parts[3].strip()}
            for line in result.stdout.splitlines() if len(parts := line.split(None, 3)) == 4}


def same_process(left, right):
    # PPID can change on reparenting; PID/start/group still identify the child.
    return right is not None and all(left[key] == right[key] for key in ("pid", "group", "started"))


def source_containers(request):
    """Query only this attempt's exact private names, including a late create."""
    prefix = f"sv2-source-{request['requestId'][:32]}-{request['attempt']}-"
    inspection = subprocess.run(["docker", "container", "ls", "--all", "--filter", "name=^/" + prefix + "[0-9]+$", "--format", "{{.Names}}"], capture_output=True, text=True, timeout=10, check=False)
    names = inspection.stdout.splitlines()
    if inspection.returncode or len(names) > 16 or any(not re.fullmatch(re.escape(prefix) + r"[0-9]+", name) for name in names):
        raise ValueError("source preparation container observation is unknown")
    return names


def cancel_native_child(identity):
    """Bounded cleanup of this exact child tree, never the launcher PGID.

    The tree is stopped before it is killed, so it cannot fork during the kill,
    and the cancellation is confirmed once every observed child is gone: the
    build commands (git and the Docker CLI) do not daemonize, so a descendant
    that reparented before the first observation is not expected. It raises,
    leaving the outcome UNKNOWN, when it cannot establish or stop the tree; the
    whole launcher's group stays the final exit boundary.
    """
    known = {identity["pid"]: identity}
    deadline = time.monotonic() + 5
    stable = 0
    while time.monotonic() < deadline and stable < 2:
        table = native_processes()
        found = dict(known)
        while True:
            additions = {pid: child for pid, child in table.items() if pid not in found and child["parent"] in found and same_process(found[child["parent"]], table.get(child["parent"]))}
            if not additions: break
            found.update(additions)
        stable = stable + 1 if found.keys() == known.keys() else 0
        known = found
        if len(known) > 256: raise ValueError("Native build child tree exceeds the bounded cleanup budget")
        for child in known.values():
            if time.monotonic() >= deadline: raise ValueError("Native build child discovery deadline exceeded")
            if same_process(child, native_processes().get(child["pid"])):
                try: os.kill(child["pid"], signal.SIGSTOP)
                except ProcessLookupError: pass
        time.sleep(0.05)
    # Kill only still-matching observed identities. Never signal a numeric PID
    # based solely on an old parent/group association, or signal the outer group.
    deadline = time.monotonic() + 5
    for child in reversed(list(known.values())):
        if time.monotonic() >= deadline: raise ValueError("Native build child signaling deadline exceeded")
        if same_process(child, native_processes().get(child["pid"])):
            try: os.kill(child["pid"], signal.SIGKILL)
            except ProcessLookupError: pass
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        table = native_processes()
        # The direct command is reaped by its owning communicate() thread.
        if not any(same_process(child, table.get(child["pid"])) for child in known.values() if child["pid"] != identity["pid"]): return
        time.sleep(0.05)
    raise ValueError("Native build child cleanup is incomplete")


def cleanup_container(name, *, require_observation, on_observed):
    """An aborted Docker create may finish after its client exits. Absence
    proves cleanup only after the exact container has been observed."""
    def present():
        inspection = subprocess.run(["docker", "container", "ls", "--all", "--filter", "name=^/" + name + "$", "--format", "{{.Names}}"], capture_output=True, text=True, timeout=10, check=False)
        if inspection.returncode or inspection.stdout.strip() not in ("", name):
            raise ValueError("build container cleanup inspection is unknown")
        return bool(inspection.stdout.strip())
    if not present():
        return not require_observation
    on_observed()
    subprocess.run(["docker", "rm", "--force", "--volumes", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10, check=False)
    if present():
        raise ValueError("build container cleanup is unknown")
    return True


class CommandFailed(ValueError):
    def __init__(self, status, stderr=b""):
        super().__init__("owned build command failed")
        self.status, self.stderr = status, stderr


class CommandDeadlineExceeded(ValueError):
    def __init__(self, timeout, stderr=b""):
        super().__init__("build command deadline exceeded")
        self.timeout, self.stderr = timeout, stderr


class MetadataCommandFailure(ValueError):
    def __init__(self, command, error, secrets):
        self.code = "METADATA_COMMAND_TIMEOUT" if isinstance(error, CommandDeadlineExceeded) else "METADATA_COMMAND_FAILED"
        reason = f"deadline exceeded after {error.timeout:g}s" if isinstance(error, CommandDeadlineExceeded) else f"exit status {error.status}"
        # Redact before truncating: never expose a suffix of a cut credential.
        tail = error.stderr.decode(errors="replace")
        for secret in secrets:
            if secret:
                tail = tail.replace(secret, "[REDACTED]")
        tail = re.sub(r"-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)", "[REDACTED]", tail)
        tail = re.sub(r"(?i)(https?://)[^/\s@]+@", r"\1[REDACTED]@", tail)
        tail = re.sub(r"(?i)(\b(?:bearer|basic)\s+)[^\s\"\']+", r"\1[REDACTED]", tail)
        tail = re.sub(r"(?i)((?:[\w.-]*(?:token|password|secret|credential|api[_-]?key|access[_-]?key)[\w.-]*)[\"\']?\s*[:=]\s*)(?:\"[^\"]*\"|\'[^\']*\'|[^\s,;&\"\']+)", r"\1[REDACTED]", tail)
        tail = re.sub(r"\b(?:gh[pousr]_[A-Za-z0-9]+|github_pat_[A-Za-z0-9_]+|(?:AKIA|ASIA)[A-Z0-9]{16}|sk-[A-Za-z0-9_-]+)\b", "[REDACTED]", tail)
        tail = re.sub(r"\beyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+", "[REDACTED]", tail)
        tail = " ".join(tail.split())
        tail = tail.encode()
        tail = ("[truncated] " if len(tail) > MAX_FAILURE_STDERR_BYTES else "") + tail[-MAX_FAILURE_STDERR_BYTES:].decode(errors="ignore")
        super().__init__(f"{self.code}: {command}: {reason}; stderr tail: {tail or '[empty]'}")


def validate_live_sdk(metadata, *, resolved):
    """Resolve only module metadata; never retain a dependency version."""
    sdk = "github.com/superdurable/dex/sdk-go"
    if resolved:
        if metadata.get("Path") != sdk or metadata.get("Error"):
            raise ValueError("Live requires Dex Go SDK v1.13.0 or later")
        module = metadata.get("Replace", metadata)
    else:
        module = next((item for item in metadata.get("Require") or [] if item.get("Path") == sdk), None)
        if module is None:
            raise ValueError("Live requires Dex Go SDK v1.13.0 or later")
        replacements = [item for item in metadata.get("Replace") or []
                        if item.get("Old", {}).get("Path") == sdk
                        and item["Old"].get("Version", "") in ("", module.get("Version"))]
        exact = next((item for item in replacements if item["Old"].get("Version")), None)
        if replacements:
            module = (exact or replacements[-1])["New"]
    version = module.get("Version", "")
    match = re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?", version)
    if match is None:
        raise ValueError("Live requires a versioned Dex Go SDK v1.13.0 or later")
    # Preliminary metadata rejects missing/local/unparseable replacements;
    # the selected build list, after dependency preparation, enforces the floor.
    if resolved and (tuple(map(int, match.groups()[:3])) < (1, 13, 0)
                     or (match.groups()[:3] == ("1", "13", "0") and match.group(4))):
        raise ValueError("Live requires Dex Go SDK v1.13.0 or later")


class Job:
    def __init__(self, request, credential, tool_image, app_manifest_tool, git_base_url, module_redirect, publisher, sdk_failure_category):
        self.sdk_failure_category = sdk_failure_category
        self.sdk_rejected = False
        self.request = request
        self.credential = credential
        self.failure_secrets = (credential,)
        self.tool_image = tool_image
        self.app_manifest_tool = app_manifest_tool
        self.git_base_url = git_base_url
        self.redirect = redirect_environment(module_redirect)
        self.publisher = publisher
        self.cancelled = threading.Event()
        self.process = None
        self.process_identity = None
        self.native = os.environ.get("SUPERVERSE_LOCAL_NATIVE") == "true"
        self.deadline = time.monotonic() + SOURCE_TIMEOUT if request.get("sourceOnly") else None
        self.container_sequence = 0
        self.containers = set()
        self.uncertain_containers = set()
        self.cleanup_unknown = False
        self.command_kind = "none"
        # A Native cancellation that could not establish or stop the exact child tree.
        self.cancel_unknown = False
        self.lock = threading.Lock()
        # QUEUE lasts from start admission until the job thread begins.
        self.admitted = time.time(), time.monotonic_ns()
        self.timings = {}
        self.thread = threading.Thread(target=self.build, daemon=True)

    def measure(self, code, started, clock):
        """Adds the work since clock (time.monotonic_ns) to phase code. A phase
        the build enters twice keeps its first start (time.time)."""
        timing = self.timings.setdefault(code, [started, 0])
        timing[1] += time.monotonic_ns() - clock

    @contextlib.contextmanager
    def phase(self, code):
        """Measures the enclosed work as phase code once it succeeds."""
        started, clock = time.time(), time.monotonic_ns()
        yield
        self.measure(code, started, clock)

    def command(self, argv, *, cwd=None, environment=None, timeout=1800, log_output=True, capture_stderr=False):
        self.command_kind = "docker run" if argv[:2] == ["docker", "run"] else Path(argv[0]).name if Path(argv[0]).name in ("git", "docker", "app-manifest") else "trusted tool"
        if self.cancelled.is_set():
            raise ValueError("build cancelled")
        if self.deadline is not None:
            timeout = min(timeout, self.deadline - time.monotonic())
            if timeout <= 0:
                raise CommandDeadlineExceeded(timeout)
        with self.lock:
            if self.cancelled.is_set(): raise ValueError("build cancelled")
            # Docker client cancellation alone does not prove its container exited.
            # Metadata and analysis containers have exact owned names for reconciliation.
            container_name = None
            if argv[:2] == ["docker", "run"]:
                self.container_sequence += 1
                container_name = f"sv2-source-{self.request['requestId'][:32]}-{self.request['attempt']}-{self.container_sequence}"
                self.containers.add(container_name)
                argv = [*argv[:2], "--name", container_name, *argv[2:]]
            self.process = subprocess.Popen(argv, cwd=cwd, env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=not self.native)
        try:
            if self.native:
                with self.lock:
                    try:
                        # A fast command may exit between ps and poll; under
                        # load ps can also miss a newly spawned direct child.
                        # Popen owns the unreaped PID, so it cannot be reused.
                        for _ in range(3):
                            if self.process.poll() is not None:
                                break
                            try:
                                self.process_identity = native_processes().get(self.process.pid)
                            except (OSError, subprocess.SubprocessError):
                                self.process_identity = None
                            if self.process_identity is not None:
                                break
                            time.sleep(0.05)
                        if self.process.poll() is None and (self.process_identity is None or self.process_identity["parent"] != os.getpid()):
                            raise ValueError("Native build child identity could not be established")
                    except Exception:
                        append_job_log(self.request, b"CHILD_IDENTITY_EXCEPTION class=native-child-observation-failed\n")
                        self.cancel_unknown = True
                        raise
            output, errors = self.process.communicate(timeout=timeout)
            if log_output:
                append_job_log(self.request, output + errors)
            if self.cancelled.is_set():
                raise ValueError("build cancelled")
            if self.process.returncode:
                raise CommandFailed(self.process.returncode, errors)
            if container_name is not None:
                with self.lock:
                    self.containers.discard(container_name)
            decoded = output.decode().strip()
            return (decoded, errors) if capture_stderr else decoded
        except subprocess.TimeoutExpired as error:
            errors = error.stderr or b""
            try:
                self.stop_command()
            except (OSError, ValueError, subprocess.SubprocessError):
                self.cleanup_unknown = True
            try:
                _, errors = self.process.communicate(timeout=10)
            except subprocess.TimeoutExpired:
                self.cancel_unknown = True
            raise CommandDeadlineExceeded(timeout, errors) from error
        finally:
            with self.lock:
                # Retain uncertain Native process responsibility for shutdown.
                if not self.native or self.process.poll() is not None:
                    self.process = None
                    self.process_identity = None

    def cleanup_containers_locked(self):
        for name in tuple(self.containers):
            try:
                if not cleanup_container(name, require_observation=name in self.uncertain_containers,
                                         on_observed=lambda: self.uncertain_containers.discard(name)):
                    raise ValueError("aborted build container creation is unconfirmed")
                self.containers.discard(name)
                self.uncertain_containers.discard(name)
            except (OSError, ValueError, subprocess.SubprocessError):
                self.cleanup_unknown = True
                raise
        self.cleanup_unknown = False

    def cancel(self):
        self.cancelled.set()
        try:
            self.stop_command()
        except (OSError, ValueError, subprocess.SubprocessError):
            # Cancellation is admitted; Inspect keeps unresolved owned work
            # pending instead of rejecting the caller's accepted cancellation.
            if not self.cleanup_unknown and not self.cancel_unknown:
                raise

    def stop_command(self):
        """Stop owned work without turning a command failure into user cancellation."""
        with self.lock:
            if self.native and self.cancelled.is_set():
                path = directory(self.request) / "state.json"
                try: previous = json.loads(path.read_bytes())
                except FileNotFoundError: previous = None  # Already-cleaned jobs must not recreate storage.
                if previous is not None and previous.get("request") == self.request and previous.get("state") == "RUNNING":
                    private_json(path, {"request": self.request, "state": "UNKNOWN", "failure": "NATIVE_CANCEL_OUTCOME_UNKNOWN"})
            try:
                if self.process is not None and self.process.poll() is None:
                    self.uncertain_containers.update(self.containers)
                    if self.native:
                        # The cancellation is confirmed once every observed child of the
                        # command is gone; otherwise its outcome stays unknown.
                        try:
                            if self.process_identity is None:
                                identity = native_processes().get(self.process.pid)
                                if identity is not None and identity["parent"] == os.getpid():
                                    self.process_identity = identity
                            if self.process_identity is None: raise ValueError("Native build child identity is unknown")
                            cancel_native_child(self.process_identity)
                            self.cancel_unknown = False
                        except Exception:
                            self.cancel_unknown = True
                            raise
                    else:
                        try: os.killpg(self.process.pid, signal.SIGTERM)
                        except ProcessLookupError: pass
                        deadline = time.monotonic() + 3
                        while self.process.poll() is None and time.monotonic() < deadline:
                            time.sleep(0.05)
                        if self.process.poll() is None:
                            try: os.killpg(self.process.pid, signal.SIGKILL)
                            except ProcessLookupError: pass
            finally:
                # A child identity failure cannot leave an owned Docker container
                # running; reconcile the independently recorded container too.
                self.cleanup_containers_locked()

    def check_live_sdk(self, metadata, *, resolved):
        try:
            validate_live_sdk(json.loads(metadata), resolved=resolved)
        except (ValueError, TypeError, KeyError):
            self.sdk_rejected = True
            append_job_log(self.request, b"Dex SDK check: rejected\n")
            raise
        append_job_log(self.request, b"Dex SDK check: ok\n")

    def metadata_failure(self, command, error):
        if isinstance(error, OSError):
            error = CommandFailed("launch error", str(error).encode())
        outcome = "timed out" if isinstance(error, CommandDeadlineExceeded) else "platform failure"
        append_job_log(self.request, ("Dex SDK check: " + outcome + "\n").encode())
        # The upstream wrapper redacts the complete stderr before its bounded tail.
        return MetadataCommandFailure(command, error, self.failure_secrets)

    def prepare_dependencies(self, container):
        logical = "docker run <tool-image> fixed dependency preparation: go mod download; go mod edit -json; go list -m -json -e -mod=readonly github.com/superdurable/dex/sdk-go"
        try:
            output, errors = self.command([*container, "--network", "bridge", "--entrypoint", "/bin/sh",
                                           self.tool_image, "-c", DEPENDENCY_PREPARATION_SCRIPT],
                                          timeout=DEPENDENCY_PREPARATION_TIMEOUT,
                                          log_output=False, capture_stderr=True)
        except (CommandFailed, CommandDeadlineExceeded, OSError) as error:
            raise self.metadata_failure(logical, error) from error
        except UnicodeError:
            raise self.metadata_failure(logical, CommandFailed("invalid metadata encoding")) from None
        try:
            (download, declared_status, resolved_status), declared, resolved = dependency_metadata(output)
        except (ValueError, UnicodeError):
            raise self.metadata_failure(logical, CommandFailed("invalid metadata output")) from None
        if declared_status:
            raise self.metadata_failure("go mod edit -json", CommandFailed(declared_status, errors))
        self.check_live_sdk(declared, resolved=False)
        if download:
            # Preserve the download failure class; metadata failures above are redacted.
            append_job_log(self.request, errors)
            append_job_log(self.request, b"Dex SDK check: platform failure\n")
            raise CommandFailed(download, errors)
        if resolved_status:
            raise self.metadata_failure("go list -m -json -e -mod=readonly github.com/superdurable/dex/sdk-go",
                                        CommandFailed(resolved_status, errors))
        self.check_live_sdk(resolved, resolved=True)

    def render(self, checkout):
        manifest_path = checkout / "dex-app.yaml"
        if manifest_path.is_symlink() or not manifest_path.is_file() or manifest_path.stat().st_size > 4 * 1024 * 1024:
            raise ValueError("manifest must be a bounded regular source file")
        manifest_bytes = manifest_path.read_bytes()
        with self.phase("ANALYSIS"):
            manifest_artifacts = json.loads(self.command([str(self.app_manifest_tool), "--manifest", str(checkout / "dex-app.yaml"), "--source-commit", self.request["sourceCommitSha"], "--project-id", self.request["projectId"], "--release-id", self.request["releaseId"]], cwd=ROOT, timeout=30))
        manifest = json.loads(manifest_bytes)
        if manifest.get("schemaVersion") != "superverse.dev/dex-app/v1" or not manifest.get("flowDefinitions"):
            raise ValueError("missing application manifest")
        output = directory(self.request) / "artifacts"
        output.mkdir(exist_ok=True, mode=0o700)
        definitions = []
        names = set()
        # Fetch actual public modules without executing repository scripts. This cache
        # belongs to this attempt only; FDG analysis remains offline and unprivileged.
        # A redirected module that the proxy lacks, a release candidate only the redirect's clone holds, is
        # fetched directly from that clone; every other module still comes from the proxies and the checksum database.
        proxies = "file:///opt/superverse/go-module-proxy,https://proxy.golang.org" + (",direct" if self.redirect else "")
        redirect = [argument for key, value in self.redirect.items() if key != "GONOSUMDB" for argument in ("-e", key + "=" + value)]
        with tempfile.TemporaryDirectory(prefix="modules-", dir=directory(self.request)) as module_cache:
            container = ["docker", "run", "--rm", "--read-only", "--cap-drop", "ALL",
                         "--security-opt", "no-new-privileges", "--user", f"{os.getuid()}:{os.getgid()}",
                         "--tmpfs", "/tmp:rw,exec,size=512m", "-e", "HOME=/tmp",
                         "-e", "GOCACHE=/tmp/go-cache", "-e", "GOMODCACHE=/dependencies",
                         "-e", "GOTOOLCHAIN=local", "-e", "CGO_ENABLED=0", "-e", "GOFLAGS=-modcacherw",
                         "-e", "GOPROXY=" + proxies,
                         "-e", "GOSUMDB=sum.golang.org", "-e", "GOPRIVATE=", "-e", "GONOPROXY=none",
                         "-e", "GONOSUMDB=" + self.redirect.get("GONOSUMDB", "none"), *redirect, "-e", "GOWORK=off",
                         "-v", f"{checkout}:/workspace:ro", "-v", f"{output}:/out:rw",
                         "-v", f"{module_cache}:/dependencies:rw", "-w", "/workspace"]
            # Fixed trusted commands read the exact module files; no project script,
            # go generate, host credential or Docker socket is passed.
            # With lazy module loading, `all` also resolves dependency-only
            # packages and can add unused checksums to go.sum. Prepare the
            # modules declared by each accepted lockfile without modifying the
            # read-only source; offline strict FDG analysis proves sufficiency.
            with self.phase("DEPENDENCY_PREPARATION"):
                self.prepare_dependencies(container)
            with self.phase("ANALYSIS"):
                for index, entry in enumerate(manifest["flowDefinitions"]):
                    source = entry.get("sourcePath") if isinstance(entry, dict) else None
                    if not isinstance(source, str) or source.startswith("/") or ".." in Path(source).parts or not source.endswith(".go"):
                        raise ValueError("invalid Flow source")
                    if (checkout / source).is_symlink() or not (checkout / source).is_file():
                        raise ValueError("Flow source is missing")
                    # Untrusted source is analyzed inside the pinned tool container without host credentials or Docker access.
                    self.command([*container, "--network", "none", "--entrypoint", "dexcli", self.tool_image, "visualize", source, "--schema-version", "2.0", "--json", "--out", f"/out/flow-{index}"])
                    graph_path = output / f"flow-{index}.json"
                    if graph_path.is_symlink() or not graph_path.is_file() or not 0 < graph_path.stat().st_size <= MAX_ARTIFACT:
                        raise ValueError("FDG must be a bounded regular artifact")
                    graph = json.loads(graph_path.read_bytes())
                    name = graph.get("flow", {}).get("name")
                    if graph.get("schemaVersion") != "2.0" or graph.get("valid") is not True or blocking_diagnostics(graph) or not name or name in names:
                        raise ValueError("invalid or duplicate FDG")
                    names.add(name)
                    definitions.append({"flowType": name, "sourcePath": source, "schemaVersion": "2.0", "digest": "sha256:" + hashlib.sha256(encoded(graph)).hexdigest(), "valid": True, "graph": graph})
        request = self.request
        identity = {"projectId": request["projectId"], "releaseId": request["releaseId"], "buildRevisionId": request["buildRevisionId"]}
        if not request.get("sourceOnly"):
            identity["configurationDigest"] = request["configurationDigest"]
            identity["configurationRevision"] = request["configurationRevision"]
        private_json(output / "flow-definitions.json", {"schemaVersion": "superverse.dev/flow-definition-bundle/v1", **identity, "sourceCommitSha": request["sourceCommitSha"], "buildProfileDigest": request["buildProfileDigest"], "flowDefinitions": sorted(definitions, key=lambda value: value["flowType"])})
        private_json(output / "connector-contract.json", manifest_artifacts["connectorContract"])
        private_json(output / "environment-contract.json", manifest_artifacts["environmentContract"])
        (output / "dex-app.yaml").write_bytes(manifest_bytes)
        (output / "dex-app.yaml").chmod(0o600)

    def build(self):
        request = self.request
        state = directory(request) / "state.json"
        self.measure("QUEUE", *self.admitted)
        try:
            trusted, body, _ = profile(request["buildProfileVersion"])
            with tempfile.TemporaryDirectory(prefix="checkout-", dir=directory(request)) as temporary:
                checkout = Path(temporary) / "repository"
                credential_env = dict(os.environ, GIT_TERMINAL_PROMPT="0", SUPERVERSE_BUILD_GIT_CREDENTIAL=self.credential)
                self.credential = ""
                helper = ["-c", "credential.helper=", "-c", "credential.helper=!f() { echo username=x-access-token; echo password=$SUPERVERSE_BUILD_GIT_CREDENTIAL; }; f"]
                with self.phase("HOST_PREPARATION"):
                    self.command(["git", "init", str(checkout)])
                    self.command(["git", "-C", str(checkout), "remote", "add", "origin", self.git_base_url + "/" + request["repositoryFullName"] + ".git"])
                    # The exact commit, with its history: the local Git host does not serve shallow requests, and the repositories are small.
                    self.command(["git", *helper, "-C", str(checkout), "fetch", "origin", request["sourceCommitSha"]], environment=credential_env)
                    credential_env.pop("SUPERVERSE_BUILD_GIT_CREDENTIAL", None)
                    self.command(["git", "-C", str(checkout), "checkout", "--detach", request["sourceCommitSha"]])
                    if self.command(["git", "-C", str(checkout), "rev-parse", "HEAD"]) != request["sourceCommitSha"]:
                        raise ValueError("source identity mismatch")
                self.render(checkout)
                if request.get("sourceOnly"):
                    with self.lock:
                        if self.cancelled.is_set() or time.monotonic() >= self.deadline:
                            raise ValueError("source preparation cancelled or expired")
                        private_json(state, {"request": request, "state": "READY", "artifactReference": "", "imageId": "", "phaseTimings": phase_timings(self.timings)})
                    return
                # A cached download layer does not guarantee that its mutable module
                # cache still contains the dependencies needed by the offline build.
                # Re-execute this stage while retaining its module/compiler caches.
                arguments = ["docker", "build", "--no-cache-filter", "go-build", "--file", str(trusted / "Dockerfile"), "--tag", self.publisher.image_tag(request)]
                labels = ("projectId", "sourceCommitSha", "buildProfileDigest", "environment", "buildRevisionId", "configurationDigest", "configurationRevision")
                for key in labels:
                    arguments += ["--label", "superverse-v2." + key + "=" + str(request[key])]
                for key, value in body["buildArgs"].items():
                    arguments += ["--build-arg", key + "=" + value]
                # bp1 predates the redirect; its accepted builds stay as they were.
                if request["buildProfileVersion"] != "bp1":
                    for key, value in self.redirect.items():
                        arguments += ["--build-arg", key + "=" + value]
                with self.phase("COMPILATION"):
                    self.command([*arguments, str(checkout)])
                with self.phase("IMAGE_PUBLICATION"):
                    reference, digest = self.publisher.publish(self, request)
                with self.lock:
                    if self.native and self.cancelled.is_set(): raise ValueError("Native build cancelled before its result")
                    private_json(state, {"request": request, "state": "READY", "artifactReference": reference, "imageId": digest, "phaseTimings": phase_timings(self.timings)})
        except Exception as error:
            # Only trusted classifications and command kinds enter the job log.
            classification = "metadata-command-failed" if isinstance(error, MetadataCommandFailure) else "command-failed" if isinstance(error, CommandFailed) else "command-timeout" if isinstance(error, CommandDeadlineExceeded) else "os-error" if isinstance(error, OSError) else "validation-or-ownership" if isinstance(error, ValueError) else "internal-error"
            append_job_log(request, f"\nBUILD_EXCEPTION class={classification} command={self.command_kind}\n".encode())
            # A failed Docker client may still own a source analysis container.
            if self.containers or (self.process is not None and self.process.poll() is None):
                try:
                    if self.process is not None and self.process.poll() is None:
                        self.stop_command()
                    else:
                        with self.lock:
                            self.cleanup_containers_locked()
                except Exception:
                    self.cleanup_unknown = True
                    append_job_log(request, b"CLEANUP_EXCEPTION class=container-or-child-outcome-unknown\n")
            # Keep a late Docker create owned across Inspect and helper restart.
            with self.lock:
                unknown = self.cleanup_unknown or self.cancel_unknown
                failure = ("CANCELLED" if self.cancelled.is_set() else str(error) if isinstance(error, MetadataCommandFailure)
                           else "COMMAND_TIMEOUT" if isinstance(error, CommandDeadlineExceeded) else "BUILD_FAILED")
                terminal = {"request": request, "state": "UNKNOWN" if unknown else "FAILED",
                            "failure": (failure + "; container cleanup outcome unknown") if self.cleanup_unknown and isinstance(error, MetadataCommandFailure) and not self.cancelled.is_set() else "CANCEL_OUTCOME_UNKNOWN" if self.cleanup_unknown else "NATIVE_CANCEL_OUTCOME_UNKNOWN" if unknown else failure,
                            "failureCategory": self.sdk_failure_category if self.sdk_rejected and not unknown and not self.cancelled.is_set() else "unclassified-platform-failure" if self.cancelled.is_set() or not isinstance(error, CommandFailed) else "",
                            "childSettled": not self.cancel_unknown and (self.process is None or self.process.poll() is not None),
                            "phaseTimings": phase_timings(self.timings)}
                if self.containers:
                    terminal["pendingContainers"] = sorted(self.containers)
                    terminal["unobservedContainers"] = sorted(self.uncertain_containers)
                    terminal["failureAfterCleanup"] = failure
                    terminal["nativeCancelUnknown"] = self.cancel_unknown
                private_json(state, terminal)

        finally:
            self.credential = ""
            self.failure_secrets = ()


class Server(http.server.ThreadingHTTPServer):
    daemon_threads = True
    def __init__(self, address, token, tool_image, app_manifest_tool, git_base_url, module_redirect, publisher):
        super().__init__(address, Handler)
        self.token, self.tool_image, self.app_manifest_tool, self.git_base_url, self.publisher = token, tool_image, app_manifest_tool, git_base_url, publisher
        self.module_redirect = module_redirect
        self.jobs = {}
        self.lock = threading.Lock()
        self.stopping = False

    def cleanup_fence(self, request):
        try: fence = json.loads(cleanup_fence_path(request).read_bytes())
        except FileNotFoundError: return None
        if not isinstance(fence, dict) or set(fence) != {"schemaVersion", "request"} or fence["schemaVersion"] != "superverse.dev/local-build-cleanup/v1":
            raise ValueError("invalid build cleanup admission record")
        previous = validate(fence["request"])
        check_request_identity(previous, request)
        return previous

    def release_cleanup_fence(self, request):
        try: fence = json.loads(release_cleanup_fence_path(request).read_bytes())
        except FileNotFoundError: return None
        if not isinstance(fence, dict) or set(fence) != {"schemaVersion", "scope"} or fence["schemaVersion"] != "superverse.dev/local-release-cleanup/v2" or fence["scope"] != release_scope(request):
            raise ValueError("invalid Release cleanup admission record")
        return fence

    def cleanup_release(self, request):
        fence = self.release_cleanup_fence(request)
        # Close every revision before inspecting jobs, including a Start that has
        # not arrived yet. This receipt survives all disposable directory cleanup.
        private_json(release_cleanup_fence_path(request), fence or {"schemaVersion": "superverse.dev/local-release-cleanup/v2", "scope": release_scope(request)}, durable=True)
        root = release_directory(request)
        if not root.exists():
            return {"state": "MISSING"}
        if root.is_symlink() or not root.is_dir():
            raise ValueError("invalid Release cleanup directory")
        candidates = []
        with os.scandir(root) as entries:
            for entry in entries:
                if entry.name.endswith(".cleanup.json"):
                    continue
                if entry.is_symlink() or not entry.is_dir(follow_symlinks=False) or (entry.name != "source" and not BUILD_REVISION.fullmatch(entry.name)):
                    raise ValueError("unexpected Release cleanup entry")
                candidates.append(Path(entry.path))
                if len(candidates) > MAX_CLEANUP_REVISIONS:
                    break
        unsettled = "RUNNING" if len(candidates) > MAX_CLEANUP_REVISIONS else None
        for child in candidates[:MAX_CLEANUP_REVISIONS]:
            path = child / "state.json"
            receipt_path = child.with_name(child.name + ".cleanup.json")
            receipt = None
            if receipt_path.exists():
                if receipt_path.is_symlink() or not receipt_path.is_file() or receipt_path.stat().st_size > MAX_BODY:
                    raise ValueError("invalid Release revision cleanup receipt")
                receipt = json.loads(receipt_path.read_bytes())
                previous = validate(receipt["request"])
            else:
                if path.is_symlink() or not path.is_file() or path.stat().st_size > MAX_BODY:
                    raise ValueError("Release cleanup state is missing or invalid")
                state = json.loads(path.read_bytes())
                previous = validate(state["request"])
            if release_scope(previous) != release_scope(request) or directory(previous) != child:
                raise ValueError("Release cleanup state belongs to another identity")
            job = self.jobs.get(str(path))
            if job is not None and job.thread.is_alive():
                job.cancel()
                unsettled = unsettled or "RUNNING"
                continue
            if receipt is None:
                state = self.reconcile_container_cleanup(path, state)
                if state["state"] in ("RUNNING", "UNKNOWN"):
                    unsettled = "UNKNOWN"
                    continue
                if state["state"] not in ("READY", "FAILED"):
                    raise ValueError("invalid Release cleanup state")
            # A durable revision receipt proves deletion was admitted after its
            # provider settled, even if a prior partial rmtree removed state.json.
            self.cleanup_fence(previous)
            private_json(cleanup_fence_path(previous), {"schemaVersion": "superverse.dev/local-build-cleanup/v1", "request": previous}, durable=True)
            shutil.rmtree(child, ignore_errors=False)
            self.jobs.pop(str(path), None)
        return {"state": unsettled or "MISSING"}

    def log_read(self, request, offset, limit):
        """One page of the build's job log from the absolute byte offset, or its
        last limit bytes when offset is None: whole lines while more can follow,
        a single longer line cut at limit. Bytes trimmed before offset are
        skipped. complete reports a build that no longer runs and whose log holds
        nothing after next; a build without a log, or closed by cleanup, has an
        empty page that is complete once nothing runs."""
        path = directory(request) / "state.json"
        if self.cleanup_fence(request) is not None or not path.exists():
            return {"text": "", "next": offset or 0, "complete": True}
        state = json.loads(path.read_bytes())
        check_request_identity(state["request"], request)
        job = self.jobs.get(str(path))
        running = state["state"] == "RUNNING" and job is not None and job.thread.is_alive()
        log = directory(request) / "build.log"
        with JOB_LOG_LOCK:
            data = b"" if log.is_symlink() or not log.is_file() else log.read_bytes()
            start = job_log_start(request)
        end = start + len(data)
        offset = max(end - limit, start) if offset is None else max(offset, start)
        page = data[min(offset, end) - start:][:limit]
        if running or offset + len(page) < end:
            line_end = page.rfind(b"\n")
            if line_end >= 0:
                page = page[:line_end + 1]
            elif len(page) < limit:
                page = b""
        following = offset + len(page)
        return {"text": page.decode(errors="replace"), "next": following, "complete": not running and following >= end}

    def reconcile_container_cleanup(self, path, state):
        request = state["request"]
        job = self.jobs.get(str(path))
        if state["state"] not in ("FAILED", "UNKNOWN") or state.get("nativeCancelUnknown"):
            return state
        child_settled = state.get("childSettled", state["state"] == "FAILED" or (bool(state.get("pendingContainers")) and state.get("nativeCancelUnknown") is False)) or (job is not None and not job.cancel_unknown and (job.process is None or job.process.poll() is not None))
        if not child_settled or (job is not None and job.thread.is_alive()):
            return state
        names = state.get("pendingContainers", state.get("ownedContainers", []))
        prefix = f"sv2-source-{request['requestId'][:32]}-{request['attempt']}-"
        if len(names) > 16 or any(not re.fullmatch(re.escape(prefix) + r"[1-9][0-9]*", name) for name in names):
            raise ValueError("invalid pending build container ownership")
        unobserved = set(state.get("unobservedContainers", names))
        if not unobserved.issubset(names):
            raise ValueError("invalid unobserved build container ownership")
        # Source retry also discovers late creates from an ended older helper.
        # Feed them into the same exact-name cleanup used for Live builds.
        discovered = source_containers(request) if request.get("sourceOnly") else []
        names = sorted(set(names) | set(discovered))
        if len(names) > 16:
            raise ValueError("invalid pending build container ownership")
        unobserved.difference_update(discovered)
        if not names and (state["state"] != "UNKNOWN" or not request.get("sourceOnly")):
            return state
        original = state.get("failureAfterCleanup", state["failure"] if state["state"] == "FAILED" else "BUILD_FAILED")
        state.update(state="UNKNOWN", pendingContainers=names, unobservedContainers=sorted(unobserved), failureAfterCleanup=original, childSettled=True)
        state.pop("ownedContainers", None)
        private_json(path, state)
        remaining = list(names)
        # One removal per inspection keeps retry and Release cleanup bounded.
        for name in names[:1]:
            def observed():
                # Persist proof before an uncertain removal, so later absence is conclusive.
                unobserved.discard(name)
                state["unobservedContainers"] = sorted(unobserved)
                private_json(path, state)
            try:
                if cleanup_container(name, require_observation=name in unobserved, on_observed=observed):
                    remaining.remove(name)
            except (OSError, ValueError, subprocess.SubprocessError):
                pass  # Retain the exact identity until its effect can be proved.
        if remaining:
            state.update(pendingContainers=remaining, unobservedContainers=sorted(unobserved.intersection(remaining)))
            private_json(path, state)
            return state
        state.update(state="FAILED", failure=original)
        for key in ("pendingContainers", "unobservedContainers", "failureAfterCleanup", "nativeCancelUnknown"):
            state.pop(key, None)
        private_json(path, state)
        append_job_log(request, b"RECONCILED class=owned-containers-absent outcome=FAILED\n")
        if job is not None:
            with job.lock:
                job.containers.difference_update(names)
                job.uncertain_containers.difference_update(names)
                job.cleanup_unknown = False
        return state

    def inspect(self, request):
        fence = self.cleanup_fence(request)
        if fence is not None:
            # No build is available after cleanup admission. Only Cleanup may
            # finish removing residual files; Inspect does not promise deletion.
            return {"state": "MISSING", "attempt": fence["attempt"]}
        path = directory(request) / "state.json"
        if not path.exists():
            return {"state": "MISSING"}
        state = json.loads(path.read_bytes())
        previous = state["request"]
        check_request_identity(previous, request)
        state = self.reconcile_container_cleanup(path, state)
        # Failure detail is bounded and redacted before persistence.
        result = {key: state[key] for key in ("state", "artifactReference", "imageId", "failure", "failureCategory", "phaseTimings") if key in state}
        result["operationId"] = request["requestId"] + "-" + str(previous["attempt"])
        result["attempt"] = previous["attempt"]
        if state["state"] == "RUNNING":
            job = self.jobs.get(str(path))
            if job is None or not job.thread.is_alive():
                result["state"] = "UNKNOWN"
        if state["state"] == "READY":
            if request.get("sourceOnly"):
                if state.get("artifactReference") or state.get("imageId"):
                    raise ValueError("source preparation unexpectedly owns an image")
            else:
                reference, digest = self.publisher.recheck(request)
                if reference != state["artifactReference"] or digest != state["imageId"]:
                    raise ValueError("published image identity changed")
            result["releaseArtifactPayloads"] = payloads(request)
        return result


def log_page(headers):
    """The offset (None for the log's end) and the byte limit a log read names."""
    limit = headers.get(LOG_LIMIT_HEADER, "")
    if not limit.isdigit() or not 0 < int(limit) <= LOG_PAGE:
        raise ValueError("log page limit invalid")
    if headers.get(LOG_TAIL_HEADER) == "true":
        return None, int(limit)
    offset = headers.get(LOG_OFFSET_HEADER, "")
    if not offset.isdigit() or len(offset) > 19:
        raise ValueError("log offset invalid")
    return int(offset), int(limit)


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_POST(self):
        if not hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + self.server.token):
            self.send_error(401)
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= MAX_BODY:
                raise ValueError("body size invalid")
            request = validate(json.loads(self.rfile.read(length)))
            if self.path not in ("/releases/inspect", "/releases/start", "/releases/cancel", "/releases/cleanup-release", "/releases/log-read"):
                self.send_error(404)
                return
            with self.server.lock:
                if self.server.stopping: raise ValueError("Native build owner is stopping")
                if self.path == "/releases/log-read":
                    result = self.server.log_read(request, *log_page(self.headers))
                elif self.path == "/releases/cleanup-release":
                    result = self.server.cleanup_release(request)
                else:
                    if self.path == "/releases/start" and self.server.release_cleanup_fence(request) is not None:
                        raise ValueError("Release is permanently closed by cleanup")
                    result = self.server.inspect(request)
                key = str(directory(request) / "state.json")
                if self.path == "/releases/start" and result["state"] in ("MISSING", "FAILED"):
                    if self.server.cleanup_fence(request) is not None:
                        raise ValueError("build identity is permanently closed by cleanup")
                    credential = self.headers.get("X-Superverse-Repository-Credential", "")
                    if not credential:
                        raise ValueError("repository credential missing")
                    if result["state"] == "FAILED":
                        previous = json.loads(Path(key).read_bytes())["request"]
                        if request["attempt"] <= previous["attempt"]:
                            raise ValueError("explicit newer retry required")
                    private_json(Path(key), {"request": request, "state": "RUNNING"})
                    job = Job(request, credential, self.server.tool_image, self.server.app_manifest_tool, self.server.git_base_url, self.server.module_redirect, self.server.publisher, self.headers.get("X-Superverse-SDK-Failure-Category", ""))
                    self.server.jobs[key] = job
                    job.thread.start()
                    result = {"state": "RUNNING", "operationId": request["requestId"] + "-" + str(request["attempt"])}
                if self.path == "/releases/cancel":
                    if request["attempt"] < result.get("attempt", 0):
                        raise ValueError("Build mutation belongs to a different accepted attempt")
                    job = self.server.jobs.get(key)
                    if job is not None and job.thread.is_alive():
                        job.cancel()
                        result = {"state": "RUNNING"}
                    elif result["state"] == "MISSING" and not request.get("sourceOnly") and self.server.cleanup_fence(request) is None and self.server.release_cleanup_fence(request) is None:
                        # A Start of this attempt that arrives after its cancellation is refused:
                        # a FAILED attempt admits only a newer one.
                        private_json(Path(key), {"request": request, "state": "FAILED", "failure": "CANCELLED"})
                        result = {"state": "FAILED", "failure": "CANCELLED", "attempt": request["attempt"]}
            data = encoded(result)
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except (ValueError, OSError, KeyError, TypeError, subprocess.SubprocessError):
            self.send_error(409, "Build request could not be reconciled")


def main():
    # --state-dir selects the lane's build state for every request this process serves.
    global STATE
    parser = argparse.ArgumentParser()
    parser.add_argument("--bind", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=29792)
    parser.add_argument("--token-file", required=True, type=Path)
    parser.add_argument("--tool-image", required=True)
    parser.add_argument("--app-manifest-tool", required=True, type=Path)
    # The origin that Project source is fetched from: GitHub, or the local Git host of the local lanes on this machine.
    parser.add_argument("--git-base-url", default="https://github.com")
    # The local lanes' redirect of the connectors library, MODULE=CLONE_URL: the clone URL is the local Git host's
    # copy as a container of this machine reaches it, so a release candidate only the copy holds builds.
    parser.add_argument("--module-redirect", default="")
    # Where images go: kind loads them into the owned cluster; docker pushes them to the Native lane's registry.
    parser.add_argument("--target", choices=TARGETS, default="kind")
    parser.add_argument("--registry", default="", help="docker target: the local registry, localhost:<port>")
    parser.add_argument("--state-dir", type=Path, default=STATE, help="the lane's private build state")
    args = parser.parse_args()
    if os.environ.get("SUPERVERSE_LOCAL_NATIVE") == "true":
        if not re.fullmatch(r"[a-z0-9][a-z0-9-]{7,63}", os.environ.get("SUPERVERSE_NATIVE_RUN_ID", "")) or os.environ.get("SUPERVERSE_NATIVE_PROCESS_GROUP") != str(os.getpgrp()) or os.getpgrp() == os.getpid():
            parser.error("Native builder must inherit its exact launcher process group")
    if args.bind != "127.0.0.1" or not re.fullmatch(r"(?:[^\s@]+@)?sha256:[a-f0-9]{64}", args.tool_image):
        parser.error("explicit local bind and immutable tool image are required")
    if not args.app_manifest_tool.is_absolute() or args.app_manifest_tool.is_symlink() or not args.app_manifest_tool.is_file() or not os.access(args.app_manifest_tool, os.X_OK):
        parser.error("the trusted compiled AppManifest tool must be an absolute executable file")
    if not re.fullmatch(r"https://[A-Za-z0-9.-]+(?::[0-9]+)?|http://(?:127\.0\.0\.1|localhost)(?::[0-9]+)?", args.git_base_url):
        parser.error("the Git base URL must be an https origin, or an http origin on this machine")
    module_redirect = None
    if args.module_redirect:
        module, _, clone = args.module_redirect.partition("=")
        if not MODULE_PATH.fullmatch(module) or not CLONE_URL.fullmatch(clone):
            parser.error("the module redirect must be MODULE=CLONE_URL, an https clone URL or one of host.docker.internal")
        module_redirect = (module, clone)
    if args.token_file.stat().st_mode & 0o077:
        parser.error("token file must be private")
    token = args.token_file.read_text().strip()
    if len(token) < 32:
        parser.error("fixture token must contain at least 32 characters")
    if (args.target == "docker") != bool(REGISTRY.fullmatch(args.registry)):
        parser.error("the docker target, and only it, publishes to a local registry localhost:<port>")
    if not args.state_dir.is_absolute():
        parser.error("the build state directory must be absolute")
    STATE = args.state_dir
    publisher = RegistryPublisher(args.registry) if args.target == "docker" else KindPublisher()
    try:
        publisher.require_available()
    except ValueError as error:
        parser.error(str(error))
    STATE.mkdir(parents=True, exist_ok=True, mode=0o700)
    server = Server((args.bind, args.port), token, args.tool_image, args.app_manifest_tool, args.git_base_url, module_redirect, publisher)
    def stop(_signum, _frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, stop)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        jobs = server.jobs.values()
        if os.environ.get("SUPERVERSE_LOCAL_NATIVE") == "true":
            with server.lock:
                server.stopping = True
                jobs = list(server.jobs.values())
        for job in jobs:
            try: job.cancel()
            except (ValueError, OSError, subprocess.SubprocessError):
                if os.environ.get("SUPERVERSE_LOCAL_NATIVE") != "true": raise
                # Keep cancelling other owned jobs; launcher group exit remains
                # mandatory and the uncertain job's persisted outcome stays UNKNOWN.
        server.server_close()


if __name__ == "__main__":
    main()
