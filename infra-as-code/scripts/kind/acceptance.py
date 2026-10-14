# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Kind browser acceptance against the exact deployed components; its receipt is the release gate."""
import os
import time

from acceptance.browser import acceptance_admission, acceptance_signals, browser_node_identity, run_browser_cases
import build_inputs
import git_host
from kind.cluster import CLUSTER, FORWARDS, GIT, state, verify_owner
from kind.deployment_identity import kind_deployment_identity
from kind.locks import KIND_ACCEPTANCE_DIRECTORY
from kind.server_config import model_target, require_unchanged
from operator_errors import require
from paths import STATE
from private_files import write_private_json

RECEIPT = STATE / "e2e-acceptance.json"


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
        results = run_browser_cases(KIND_ACCEPTANCE_DIRECTORY, fence, browser_node, check_environment_identity, lane="Kind",
                                    studio=f"http://localhost:{FORWARDS['studio'].local_port}", git=GIT, local_git_url=git_host.HOST_URL,
                                    repository_host=git_host.repository_host(value["deploymentInputs"]["config"]["input"]))
        current = state()
        require(current.get("deployedImages") == deployed_images and current.get("images") == deployed_images and
                current.get("runtimeIdentity") == runtime_identity and build_inputs.kind_behavior_fingerprint() == fingerprint and
                kind_deployment_identity(profile, deployed_images) == runtime_identity,
                "Source or deployed resource identity changed during E2E; no acceptance receipt emitted")
        receipt = {"schema": 2, "runtimeIdentity": runtime_identity, "environment": "local-kind", "cluster": CLUSTER, "sourceFingerprint": fingerprint,
                   "commonSourceFingerprint": value["commonSourceFingerprint"], "images": deployed_images, "deploymentInputs": value["deploymentInputs"],
                   "serverConfiguration": value["deploymentInputs"]["config"]["profile"],
                   "mockModel": bool(value["deploymentInputs"].get("mockModel")), "agentModel": value["deploymentInputs"].get("agentModel"), "modelTarget": target,
                   "cases": results, "passedAt": int(time.time()), "browserNode": browser_node}
        write_private_json(RECEIPT, receipt)
        # Only normal completion of every case and final proof removes the fence.
        fence.unlink()
        print("Three complete browser scenarios passed; receipt: " + str(RECEIPT))
