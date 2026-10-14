# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Read-only identities of the images loaded into the owned Kind nodes."""
import json
import os
import re
import selectors
import subprocess
import time

import build_inputs
from commands import run
from image_identity import DIGEST, MAX_METADATA, graph, verify_docker_root
from kind.cluster import kind_nodes
from operator_errors import require
from paths import ROOT

CODING_REPOSITORY = "docker.io/superverse-v2/coding-sandbox"


def ctr(node, *args, **kwargs):
    """containerd on one Kind node, in the namespace Kubernetes uses."""
    return run(["docker", "exec", node, "ctr", "-n", "k8s.io", *args], **kwargs)


def loaded_manifest_digest(node, reference):
    """The one manifest digest that a node's containerd holds for an image reference."""
    rows = [line.split() for line in ctr(node, "images", "ls", capture=True).splitlines()]
    digests = [row[2] for row in rows if row and row[0] == reference]
    require(len(digests) == 1 and DIGEST.fullmatch(digests[0]), "No unique loaded manifest for " + reference)
    return digests[0]


def node_reader(node):
    """Read exact content bytes from one node's containerd store, bounded in size and time."""

    def read(digest, _kind):
        process = None
        selector = selectors.DefaultSelector()
        try:
            process = subprocess.Popen(["docker", "exec", node, "ctr", "-n", "k8s.io", "content", "get", digest],
                                       cwd=ROOT, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
            os.set_blocking(process.stdout.fileno(), False)
            selector.register(process.stdout, selectors.EVENT_READ)
            deadline, raw = time.monotonic() + 30, bytearray()
            while True:
                remaining = deadline - time.monotonic()
                require(remaining > 0 and selector.select(remaining), "Kind image metadata read timed out")
                chunk = os.read(process.stdout.fileno(), min(65536, MAX_METADATA + 1 - len(raw)))
                if not chunk:
                    break
                raw.extend(chunk)
                require(len(raw) <= MAX_METADATA, "Kind image metadata exceeds its bound")
            require(process.wait(timeout=max(0.1, deadline - time.monotonic())) == 0, "Accepted Kind image metadata is unavailable")
            return bytes(raw)  # Preserve bytes; stripping whitespace changes the digest.
        except (OSError, subprocess.TimeoutExpired):
            raise RuntimeError("Accepted Kind image metadata is unavailable") from None
        finally:
            selector.close()
            if process is not None:
                process.stdout.close()
                if process.poll() is None:
                    process.kill()
                process.wait(timeout=5)
    return read


def component_reference(name, image):
    """The digest-qualified reference a deployment uses for a built component."""
    reference = image.get("manifestRef", "")
    require(re.fullmatch(r"[a-z0-9][a-z0-9./:_-]+@sha256:[a-f0-9]{64}", reference) and DIGEST.fullmatch(image.get("id", "")),
            "Deployment requires exact Docker and loaded descriptor identities: " + name)
    return reference


def node_graphs(reference, platform):
    """The verified OCI identity and config of one reference on every owned Kind node."""
    return {node: graph(node_reader(node), reference.split("@", 1)[1], platform) for node in kind_nodes()}


def observe(name, image):
    """Identity of a freshly loaded component, bound to the Docker image it was loaded from."""
    graphs = list(node_graphs(component_reference(name, image), "linux/" + build_inputs.architecture()).values())
    identity, config = graphs[0]
    require(all(other == identity for other, _ in graphs), "Kind nodes disagree about image content")
    verify_docker_root(image["id"], identity, config)
    return identity


def node_image_identities(images):
    """Per-node CRI identity of each deployment-bound image; content must equal its recorded identity."""
    result = {}
    for name in sorted(images):
        image = images[name]
        reference = component_reference(name, image)
        accepted = image["identity"]
        result[name] = {}
        for node, (observed, _) in node_graphs(reference, accepted["platform"]).items():
            require(observed == accepted, "Loaded image content differs from its deployment-bound identity: " + name)
            status = json.loads(run(["docker", "exec", node, "crictl", "inspecti", reference], capture=True))["status"]
            require(status.get("id") == accepted["config"]["digest"] and reference in status.get("repoDigests", []),
                    "Deployed component does not resolve to the accepted CRI config: " + name)
            result[name][node] = {"id": status["id"], "reference": reference, "repoDigests": sorted(status["repoDigests"])}
    return result
