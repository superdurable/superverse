#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Trusted CodeBuild runner. Application code never executes in this process."""
import base64
import contextlib
from datetime import datetime, timezone
import hashlib
import ipaddress
import json
import os
from pathlib import Path, PurePosixPath
import re
import resource
import signal
import subprocess
import tarfile
import tempfile
import time
import uuid


# Dex reports a Step group that is neither one run nor one choice as this warning; it never blocks.
ADVISORY_DIAGNOSTICS = frozenset({"v2_step_group_shape"})


def blocking_diagnostics(graph):
    return [d for d in graph.get("diagnostics") or []
            if not (d.get("severity") == "warning" and d.get("code") in ADVISORY_DIAGNOSTICS)]


MAX_ARTIFACT = 32 * 1024 * 1024
MAX_IMAGE_ARCHIVE = 2 * 1024 * 1024 * 1024
SOURCE_DEADLINE = None
PROCESS_PATH = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"


def encoded(value):
    return json.dumps(value, ensure_ascii=False, separators=(",", ":")).encode()


def digest(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


def run(argv, *, cwd=None, timeout=1800, input_bytes=None):
    if SOURCE_DEADLINE is not None:
        timeout = min(timeout, SOURCE_DEADLINE - time.monotonic())
        if timeout <= 0: raise ValueError("source preparation deadline exceeded")
    # App-controlled output is never copied into CloudWatch or exception text.
    def output_limit():
        resource.setrlimit(resource.RLIMIT_FSIZE, (16 * 1024 * 1024, 16 * 1024 * 1024))
    with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
        process = subprocess.Popen(argv, cwd=cwd, stdin=subprocess.PIPE if input_bytes is not None else subprocess.DEVNULL,
                                   stdout=stdout, stderr=stderr, preexec_fn=output_limit)
        try:
            process.communicate(input=input_bytes, timeout=timeout)
            if process.returncode:
                raise ValueError("trusted hosted build command failed")
        finally:
            # A Docker client exiting is not proof its workload stopped. Owners
            # below separately stop/wait/remove the exact container.
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
        stdout.seek(0)
        return stdout.read(16 * 1024 * 1024)


def timing(code, started, clock):
    """The phaseTimings entry of phase code, started at started (time.time)
    and measured since clock (time.monotonic_ns). Its work is in milliseconds,
    at least one, so a measured phase never reads as running."""
    return {"phase": code, "startedAt": datetime.fromtimestamp(started, timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z"),
            "durationMs": max(1, round((time.monotonic_ns() - clock) / 1e6))}


@contextlib.contextmanager
def measured(timings, code):
    """Appends the enclosed work to timings as phase code once it succeeds."""
    started, clock = time.time(), time.monotonic_ns()
    yield
    timings.append(timing(code, started, clock))


def bounded(path, limit=MAX_ARTIFACT):
    if path.is_symlink() or not path.is_file() or path.stat().st_size > limit:
        raise ValueError("artifact must be a bounded regular file")
    with path.open("rb") as source:
        result = source.read(limit + 1)
    if len(result) > limit:
        raise ValueError("artifact size exceeded")
    return result


def protect_dependency_network():
    # This changes only this ephemeral CodeBuild Docker host, never a cluster.
    # Fixed download commands need public registries but cannot reach credentials,
    # host bridge services or VPC-private endpoints. App execution is offline.
    bridge = json.loads(run(["docker", "network", "inspect", "bridge"]))
    if len(bridge) != 1 or bridge[0].get("EnableIPv6") or bridge[0].get("Driver") != "bridge" or bridge[0].get("Name") != "bridge":
        raise ValueError("reviewed IPv4 Docker bridge is required")
    interface = bridge[0].get("Options", {}).get("com.docker.network.bridge.name")
    if not isinstance(interface, str) or not re.fullmatch(r"[a-zA-Z0-9_.-]{1,15}", interface):
        raise ValueError("exact bridge interface is required")
    for config in bridge[0].get("IPAM", {}).get("Config", []):
        if ipaddress.ip_network(config["Subnet"]).version != 4:
            raise ValueError("IPv6 bridge subnet is not supported")
    # DOCKER-USER is reached from FORWARD, not INPUT. Deny *all* traffic to
    # the CodeBuild host from this bridge, including its public/bridge addresses.
    run(["iptables", "-w", "-I", "INPUT", "1", "-i", interface, "-j", "REJECT"], timeout=30)
    for destination in ("0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
                        "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "240.0.0.0/4"):
        run(["iptables", "-w", "-I", "DOCKER-USER", "1", "-i", interface, "-d", destination, "-j", "REJECT"], timeout=30)
    for chain in ("INPUT", "FORWARD"):
        run(["ip6tables", "-w", "-I", chain, "1", "-i", interface, "-j", "REJECT"], timeout=30)
    return bridge[0]["Id"]


def owned_container(name, owner):
    ids = run(["docker", "container", "ls", "--all", "--quiet", "--no-trunc", "--filter", "name=^/" + name + "$"]).decode().split()
    if not ids:
        return None
    if len(ids) != 1 or not re.fullmatch(r"[a-f0-9]{64}", ids[0]):
        raise ValueError("ambiguous build container identity")
    container = json.loads(run(["docker", "container", "inspect", ids[0]]))[0]
    if container.get("Name") != "/" + name or container.get("Config", {}).get("Labels", {}).get("superverse-v2.build-owner") != owner:
        raise ValueError("build container ownership mismatch")
    return container


def remove_container(name, owner):
    # Work deadline expiry must still execute the owning stop/wait/remove path.
    global SOURCE_DEADLINE
    previous = SOURCE_DEADLINE
    SOURCE_DEADLINE = time.monotonic() + 30
    try:
        return _remove_container(name, owner)
    finally:
        SOURCE_DEADLINE = previous


def _remove_container(name, owner):
    # Called even after an uncertain create/start response. The chosen name and
    # owner existed before dispatch; no anonymous process loses responsibility.
    container = owned_container(name, owner)
    if container is None:
        return
    identity = container["Id"]
    if container["State"]["Running"]:
        run(["docker", "stop", "--time", "10", identity], timeout=30)
    if container["State"].get("Status") != "created":
        run(["docker", "wait", identity], timeout=30)
    observed = owned_container(name, owner)
    if observed is None or observed["Id"] != identity or observed["State"]["Running"]:
        raise ValueError("build container termination is unconfirmed")
    run(["docker", "rm", "--volumes", identity], timeout=30)
    if owned_container(name, owner) is not None:
        raise ValueError("build container cleanup is incomplete")


def tool_command(options, command, timeout=1800):
    owner = uuid.uuid4().hex
    name = "sv2-tool-" + owner
    try:
        run(["docker", "create", "--name", name, "--label", "superverse-v2.build-owner=" + owner,
             "--log-driver", "none", *options, *command], timeout=30)
        return run(["docker", "start", "--attach", name], timeout=timeout)
    finally:
        remove_container(name, owner)


def validate_image_archive(path, image):
    if path.is_symlink() or not path.is_file() or not 0 < path.stat().st_size <= MAX_IMAGE_ARCHIVE:
        raise ValueError("image archive exceeds the accepted bound")
    with tarfile.open(path, "r:") as archive:
        names, total = set(), 0
        for member in archive:
            name = PurePosixPath(member.name)
            if member.name in names or name.is_absolute() or ".." in name.parts or not (member.isfile() or member.isdir()):
                raise ValueError("invalid Docker image archive entry")
            names.add(member.name)
            total += member.size
            if len(names) > 10000 or total > MAX_IMAGE_ARCHIVE:
                raise ValueError("image archive contents exceed the accepted bound")
        manifest_entry = archive.getmember("manifest.json")
        if not manifest_entry.isfile() or manifest_entry.size > 65536:
            raise ValueError("invalid Docker image manifest")
        with archive.extractfile(manifest_entry) as data:
            manifest = json.load(data)
        if len(manifest) != 1 or manifest[0].get("RepoTags") != [image]:
            raise ValueError("image archive tag does not match this build")
        item = manifest[0]
        if not isinstance(item.get("Layers"), list) or not 0 < len(item["Layers"]) <= 256:
            raise ValueError("invalid image layer inventory")
        for name in [item.get("Config"), *item["Layers"]]:
            if not isinstance(name, str) or name not in names or not archive.getmember(name).isfile():
                raise ValueError("image archive reference is absent")


def build_image(config, request, source, work, profile, bridge_id, image):
    owner = uuid.uuid4().hex
    name = "sv2-buildkit-" + owner
    recipe, output = work / "recipe", work / "image"
    recipe.mkdir(); output.mkdir()
    # Remove only the pinned profile's external frontend directive from this
    # private recipe. The imported bytes/profile digest remain unchanged. The
    # built-in frontend is now part of the immutable HostImage executor identity.
    dockerfile = base64.b64decode(profile["dockerfile"], validate=True)
    directive = b"# syntax=docker/dockerfile:1.7\n"
    if not dockerfile.startswith(directive):
        raise ValueError("unsupported product Dockerfile frontend")
    dockerfile = dockerfile[len(directive):]
    if re.search(rb"(?im)^\s*#\s*syntax\s*=", dockerfile):
        raise ValueError("additional Dockerfile frontend directive is forbidden")
    (recipe / "Dockerfile").write_bytes(dockerfile)
    build_args = json.loads(base64.b64decode(profile["manifest"], validate=True))["buildArgs"]
    if set(build_args) != {"GO_BUILD_IMAGE", "NODE_BUILD_IMAGE", "STATIC_BASE_IMAGE"} or any(not re.fullmatch(r"[^\s@]+@sha256:[a-f0-9]{64}", value) for value in build_args.values()):
        raise ValueError("unsupported product build arguments")
    (recipe / "buildkitd.toml").write_text('''root = "/var/lib/buildkit"
insecure-entitlements = []
provenanceEnvDir = "/empty-provenance"
[grpc]
  address = ["unix:///run/buildkit/buildkitd.sock"]
[dns]
  nameservers = ["1.1.1.1", "8.8.8.8"]
[cdi]
  disabled = true
[worker.oci]
  enabled = true
  snapshotter = "native"
  networkMode = "host"
  noProcessSandbox = false
  max-parallelism = 2
[worker.containerd]
  enabled = false
[frontend."dockerfile.v0"]
  enabled = true
[frontend."gateway.v0"]
  enabled = false
''')
    clean_env = ["/usr/bin/env", "-i", "PATH=" + PROCESS_PATH, "HOME=/tmp", "DOCKER_CONFIG=/tmp/empty-docker"]
    try:
        # The trusted nested OCI worker requires privilege, but receives no
        # explicit host device/socket/config/credential/source-write mount.
        run(["docker", "create", "--name", name, "--label", "superverse-v2.build-owner=" + owner,
             "--network", "bridge", "--dns", "1.1.1.1", "--dns", "8.8.8.8", "--privileged",
             "--sysctl", "net.ipv6.conf.all.disable_ipv6=1", "--sysctl", "net.ipv6.conf.default.disable_ipv6=1",
             "--memory", "8g", "--cpus", "4", "--pids-limit", "2048", "--log-driver", "none",
             "--ulimit", f"fsize={MAX_IMAGE_ARCHIVE}:{MAX_IMAGE_ARCHIVE}",
             "-v", f"{source}:/context:ro", "-v", f"{recipe}:/recipe:ro", "-v", f"{output}:/output:rw",
             "--entrypoint", clean_env[0], config["hostImage"], *clean_env[1:],
             "buildkitd", "--config", "/recipe/buildkitd.toml"], timeout=30)
        run(["docker", "start", name], timeout=30)
        observed = owned_container(name, owner)
        networks = observed["NetworkSettings"]["Networks"] if observed else {}
        if observed is None or observed["Config"]["Image"] != config["hostImage"] or set(networks) != {"bridge"} or networks["bridge"]["NetworkID"] != bridge_id or networks["bridge"].get("GlobalIPv6Address"):
            raise ValueError("BuildKit is not on the reviewed fixed bridge")
        identity = observed["Id"]
        ctl = ["docker", "exec", identity, *clean_env, "buildctl", "--addr", "unix:///run/buildkit/buildkitd.sock"]
        deadline = time.monotonic() + 30
        while True:
            try:
                workers = json.loads(run([*ctl, "debug", "workers", "--format", "{{json .}}"], timeout=5))
                break
            except (ValueError, subprocess.TimeoutExpired):
                if time.monotonic() >= deadline:
                    raise ValueError("BuildKit worker startup was not confirmed") from None
                time.sleep(0.25)
        labels = workers[0].get("labels", {}) if len(workers) == 1 else {}
        expected = {"executor": "oci", "network": "host", "oci.process-mode": "sandbox", "snapshotter": "native"}
        if any(labels.get("org.mobyproject.buildkit.worker." + key) != value for key, value in expected.items()) or workers[0].get("buildkitVersion", {}).get("version") != "v0.25.1":
            raise ValueError("unexpected BuildKit worker configuration")
        # 'host' above is this isolated container's namespace, whose only network
        # is the inspected Docker bridge. No host-network entitlement is allowed.
        args = [*ctl, "build", "--frontend", "dockerfile.v0", "--local", "context=/context",
                "--local", "dockerfile=/recipe", "--opt", "filename=Dockerfile", "--opt", "platform=linux/arm64",
                "--output", "type=docker,name=" + image + ",dest=/output/image.tar"]
        for key in ("projectId", "sourceCommitSha", "buildProfileDigest", "environment", "buildRevisionId", "configurationDigest"):
            args += ["--opt", "label:superverse-v2." + key + "=" + request[key]]
        for key, value in sorted(build_args.items()):
            args += ["--opt", "build-arg:" + key + "=" + value]
        run(args, timeout=2400)
    finally:
        remove_container(name, owner)
    archive = output / "image.tar"
    validate_image_archive(archive, image)
    run(["docker", "load", "--input", str(archive)], timeout=300)
    archive.unlink()


def main():
    global SOURCE_DEADLINE
    host = time.time(), time.monotonic_ns()
    config = json.loads(base64.b64decode(os.environ["SV2_BUILD_CONFIG"], validate=True))
    request = config["request"]
    if request.get("sourceOnly"):
        SOURCE_DEADLINE = time.monotonic() + 240
    source = Path(os.environ["CODEBUILD_SRC_DIR"]).resolve(strict=True)
    build_id = os.environ["CODEBUILD_BUILD_ID"]
    if not build_id.startswith(config["projectName"] + ":"):
        raise ValueError("build identity mismatch")
    if os.environ.get("CODEBUILD_SOURCE_VERSION") != config["sourceVersion"]:
        raise ValueError("exact versioned source was not checked out")
    # CodeBuild source was normalized by the platform; reject any subsequent
    # unexpected symlink before a file is passed into the trusted outer process.
    if any(item.is_symlink() for item in source.rglob("*")):
        raise ValueError("source archive contains a link")
    manifest_bytes = bounded(source / "dex-app.yaml", 4 * 1024 * 1024)
    manifest = json.loads(manifest_bytes)
    entries = manifest.get("flowDefinitions")
    if manifest.get("schemaVersion") != "superverse.dev/dex-app/v1" or not isinstance(entries, list) or not entries or len(entries) > 1000:
        raise ValueError("missing or oversized Flow declaration list")
    profile = config["profile"]
    parts = [base64.b64decode(profile[name], validate=True) for name in ("manifest", "dockerfile")]
    if hashlib.sha256(b"\0".join(parts)).hexdigest() != request["buildProfileDigest"]:
        raise ValueError("build profile fingerprint mismatch")
    tool = config["toolImage"]
    password = run(["aws", "ecr", "get-login-password", "--region", config["region"]], timeout=30)
    run(["docker", "login", "--username", "AWS", "--password-stdin", config["repositoryURI"].split("/", 1)[0]], input_bytes=password, timeout=30)
    password = None
    bridge_id = protect_dependency_network()
    run(["docker", "pull", tool], timeout=600)
    run(["docker", "pull", config["hostImage"]], timeout=600)
    # Exact image digest is the authority; this label guards accidental use of
    # the coding image, which has a different toolchain contract.
    labels = json.loads(run(["docker", "image", "inspect", tool, "--format", "{{json .Config.Labels}}"] )) or {}
    if labels.get("io.superdurable.hosted-build-tools") != "v1" or labels.get("io.superdurable.dexcli") != "1.6.4":
        raise ValueError("unsupported immutable build tools")
    timings = [timing("HOST_PREPARATION", *host)]
    with tempfile.TemporaryDirectory(prefix="sv2-hosted-") as temporary:
        work = Path(temporary)
        output, dependencies = work / "output", work / "dependencies"
        output.mkdir(mode=0o777); dependencies.mkdir(mode=0o777)
        output.chmod(0o777); dependencies.chmod(0o777)
        container = ["--read-only", "--cap-drop", "ALL",
                     "--security-opt", "no-new-privileges", "--user", "65532:65532",
                     "--memory", "4g", "--cpus", "2", "--pids-limit", "512",
                     "--tmpfs", "/tmp:rw,exec,size=1g", "-e", "HOME=/tmp",
                     "-e", "GOCACHE=/tmp/go-cache", "-e", "GOMODCACHE=/dependencies",
                     "-e", "GOTOOLCHAIN=local", "-e", "CGO_ENABLED=0", "-e", "GOWORK=off",
                     "-e", "GOFLAGS=-modcacherw", "-e", "GOPROXY=https://proxy.golang.org",
                     "-e", "GOSUMDB=sum.golang.org", "-e", "GOPRIVATE=", "-e", "GONOPROXY=none",
                     "-e", "GONOSUMDB=none", "-v", f"{source}:/workspace:ro",
                     "-v", f"{output}:/out:rw", "-v", f"{dependencies}:/dependencies:rw", "-w", "/workspace"]
        # Resolve the modules declared by the accepted lockfiles. Under lazy
        # loading, downloading `all` can rewrite go.sum for dependency-only
        # packages; accepted source is read only. Offline strict FDG checks below
        # still reject missing dependencies or checksums.
        with measured(timings, "DEPENDENCY_PREPARATION"):
            for module in ("/workspace", "/workspace/tools/openapi"):
                tool_command(container, ["--network", "bridge", "--dns", "1.1.1.1", "--dns", "8.8.8.8",
                     "--sysctl", "net.ipv6.conf.all.disable_ipv6=1", "--sysctl", "net.ipv6.conf.default.disable_ipv6=1", "--entrypoint", "go", tool,
                     "-C", module, "mod", "download"], timeout=600)
        with measured(timings, "ANALYSIS"):
            manifest_artifacts = json.loads(tool_command(container, ["--network", "none", "--entrypoint", "app-manifest", tool,
                 "--manifest", "/workspace/dex-app.yaml", "--source-commit", request["sourceCommitSha"],
                 "--project-id", request["projectId"], "--release-id", request["releaseId"]], timeout=30))
            definitions, names, paths = [], set(), set()
            definition_bytes = 0
            for index, entry in enumerate(entries):
                location = entry.get("sourcePath") if isinstance(entry, dict) else None
                if not isinstance(location, str) or location.startswith("/") or ".." in Path(location).parts or not location.endswith(".go") or location in paths:
                    raise ValueError("invalid declared Flow source")
                bounded(source / location)
                paths.add(location)
                tool_command(container, ["--network", "none", "--entrypoint", "dexcli", tool,
                     "visualize", location, "--schema-version", "2.0", "--json", "--out", f"/out/flow-{index}"])
                graph = json.loads(bounded(output / f"flow-{index}.json"))
                definition_bytes += len(encoded(graph))
                if definition_bytes > MAX_ARTIFACT:
                    raise ValueError("Flow bundle size exceeded")
                (output / f"flow-{index}.json").unlink()
                name = graph.get("flow", {}).get("name")
                if graph.get("schemaVersion") != "2.0" or graph.get("valid") is not True or blocking_diagnostics(graph) or not name or name in names:
                    raise ValueError("invalid or duplicate FDG")
                names.add(name)
                definitions.append({"flowType": name, "sourcePath": location, "schemaVersion": "2.0", "digest": digest(encoded(graph)), "valid": True, "graph": graph})
            bundle = {"schemaVersion": "superverse.dev/flow-definition-bundle/v1", "projectId": request["projectId"], "releaseId": request["releaseId"],
                      "buildRevisionId": request["buildRevisionId"], "sourceCommitSha": request["sourceCommitSha"],
                      "buildProfileDigest": request["buildProfileDigest"], "flowDefinitions": sorted(definitions, key=lambda value: value["flowType"])}
            if not request.get("sourceOnly"):
                bundle["configurationDigest"] = request["configurationDigest"]
                bundle["configurationRevision"] = request["configurationRevision"]
            payloads = {"flowDefinitionBundle": encoded(bundle).decode(), "connectorContract": encoded(manifest_artifacts["connectorContract"]).decode(),
                        "environmentContract": encoded(manifest_artifacts["environmentContract"]).decode(), "dexAppManifest": manifest_bytes.decode()}
            if any(not value or len(value.encode()) > MAX_ARTIFACT for value in payloads.values()):
                raise ValueError("release payload exceeds its bounded contract")
        image_digest, artifact_reference = "", ""
        if not request.get("sourceOnly"):
            image = config["repositoryURI"] + ":" + config["imageTag"]
            with measured(timings, "COMPILATION"):
                build_image(config, request, source, work, profile, bridge_id, image)
            with measured(timings, "IMAGE_PUBLICATION"):
                run(["docker", "push", image], timeout=600)
                images = json.loads(run(["aws", "ecr", "describe-images", "--region", config["region"],
                             "--registry-id", config["accountId"], "--repository-name", config["repositoryURI"].split("/", 1)[1],
                             "--image-ids", "imageTag=" + config["imageTag"]], timeout=30))["imageDetails"]
                if len(images) != 1 or not re.fullmatch(r"sha256:[a-f0-9]{64}", images[0].get("imageDigest", "")):
                    raise ValueError("published image identity absent")
                image_digest = images[0]["imageDigest"]
                artifact_reference = config["repositoryURI"] + "@" + image_digest
        result = {"schemaVersion": "superverse.dev/hosted-build-result/v2", "executorDigest": config["executorDigest"], "request": request, "buildId": build_id,
                  "artifactReference": artifact_reference, "imageId": image_digest,
                  "releaseArtifactPayloads": payloads, "phaseTimings": timings}
        result_file = work / "result.json"
        result_file.write_bytes(encoded(result)); result_file.chmod(0o600)
        published = json.loads(run(["aws", "s3api", "put-object", "--region", config["region"], "--bucket", config["bucket"], "--expected-bucket-owner", config["accountId"],
            "--key", config["resultKey"], "--body", str(result_file), "--content-type", "application/json",
            "--server-side-encryption", "aws:kms", "--ssekms-key-id", config["kmsKeyARN"], "--if-none-match", "*"], timeout=30))
        if published.get("VersionId") in (None, "", "null"):
            raise ValueError("versioned result publication absent")


if __name__ == "__main__":
    def cancelled(signum, _frame):
        # Let owner finally blocks perform bounded container termination before
        # the CodeBuild job exits. A forced host kill still requires AWS terminal
        # reconciliation; a client interruption never proves cleanup complete.
        signal.signal(signum, signal.SIG_IGN)
        raise ValueError("hosted build cancelled")
    signal.signal(signal.SIGTERM, cancelled)
    signal.signal(signal.SIGINT, cancelled)
    try:
        main()
    except Exception:
        # Provider output and app source stay out of generic error messages.
        raise SystemExit("Hosted release build did not complete") from None
