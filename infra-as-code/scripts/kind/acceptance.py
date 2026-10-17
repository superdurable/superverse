# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Kind browser acceptance against the exact deployed components; its receipt is the release gate."""
import json
import os
import time

from acceptance.browser import acceptance_admission, acceptance_signals, browser_node_identity, run_browser_cases
import build_inputs
from commands import run
import git_host
from kind.cluster import CLUSTER, FORWARDS, GIT, NAMESPACE, kind_nodes, kubectl, state, verify_owner
from kind.deployment_identity import kind_deployment_identity
from kind.locks import KIND_ACCEPTANCE_DIRECTORY
from kind.server_config import model_target, require_unchanged
from operator_errors import require
from paths import ROOT, STATE
from private_files import write_private, write_private_json

RECEIPT = STATE / "e2e-acceptance.json"


def observed_rpc_coverage():
    """The business methods the server Pod has served since it started, from its mounted /tmp on its owned Kind node.

    The distroless server has no cat binary. The file holds the Pod's whole lifetime, so only a Pod that has served
    nothing before the cases credits them alone; the final identity proof requires the same Pod throughout.
    """
    pods = json.loads(kubectl("get", "pods", "-n", NAMESPACE, "-o", "json", capture=True))["items"]
    servers = [pod for pod in pods if pod["metadata"]["name"].startswith("superverse-v2-server-") and
               pod["status"].get("phase") == "Running"]
    require(len(servers) == 1, "Kind RPC coverage requires exactly one running server Pod")
    pod = servers[0]
    node = pod["spec"]["nodeName"]
    require(node in kind_nodes(), "Kind RPC coverage server Pod runs on an unowned node")
    file = "/var/lib/kubelet/pods/" + pod["metadata"]["uid"] + "/volumes/kubernetes.io~empty-dir/tmp/superverse-rpc-coverage.json"
    return run(["docker", "exec", node, "cat", file], capture=True)


def e2e(_args):
    browser_node = browser_node_identity()
    value = verify_owner()
    fingerprint = build_inputs.kind_behavior_fingerprint()
    require(value.get("deployedSourceFingerprint") == fingerprint, "Deployed source does not match checkout; rebuild and deploy")
    runtime_identity = value.get("runtimeIdentity")
    deployed_images = value.get("deployedImages")
    require(deployed_images and value.get("images") == deployed_images, "Built images differ from the accepted deployment; deploy before acceptance")
    profile = value["deploymentInputs"]["profile"]
    require(value["deploymentInputs"].get("config"), "The deployment predates the server configuration profiles; redeploy before acceptance")
    require_unchanged(value["deploymentInputs"]["config"])
    # The final acceptance runs the model the platform developer decided, which the receipt records; the browser specs get it from the environment.
    target = model_target(value["deploymentInputs"]["config"], os.environ)
    require(runtime_identity and kind_deployment_identity(profile, deployed_images) == runtime_identity,
            "Deployed resources/configuration changed; redeploy before acceptance")

    def check_environment_identity():
        # The acceptance holds the Kind action lock, so no Kind command changes the deployment meanwhile; this reads only
        # the lane's own state, and the final proof below compares the cluster's resources.
        current = state()
        require(current.get("deployedImages") == deployed_images and current.get("runtimeIdentity") == runtime_identity,
                "The Kind deployment changed during browser acceptance")
    with acceptance_signals(), acceptance_admission(KIND_ACCEPTANCE_DIRECTORY) as fence:
        # Refused before the fence: anything the server served earlier would count as the cases' RPC coverage.
        served = json.loads(observed_rpc_coverage())["methods"]
        require(not served, f"The Kind server has served {len(served)} business RPCs since its Pod started, which the cases' RPC "
                "coverage would include; delete the server Pod, run sv2 kind deploy again and restart sv2 kind ports")
        results = run_browser_cases(KIND_ACCEPTANCE_DIRECTORY, fence, browser_node, check_environment_identity, lane="Kind",
                                    studio=f"http://localhost:{FORWARDS['studio'].local_port}", git=GIT, local_git_url=git_host.HOST_URL,
                                    repository_host=git_host.repository_host(value["deploymentInputs"]["config"]["input"]))
        current = state()
        require(current.get("deployedImages") == deployed_images and current.get("images") == deployed_images and
                current.get("runtimeIdentity") == runtime_identity and build_inputs.kind_behavior_fingerprint() == fingerprint and
                kind_deployment_identity(profile, deployed_images) == runtime_identity,
                "Source or deployed resource identity changed during E2E; no acceptance receipt emitted")
        observed = observed_rpc_coverage()
        coverage = STATE / "rpc-coverage.json"
        write_private(coverage, observed)
        print(run(["python3", ROOT / "scripts/rpc-coverage-map.py", "merge", "kind-e2e", coverage], capture=True))
        receipt = {"schema": 2, "runtimeIdentity": runtime_identity, "environment": "local-kind", "cluster": CLUSTER, "sourceFingerprint": fingerprint,
                   "commonSourceFingerprint": value["commonSourceFingerprint"], "images": deployed_images, "deploymentInputs": value["deploymentInputs"],
                   "serverConfiguration": value["deploymentInputs"]["config"]["profile"],
                   "mockModel": bool(value["deploymentInputs"].get("mockModel")), "agentModel": value["deploymentInputs"].get("agentModel"), "modelTarget": target,
                   "cases": results, "passedAt": int(time.time()), "browserNode": browser_node}
        write_private_json(RECEIPT, receipt)
        # Only normal completion of every case and final proof removes the fence.
        fence.unlink()
        print("Three complete browser scenarios passed; receipt: " + str(RECEIPT))
