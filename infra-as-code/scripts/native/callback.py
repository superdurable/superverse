# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Bounded, read-only proof that platform Dex reaches the Native Worker through an existing Flow.

Only fixed reads repeat, and the one invoked RPC is the Attribute-only GetDexSummary. CLI output
may carry private Flow state: it stays in memory and never reaches logs or the run record.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

from commands import run
from operator_errors import require
from paths import ROOT
from pins import DEXCLI_COMMIT, DEXCLI_VERSION

FLOW_PREFIXES = {
    "RepositoryBindingFlow": "repository-",
    "ProjectFlow": "project-", "ProjectSetupFlow": "setup-",
    "SessionAgentFlow": "session-", "SandboxProvisionFlow": "sandbox-provision-", "PreviewFlow": "preview-",
    "ProjectReleaseFlow": "release-", "ProjectDeployFlow": "deploy-",
}
# Deadline exceeded, not found, resource exhausted, failed precondition, aborted and unavailable are transient.
RETRYABLE_GRPC_CODES = (4, 5, 8, 9, 10, 14)


class RetryRead(Exception):
    """A transient read failure; discovery restarts from the current run."""


def dexcli():
    """The repository-pinned dexcli: the binary `make install-dexcli` installs, or the one DEXCLI names, as app-template generation does."""
    cli = Path(os.environ.get("DEXCLI") or "/tmp/superverse-v2-tools/dexcli-v" + DEXCLI_VERSION)
    require(cli.is_file(), "Native readiness requires the repository-pinned dexcli; run make install-dexcli or set DEXCLI")
    try:
        run([sys.executable, ROOT / "scripts/check-dex-versions.py", "--dexcli-binary", cli], private=True, timeout=30)
    except RuntimeError:
        raise RuntimeError("Native readiness dexcli identity differs from the pinned release") from None
    return cli


def dex_answers(cli, dex_address):
    """Platform Dex serves its API: the server information the SDK reads first. A published port accepts before the process listens."""
    result = subprocess.run([str(cli), "api", "call", "GetServerInfo", "--server", dex_address, "--output", "json", "--timeout", "5s", "--data", "{}"],
                            cwd=ROOT, capture_output=True, text=True, timeout=15)
    require(result.returncode == 0 and isinstance(json.loads(result.stdout or "null"), dict), "Platform Dex does not answer yet")


def callback_readiness(dex_address, worker_address, headless, cancelled, *, seconds=120):
    cli = dexcli()
    identity = {"cliVersion": DEXCLI_VERSION, "cliCommit": DEXCLI_COMMIT, "cliDigest": hashlib.sha256(cli.read_bytes()).hexdigest()}
    return {**identity, **CallbackProbe(cli, dex_address, time.monotonic() + seconds, cancelled).verify(worker_address, headless)}


