// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Explicit development data only; never a production or acceptance backend.
import type { DeploymentSnapshot } from "@/lib/generated/protobuf/superverse/v2/DeploymentSnapshot";
import type { FlowDefinition } from "@/lib/generated/protobuf/superverse/v2/FlowDefinition";
import type { PhaseTiming } from "@/lib/generated/protobuf/superverse/v2/PhaseTiming";
import type { PreviewSnapshot } from "@/lib/generated/protobuf/superverse/v2/PreviewSnapshot";
import type { ProjectSnapshot } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import type { ReleaseSnapshot } from "@/lib/generated/protobuf/superverse/v2/ReleaseSnapshot";
import type { SessionSnapshot } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import { encodeStruct, isoToTimestamp } from "@/lib/shared/protobuf";
import { loadProjectFlowDefinitions } from "@/mock/fixture/flow-runs";
import { snapshotForSession } from "./agent-state";
import type { MockPhaseTiming } from "./clock";
import type { MockCancellation, MockDeployment, MockRelease } from "./publishing";
import { mockConfigurationDraft } from "./configuration";
import { previewSetupSnapshot } from "./setup/preview-setup";
import { getProject, getSession, type MockMainCommit, type MockStore } from "./store";

// The store keeps persisted status values; these snapshots are the generated messages the
// backend's codec round trip completes, so tsc rejects a status name the protocol lacks.

/** The snapshots in one of `statuses`; no statuses lists them all, as Go's List methods do. */
export function withStatuses<T extends { status?: unknown }>(items: T[], statuses: readonly T["status"][]): T[] {
  return statuses.length ? items.filter((item) => statuses.includes(item.status)) : items;
}

function mockPhaseTimings(timings: MockPhaseTiming[]): PhaseTiming[] {
  return timings.map((timing) => ({ ...timing, startedAt: isoToTimestamp(timing.startedAt) }));
}

function mockCancellation(cancellation: MockCancellation | undefined) {
  return cancellation && { ...cancellation, requestedAt: isoToTimestamp(cancellation.requestedAt) };
}

export function mockDeploymentSnapshot(current: MockDeployment): DeploymentSnapshot {
  return {
    projectId: current.projectId,
    deploymentId: current.id,
    releaseId: current.releaseId,
    status: `DEPLOYMENT_STATUS_${current.status}`,
    revision: current.revision,
    generation: current.generation,
    buildRevisionId: current.buildRevisionId,
    configurationRevision: current.configurationRevision,
    url: current.url ?? "",
    createdAt: isoToTimestamp(current.updatedAt),
    phaseTimings: mockPhaseTimings(current.phaseTimings),
    failure: current.failure,
    cancellation: mockCancellation(current.cancellation),
    version: current.version,
  };
}

export function mockReleaseSnapshot(current: MockRelease): ReleaseSnapshot {
  const { preparedSource } = current;
  return {
    projectId: current.projectId,
    releaseId: current.id,
    sourceCommit: current.sourceCommitSha,
    status: `RELEASE_STATUS_${current.state}`,
    revision: current.revision,
    buildProfileVersion: current.buildProfile,
    createdAt: isoToTimestamp(current.createdAt),
    preparedSource: {
      ...preparedSource,
      status: `RELEASE_STATUS_${preparedSource.status}`,
      // Only a Preview draft's commit can be null; the codec encodes null and empty alike.
      appManifest: { ...preparedSource.appManifest, sourceCommit: preparedSource.appManifest.sourceCommit ?? "" },
      phaseTimings: mockPhaseTimings(preparedSource.phaseTimings),
    },
    activeBuildRevisionId: current.activeBuildRevisionId,
    readyBuildRevisionId: current.readyBuildRevisionId,
    buildRevisions: current.buildRevisions.map((build) => ({
      ...build,
      status: `RELEASE_STATUS_${build.status}`,
      autoDeployStatus: build.autoDeployStatus ? `AUTO_DEPLOY_STATUS_${build.autoDeployStatus}` : undefined,
      createdAt: isoToTimestamp(build.createdAt),
      phaseTimings: mockPhaseTimings(build.phaseTimings),
      cancellation: mockCancellation(build.cancellation),
    })),
  };
}

