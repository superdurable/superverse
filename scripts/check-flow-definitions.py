#!/usr/bin/env python3
# Copyright (c) 2026 Super Durable, Inc.
#
# Licensed under the Sustainable Use License 1.0.
# You may not use this file except in compliance with the License.
# See the LICENSE file in the repository root.
#
# SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"""Regenerate exact FDG 2.0 for every registered platform Flow and reject drift."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys


# Dex reports a Step group that is neither one run nor one choice as this warning; it never blocks.
ADVISORY_DIAGNOSTICS = frozenset({"v2_step_group_shape"})


def blocking_diagnostics(graph):
    return [d for d in graph.get("diagnostics") or []
            if not (d.get("severity") == "warning" and d.get("code") in ADVISORY_DIAGNOSTICS)]


ROOT = Path(__file__).resolve().parents[1]
FLOWS = (
    ("repository", "projects/repobinding/repository_binding_flow", "RepositoryBindingFlow"),
    ("project", "projects/project_flow", "ProjectFlow"),
    ("setup", "projects/setup/project_setup_flow", "ProjectSetupFlow"),
    ("session-agent", "agents/session/session_agent_flow", "SessionAgentFlow"),
    ("subagent", "agents/session/subagent/subagent_flow", "SubagentFlow"),
    ("platform-issue", "agents/platformissue/platform_issue_flow", "PlatformIssueFlow"),
    ("connector-contribution", "agents/session/contribution/connector_contribution_flow", "ConnectorContributionFlow"),
    ("background-command", "agents/session/backgroundcommand/background_command_flow", "BackgroundCommandFlow"),
    ("session-workspace", "agents/sandbox/sessionworkspace/session_workspace_flow", "SessionWorkspaceFlow"),
    ("sandbox-provision", "agents/sandbox/provision/sandbox_provision_flow", "SandboxProvisionFlow"),
    ("preview", "agents/sandbox/preview/preview_flow", "PreviewFlow"),
    ("releases", "delivery/releases/project_release_flow", "ProjectReleaseFlow"),
    ("deployments", "delivery/deployments/project_deploy_flow", "ProjectDeployFlow"),
    ("teamspace", "tenancy/teamspaces/team_space_flow", "TeamSpaceFlow"),
)
AGENT_CHANNELS = (
    "answeredUserInputsChannel", "queuedUserMessagesChannel",
    "steeredUserMessagesChannel", "toolApprovalsChannel",
    "parallelToolResultsChannel", "sessionNotificationsChannel",
    "workspaceOutcomesChannel", "backgroundCommandStopsChannel",
)
PREVIEW_CHANNELS = ("PreviewCommands", "WorkspaceSettled")
SUBAGENT_CHANNELS = ("parallelToolResultsChannel",)
CONTRIBUTION_CHANNELS = ("endChannel",)
WORKSPACE_CHANNELS = ("workspaceCommandsChannel", "provisionOutcomesChannel")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def referenced_resources(value):
    if isinstance(value, dict):
        if "resourceId" in value:
            yield value["resourceId"]
        for child in value.values():
            yield from referenced_resources(child)
    elif isinstance(value, list):
        for child in value:
            yield from referenced_resources(child)


def verify(graph, source, flow, name):
    require(graph.get("schemaVersion") == "2.0" and graph.get("valid") is True and blocking_diagnostics(graph) == [],
            f"{flow}: strict FDG 2.0 requires validity and no diagnostic but the Step group shape warning")
    require(graph.get("source") == {"language": "go", "path": source} and graph.get("flow", {}).get("name") == flow,
            f"{flow}: generated source/type identity differs")
    nodes = {node["id"]: node for node in graph.get("nodes", [])}
    channels = {"session-agent": AGENT_CHANNELS, "preview": PREVIEW_CHANNELS, "subagent": SUBAGENT_CHANNELS, "connector-contribution": CONTRIBUTION_CHANNELS, "session-workspace": WORKSPACE_CHANNELS}.get(name, ())
    for channel in channels:
        resource = "resource:channel:" + channel
        require(resource in nodes and any(resource in set(referenced_resources(node.get("wait", {}))) for node in nodes.values()),
                f"{flow}: missing Channel or WaitFor edge: {channel}")
    if name == "session-agent":
        require(any(node.get("name") == "ExecuteTool" for node in nodes.values()), "SessionAgent: missing canonical ExecuteTool")
    if name == "subagent":
        for identity in ("rpc:StopSubagent", "rpc:GetSubagentSnapshot", "resource:attribute:projectIDAttribute", "resource:attribute:statusAttribute"):
            require(identity in nodes, f"Subagent: missing stop, read or cleanup index: {identity}")
    if name == "connector-contribution":
        for identity in ("rpc:UpdateConnectorContribution", "rpc:CloseConnectorContribution", "rpc:GetConnectorContributionSnapshot",
                         "resource:attribute:projectIDAttribute", "resource:attribute:statusAttribute"):
            require(identity in nodes, f"ConnectorContribution: missing update, close, read or cleanup index: {identity}")
    if name == "releases":
        build_map = nodes.get("resource:attribute:ReleaseBuildRevisions", {}).get("resource", {})
        require(build_map.get("map") is True and build_map.get("valueType") == "ReleaseBuildRevision",
                "Release: missing exact build revision AttributeMap")
        required = (
            "resource:attribute:PreparedSource",
            "resource:attribute:ActiveBuildRevisionID", "resource:attribute:ReadyBuildRevisionID",
            "resource:channel:ReleaseBuildCommands", "rpc:GetReleaseBuildRevision",
            "rpc:RequestReleaseBuildRevision", "rpc:RequestReleaseBuildRecovery", "rpc:RequestReleaseSourcePreparation",
        )
        for identity in required:
            require(identity in nodes, f"Release: missing publishing primitive/RPC: {identity}")
    if name == "deployments":
        require("resource:attribute:BuildRevisionID" in nodes, "Deployment: missing immutable build identity")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("dexcli", type=Path)
    parser.add_argument("--write", action="store_true", help="Replace checked-in graphs with validated native CLI output")
    args = parser.parse_args()
    cli = args.dexcli.resolve(strict=True)
    environment = {**os.environ, "GOWORK": "off"}
    failures = []
    for name, stem, flow in FLOWS:
        source = "internal/" + stem + ".go"
        target = ROOT / "docs/flows" / ("sv2-" + name + "-flow.json")
        try:
            result = subprocess.run([str(cli), "visualize", source, "--language", "go", "--schema-version", "2.0", "--json"],
                                    cwd=ROOT, env=environment, capture_output=True, timeout=120, check=False)
            require(result.returncode == 0, f"{flow}: pinned CLI failed: {result.stderr.decode(errors='replace')[:2000]}")
            graph = json.loads(result.stdout)
            verify(graph, source, flow, name)
            if args.write:
                target.write_bytes(result.stdout)
            else:
                require(target.read_bytes() == result.stdout, f"{target.relative_to(ROOT)}: generated graph differs; run this checker with --write")
            print(f"{flow}: valid FDG 2.0, {len(graph['nodes'])} nodes, " + ("written" if args.write else "exact match"))
        except (OSError, ValueError, subprocess.TimeoutExpired) as error:
            failures.append(str(error))
    if failures:
        print("\n".join(failures), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