class CallbackProbe:
    def __init__(self, cli, dex_address, deadline, cancelled):
        self.cli, self.dex_address, self.deadline, self.cancelled = cli, dex_address, deadline, cancelled

    def verify(self, worker_address, headless):
        attempts = 0
        other_workers = 0
        saw_execution = False
        while True:
            try:
                for candidate in self.candidates():
                    saw_execution = True
                    flow_id = candidate.get("flowId")
                    require(isinstance(flow_id, str) and flow_id, "Native readiness candidate lacks identity")
                    flow_type, run_id = self.current(flow_id)
                    require(candidate.get("flowType") == flow_type, "Native readiness candidate type changed")
                    config = self.read("GetFlowState", {"flowId": flow_id, "runId": run_id}).get("flowConfig", {})
                    target = config.get("workerTarget", {}) if isinstance(config, dict) else {}
                    require(isinstance(target, dict) and isinstance(target.get("address"), str) and target["address"],
                            "Existing Flow Worker target is missing")
                    # Real integration tests can retain Flows on their own temporary Workers. They are
                    # not a callback probe of the Native server; never invoke an RPC on another target.
                    if target["address"] != worker_address or target.get("isHeadlessAddress", False) is not headless:
                        other_workers += 1
                        continue
                    attempts += 1
                    self.read("InvokeRPC", {"flowId": flow_id, "runId": run_id, "rpcName": "GetDexSummary",
                                            "requestId": str(uuid.uuid4()), "timeoutSeconds": 8, "input": {"nullValue": "NULL_VALUE"}})
                    if self.current(flow_id) != (flow_type, run_id):
                        raise RetryRead()
                    return {"level": "CALLBACK_VERIFIED", "flowType": flow_type, "flowIdSHA256": hashlib.sha256(flow_id.encode()).hexdigest(),
                            "runId": run_id, "workerAddress": worker_address, "workerHeadless": headless, "attempts": attempts,
                            "otherWorkerCandidates": other_workers}
                # Open platform Flows that all run on other Workers mean this Worker is not the lane's.
                # Without any open platform Flow, nothing can be invoked: explicitly weaker than a callback proof.
                require(not saw_execution, "Open platform Flows run only on other Workers; Native readiness is incomplete")
                return {"level": "TRANSPORT_ONLY_NO_OPEN_FLOW", "attempts": attempts}
            except RetryRead:
                require(time.monotonic() < self.deadline, "Native Dex callback readiness deadline expired")
                # Discovery re-resolves the current run after a run change, closure, visibility races or transport loss.
                for _ in range(4):
                    require(not self.cancelled(), "Native callback readiness was interrupted")
                    time.sleep(min(0.25, max(0, self.deadline - time.monotonic())))

    def candidates(self):
        """Running platform Flows."""
        query = "ExecutionStatus = 'Running' AND (" + " OR ".join("FlowType = '" + name + "'" for name in FLOW_PREFIXES) + ")"
        token, seen = "", set()
        for _ in range(4):
            page = self.read("SearchFlows", {"query": query, "pageSize": 25, "nextPageToken": token})
            entries = page.get("flowRuns", [])
            require(isinstance(entries, list) and all(isinstance(entry, dict) for entry in entries), "Native readiness search response is invalid")
            yield from entries
            token = page.get("nextPageToken", "")
            require(isinstance(token, str), "Native readiness search cursor is invalid")
            if not token:
                break
            require(token not in seen, "Native readiness search cursor repeated")
            seen.add(token)
        else:
            raise RuntimeError("Native callback candidate discovery exceeded its bound")

    def current(self, flow_id):
        """The Flow's type and current run, which must still be running."""
        value = self.read("GetFlowSummary", {"flowId": flow_id})
        execution = value.get("flowExecutionId", {})
        flow_type = value.get("flowType")
        require(isinstance(execution, dict) and execution.get("flowId") == flow_id and flow_type in FLOW_PREFIXES, "Native readiness Flow identity is incompatible")
        run_id = execution.get("runId")
        require(isinstance(run_id, str) and run_id and flow_id.startswith(FLOW_PREFIXES[flow_type]), "Native readiness Flow identity is invalid")
        if value.get("flowStatus") != "FLOW_STATUS_RUNNING":
            raise RetryRead()
        return flow_type, run_id

    def read(self, method, payload):
        arguments = ["api", "call", method, "--server", self.dex_address, "--output", "json", "--no-hydrate", "--timeout", "8s", "--data", "-"]
        if method == "InvokeRPC":
            # The generic call needs CLI confirmation; only the Attribute-only summary is ever invoked,
            # without locks, transactional mode, Channels, AttributeMaps or Step movements.
            require(set(payload) == {"flowId", "runId", "rpcName", "requestId", "timeoutSeconds", "input"} and payload["rpcName"] == "GetDexSummary" and
                    payload["input"] == {"nullValue": "NULL_VALUE"}, "Native readiness may invoke only GetDexSummary")
            arguments.append("--yes")
        try:
            value = json.loads(self.cli_call(arguments, payload))
        except ValueError:
            raise RuntimeError("Native callback readiness returned invalid JSON") from None
        require(isinstance(value, dict), "Native callback readiness returned invalid response")
        return value

    def cli_call(self, arguments, payload):
        require(not self.cancelled(), "Native callback readiness was interrupted")
        remaining = self.deadline - time.monotonic()
        require(remaining > 0, "Native Dex callback readiness deadline expired")
        # Not commands.run: the structured gRPC status on stderr decides whether the read repeats.
        try:
            result = subprocess.run([str(self.cli), *arguments], cwd=ROOT, input=json.dumps(payload), text=True, capture_output=True, timeout=min(12, remaining))
        except subprocess.TimeoutExpired:
            raise RetryRead() from None
        require(not self.cancelled(), "Native callback readiness was interrupted")
        if result.returncode:
            # The CLI may print an update notice before its structured error; classify the status
            # code only and never echo provider error text.
            error = {}
            for line in reversed(result.stderr.splitlines()):
                try:
                    value = json.loads(line)
                except ValueError:
                    continue
                if isinstance(value, dict) and isinstance(value.get("error"), dict):
                    error = value["error"]
                    break
            if error.get("grpcCode") in RETRYABLE_GRPC_CODES:
                raise RetryRead()
            raise RuntimeError("Native callback readiness CLI read failed; callback remains unverified")
        return result.stdout