export function mockProjectSnapshot(store: MockStore, projectId: string): ProjectSnapshot | null {
  const current = getProject(store, projectId);
  if (!current) return null;
  const live = store.liveDeployments.get(projectId);
  const active = [...store.deployments.values()].find(
    (item) => item.projectId === projectId && (item.status === "DEPLOYING" || item.status === "CANCELLING"),
  );
  return {
    projectId,
    name: current.displayName,
    activeDeployment: active && { deploymentId: active.id, generation: active.generation },
    status: current.state === "ACTIVE" ? "PROJECT_STATUS_READY" : `PROJECT_STATUS_${current.state}`,
    revision: current.revision,
    live: live && {
      deploymentId: live.id,
      releaseId: live.releaseId,
      buildRevisionId: live.buildRevisionId,
      // The Live generation is the mock's Live pointer revision.
      revision: live.generation,
      url: live.url ?? "",
      version: mockCommitVersion(store, projectId, live.releaseId),
    },
    createdAt: isoToTimestamp(current.createdAt),
    updatedAt: isoToTimestamp(current.updatedAt),
    repository: {
      repositoryId: current.repository.githubRepositoryId,
      fullName: `${current.repository.owner}/${current.repository.name}`,
      defaultBranch: current.repository.defaultBranch,
    },
  };
}

export function mockPreviewSnapshot(store: MockStore, projectId: string): PreviewSnapshot | null {
  const current = getSession(store, projectId);
  if (!current) return null;
  const { preview } = current;
  return {
    projectId,
    status: `PREVIEW_STATUS_${preview.state}`,
    revision: preview.revision,
    generation: preview.generation,
    url: preview.url ?? "",
    preparedSource: preview.preparedSource && {
      ...preview.preparedSource,
      sourceCommit: preview.preparedSource.sourceCommit ?? "",
    },
    phaseTimings: mockPhaseTimings(preview.phaseTimings),
    workspaceRequestId: preview.workspaceRequestId,
    sourceCommit: preview.sourceCommit,
    definitionRevision: preview.definitionRevision,
    servingRevision: preview.servingRevision,
    automaticDeployment: preview.automaticDeployment && {
      commit: preview.automaticDeployment.commit,
      status: `PREVIEW_DEPLOYMENT_STATUS_${preview.automaticDeployment.status}`,
      updatedAt: isoToTimestamp(preview.automaticDeployment.updatedAt),
    },
    configurationGaps: preview.configurationGaps,
    servingVersion: preview.sourceCommit ? mockCommitVersion(store, projectId, preview.sourceCommit) : null,
    ...previewSetupSnapshot(mockConfigurationDraft(store, projectId, "preview").setup),
  };
}

/** The Project's Session with its conversation, workspace and Preview. */
export function mockSessionSnapshot(store: MockStore, projectId: string): SessionSnapshot | null {
  const current = getSession(store, projectId);
  if (!current) return null;
  const { workspace } = current;
  return {
    projectId,
    status: `SESSION_STATUS_${current.state}`,
    revision: current.revision,
    headCommit: current.headCommitSha ?? "",
    createdAt: isoToTimestamp(current.createdAt),
    updatedAt: isoToTimestamp(current.updatedAt),
    agent: snapshotForSession(store, projectId),
    workspace: {
      workspaceRequestId: workspace.workspaceRequestId,
      status: `WORKSPACE_STATUS_${workspace.state}`,
      generation: workspace.generation,
      workspaceState: "WORKSPACE_STATE_CLEAN",
      expiresAt: isoToTimestamp(workspace.expiresAt),
      phaseTimings: mockPhaseTimings(workspace.phaseTimings),
    },
    preview: mockPreviewSnapshot(store, projectId),
    // The mock Session contributes no connector; a real one reads its contribution Flows.
    connectorContributions: [],
  };
}

/** The Flow Definitions every mock prepared source carries: the Project's recorded ones. */
export async function mockFlowDefinitions(): Promise<FlowDefinition[]> {
  return (await loadProjectFlowDefinitions()).map((item) => ({ ...item, definition: encodeStruct(item.graph) }));
}

/** The Project's newest 100 main-branch commits, newest first. */
/** The mock's "V{n} · subject": V0 is the Project's oldest commit, then V1, V2 by commit time. */
export function mockCommitVersion(store: MockStore, projectId: string, commit: string) {
  const commits = listReleaseCandidates(store, projectId);
  const index = commits.findIndex((item) => item.sourceCommitSha === commit);
  return index < 0 ? null : { number: String(commits.length - 1 - index), subject: commits[index].title };
}

export function listReleaseCandidates(store: MockStore, projectId: string): MockMainCommit[] {
  return store.mainCommits
    .filter((item) => item.projectId === projectId)
    .sort((left, right) => right.committedAt.localeCompare(left.committedAt))
    .slice(0, 100);
}
