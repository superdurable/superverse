# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Exact identities of the deployed Kind resources and the images their Pods run; read-only."""
import hashlib
import json

import yaml

from kind.cluster import NAMESPACE, SERVER_CONFIG, kubectl, read_object
from kind.node_images import component_reference, node_image_identities
from operator_errors import require


def json_digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True).encode()).hexdigest()


def pod_ready(pod):
    """A Pod that is Ready and not terminating."""
    return (not pod["metadata"].get("deletionTimestamp") and
            any(c.get("type") == "Ready" and c.get("status") == "True" for c in pod.get("status", {}).get("conditions", [])))


def accepted_image_ids(cri):
    """Every container imageID spelling that names one node's accepted CRI image."""
    return {cri["id"], "containerd://" + cri["id"], *cri["repoDigests"], *("docker-pullable://" + ref for ref in cri["repoDigests"])}


def profile_sandbox_image(configmap):
    """The image of the single sandboxd container an installed coding profile selects."""
    containers = yaml.safe_load(configmap["data"]["sandbox.yaml"])["spec"]["podTemplate"]["spec"]["containers"]
    require([c.get("name") for c in containers] == ["sandboxd"], "Installed coding profile must run exactly the sandboxd container")
    return containers[0].get("image")


def kind_deployment_identity(profile, images):
    require(set(images) == {"server", "studio", "coding"}, "Three deployment-bound component image records are required")
    node_images = node_image_identities(images)
    result = {}
    deployments = {}
    for name in ("server", "studio", "dex"):
        deployment = deployments[name] = read_object("deployment", "superverse-v2-" + name)
        meta, status = deployment["metadata"], deployment.get("status", {})
        require(status.get("observedGeneration", 0) >= meta["generation"] and status.get("availableReplicas", 0) >= deployment["spec"].get("replicas", 1),
                "Deployment is not ready: superverse-v2-" + name)
        result["deployment/superverse-v2-" + name] = {"uid": meta["uid"], "generation": meta["generation"], "templateDigest": json_digest(deployment["spec"]["template"])}
    for kind, name in [("secret", "superverse-v2-server"), ("secret", "superverse-v2-studio"), ("secret", "superverse-v2-dex-config"),
                       ("configmap", SERVER_CONFIG), ("configmap", profile)]:
        item = read_object(kind, name)
        result[kind + "/" + name] = {"uid": item["metadata"]["uid"], "resourceVersion": item["metadata"]["resourceVersion"]}
        if name == profile:
            require(profile_sandbox_image(item) == component_reference("coding", images["coding"]),
                    "Installed coding profile image differs from the deployment-bound component")
    pods = json.loads(kubectl("get", "pods", "-n", NAMESPACE, "-o", "json", capture=True))["items"]
    replica_sets = json.loads(kubectl("get", "replicasets", "-n", NAMESPACE, "-o", "json", capture=True))["items"]
    for name in ("server", "studio"):
        deployment = deployments[name]
        reference = component_reference(name, images[name])
        require([(c.get("name"), c.get("image")) for c in deployment["spec"]["template"]["spec"]["containers"]] == [(name, reference)],
                "Deployed image differs from deployment-bound component: " + name)
        owned_sets = {r["metadata"]["uid"] for r in replica_sets
                      if any(o.get("uid") == deployment["metadata"]["uid"] and o.get("controller") is True for o in r["metadata"].get("ownerReferences", []))}
        selected = [p for p in pods if any(o.get("uid") in owned_sets and o.get("controller") is True for o in p["metadata"].get("ownerReferences", []))]
        require(selected and len(selected) == deployment["spec"].get("replicas", 1), "Deployed component has missing or transitional Pods: " + name)
        observed = []
        for pod in selected:
            cri = node_images[name].get(pod["spec"].get("nodeName"))
            require(cri and pod_ready(pod), "Deployed component Pod is not ready on an accepted node: " + name)
            statuses = pod.get("status", {}).get("containerStatuses", [])
            require([(c.get("name"), c.get("image")) for c in pod["spec"]["containers"]] == [(name, reference)] and len(statuses) == 1 and
                    statuses[0].get("name") == name and statuses[0].get("ready") is True and statuses[0].get("imageID") in accepted_image_ids(cri),
                    "Running Pod image differs from the accepted CRI identity: " + name)
            observed.append({"uid": pod["metadata"]["uid"], "node": pod["spec"]["nodeName"], "configDigest": cri["id"], "manifestRef": reference})
        result["component/" + name] = sorted(observed, key=lambda p: p["uid"])
    # The profile selects future Sandbox images; no Sandbox is created just for evidence.
    result["component/coding"] = {node: {"configDigest": value["id"], "manifestRef": value["reference"]} for node, value in sorted(node_images["coding"].items())}
    return result
