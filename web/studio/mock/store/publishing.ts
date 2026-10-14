// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Explicit in-memory development fixtures. No provider calls or credential values.
// Releases, their builds and the Live deployment.
import { createHash } from "node:crypto";
import type { CommitVersion } from "@/lib/generated/protobuf/superverse/v2/CommitVersion";
import type { Failure__Output } from "@/lib/generated/protobuf/superverse/v2/Failure";
import type { PreviewValidation } from "@/lib/generated/protobuf/superverse/v2/PreviewValidation";
import { admittedValidation } from "./validation/preview";
import type { StartDeploymentRequest__Output } from "@/lib/generated/protobuf/superverse/v2/StartDeploymentRequest";
import type { StartReleaseBuildRequest__Output } from "@/lib/generated/protobuf/superverse/v2/StartReleaseBuildRequest";
import { rpcFailure } from "@/lib/server/grpc-client";
import { httpError } from "@/lib/server/request-body";
import { REVISION_CHANGED } from "@/lib/shared/revision-conflict";
import { MOCK_DEFINITION_REVISION } from "@/mock/fixture/flow-definitions";
import {
  deploymentPhases,
  failedBuildPhases,
  mockPhases,
  nowIso,
  randomHex,
  readyBuildPhases,
  type MockPhaseTiming,
} from "./clock";
import {
  mockConfigurationDraft,
  mockConfigurationReady,
  type MockAppManifest,
  type MockNativeConfiguration,
} from "./configuration";
import type { MockSession, MockStore } from "./store";
import { mockCommitVersion } from "./resource-snapshots";

/** Who asked to cancel a build or a deployment, and when. */
export interface MockCancellation {
  actorAccountId: string;
  requestedAt: string;
}

/**
 * A publish that runs on the mock clock (`delivery/publish-runs.ts`): when it began, how long each stage
 * after Check takes, how it ends, and a person's cancellation.
 */
export interface MockPublishRun {
  startedAtMs: number;
  /** Lock settings, Build, Deploy, Go live. */
  stageMs: [number, number, number, number];
  outcome: "ready" | "build-failed" | "unhealthy";
  cancelledAtMs?: number;
  settled: boolean;
}

export interface MockBuildRevision {
  buildRevisionId: string;
  status: "READY" | "FAILED" | "BUILDING" | "CANCELLED";
  revision: string;
  configurationRevision: string;
  draftConfigurationRevision: string;
  imageDigest: string;
  buildProfileVersion: string;
  providerSettled: boolean;
  autoDeploy: boolean;
  autoDeployStatus: "" | "ACCEPTED" | "REJECTED";
  autoDeploymentId?: string;
  autoDeployFailure?: Pick<Failure__Output, "code" | "message">;
  createdAt: string;
  phaseTimings: MockPhaseTiming[];
  frozenConfiguration: MockNativeConfiguration;
  failure:
    | (Pick<Failure__Output, "code" | "message" | "isRecoverable"> & Partial<Pick<Failure__Output, "phase" | "remedy">>)
    | null;
  expectedLiveRevision: string;
  cancellation?: MockCancellation;
  run?: MockPublishRun;
  /** Who started it, as Live's history names them. */
  startedBy?: string;
  /** How the build met the publishing rule, decided at admission. */
  previewValidation: PreviewValidation;
  recoveryRequests?: Set<string>;
}

/** A Release; its ID is its source commit. */
export interface MockRelease {
  id: string;
  projectId: string;
  sourceCommitSha: string;
  state: "DRAFT" | MockBuildRevision["status"];
  revision: string;
  buildProfile: string;
  createdAt: string;
  preparedSource: {
    status: "READY";
    sourceCommit: string;
    definitionRevision: string;
    appManifest: MockAppManifest;
    phaseTimings: MockPhaseTiming[];
    /** The commit's version, frozen at preparation. */
    version: CommitVersion | null;
  };
  activeBuildRevisionId: string;
  readyBuildRevisionId: string;
  buildRevisions: MockBuildRevision[];
  /** The request fingerprint each build identity was admitted with. */
  buildRequests: Map<string, string>;
}

export interface MockDeployment {
  projectId: string;
  id: string;
  releaseId: string;
  buildRevisionId: string;
  configurationRevision: string;
  generation: string;
  revision: string;
  status: "READY" | "REMOVED" | "DEPLOYING" | "FAILED" | "CANCELLING" | "CANCELLED";
  url: string | null;
  phaseTimings: MockPhaseTiming[];
  updatedAt: string;
  failure?: Pick<Failure__Output, "code" | "message" | "isRecoverable" | "phase" | "remedy" | "owner">;
  cancellation?: MockCancellation;
  version?: CommitVersion | null;
  run?: MockPublishRun;
  startedBy?: string;
}

// The fields of the decoded StartDeployment and StartReleaseBuild requests these fixtures read.
type MockDeployRequest = Pick<
  StartDeploymentRequest__Output,
  "releaseId" | "buildRevisionId" | "configurationRevision" | "expectedLiveRevision" | "deploymentId"
>;

type MockBuildRequest = Pick<
  StartReleaseBuildRequest__Output,
  | "requestId"
  | "releaseId"
  | "buildRevisionId"
  | "configurationRevision"
  | "expectedRevision"
  | "autoDeploy"
  | "expectedLiveRevision"
  | "previewSkipReason"
>;

const failure = (message: string) => ({ code: "MOCK_BUILD_FAILED", message, isRecoverable: true });

export function prepareMockRelease(store: MockStore, projectId: string, commit: string): MockRelease {
  if (!store.mainCommits.some((item) => item.projectId === projectId && item.sourceCommitSha === commit))
    throw httpError(404, "Unknown mock main commit");
  const existing = store.releases.get(`${projectId}:${commit}`);
  if (existing) return existing;
  const draft = mockConfigurationDraft(store, projectId, "live", commit);
  const release: MockRelease = {
    id: commit,
    projectId,
    sourceCommitSha: commit,
    state: "DRAFT",
    revision: "1",
    buildProfile: "mock-v2",
    createdAt: nowIso(),
    preparedSource: {
      status: "READY",
      sourceCommit: commit,
      definitionRevision: MOCK_DEFINITION_REVISION,
      appManifest: draft.appManifest,
      version: mockCommitVersion(store, projectId, commit),
      phaseTimings: mockPhases(
        ["QUEUE", 0, 1_800],
        ["HOST_PREPARATION", 1_800, 14_000],
        ["DEPENDENCY_PREPARATION", 15_800, 22_000],
        ["ANALYSIS", 37_800, 5_100],
      ),
    },
    activeBuildRevisionId: "",
    readyBuildRevisionId: "",
    buildRevisions: [],
    buildRequests: new Map(),
  };
  store.releases.set(`${projectId}:${commit}`, release);
  return release;
}

/**
 * A deployment of a READY build; it serves Live at once, or, unsettled, starts DEPLOYING for
 * `delivery/publish-runs.ts` to take through its phases.
 */
export function deployMockLive(
  store: MockStore,
  projectId: string,
  body: MockDeployRequest,
  settle = true,
): MockDeployment {
  const release = store.releases.get(`${projectId}:${body.releaseId}`);
  const build = release?.buildRevisions.find((item) => item.buildRevisionId === body.buildRevisionId);
  if (!release || !build || build.status !== "READY" || body.configurationRevision !== build.configurationRevision)
    throw httpError(409, "Select a successful immutable fixture build");
  const id = body.deploymentId;
  if (!id) throw httpError(400, "Deployment identity is required");
  const existing = store.deployments.get(`${projectId}:${id}`);
  if (existing) {
    if (existing.releaseId !== body.releaseId || existing.buildRevisionId !== body.buildRevisionId)
      throw httpError(409, "Deployment identity conflicts");
    return existing;
  }
  const current = store.liveDeployments.get(projectId);
  if (body.expectedLiveRevision !== (current?.generation ?? "0"))
    throw rpcFailure(409, "Live generation changed", REVISION_CHANGED);
  const deployment: MockDeployment = {
    projectId,
    id,
    releaseId: release.id,
    buildRevisionId: build.buildRevisionId,
    configurationRevision: build.configurationRevision,
    generation: String(Number(current?.generation ?? 0) + 1),
    revision: "1",
    status: settle ? "READY" : "DEPLOYING",
    url: settle ? `${store.origin}/api/mock-app?env=live&project=${projectId}` : null,
    phaseTimings: settle ? deploymentPhases() : [],
    updatedAt: nowIso(),
    version: mockCommitVersion(store, projectId, release.id),
  };
  if (settle) store.liveDeployments.set(projectId, deployment);
  store.deployments.set(`${projectId}:${id}`, deployment);
  return deployment;
}

/** Stops Live: its deployment is kept as REMOVED. */
export function removeMockLive(store: MockStore, projectId: string) {
  const live = store.liveDeployments.get(projectId);
  if (live) store.deployments.set(`${projectId}:${live.id}`, { ...live, status: "REMOVED", url: null });
  store.liveDeployments.delete(projectId);
}

/**
 * Admits a build of the Release's frozen copy. It settles at once, deploying automatically, or, unsettled,
 * starts BUILDING for `delivery/publish-runs.ts` to run; a replayed request answers null.
 */
export function buildMockRelease(
  store: MockStore,
  projectId: string,
  body: MockBuildRequest,
  settle = true,
): MockBuildRevision | null {
  const release = store.releases.get(`${projectId}:${body.releaseId}`);
  if (!release) throw httpError(409, "Prepare this mock commit first");
  if (!/^build-[a-f0-9-]{36}$/.test(body.buildRevisionId) || !body.requestId)
    throw httpError(400, "Build and request identities are required");
  const fingerprint = JSON.stringify([
    body.requestId,
    body.releaseId,
    body.buildRevisionId,
    body.configurationRevision,
    body.expectedRevision,
    body.autoDeploy,
    body.expectedLiveRevision,
    body.previewSkipReason,
  ]);
  const prior = release.buildRequests.get(body.buildRevisionId);
  if (prior) {
    if (prior !== fingerprint) throw httpError(409, "Build identity conflicts");
    return null;
  }
  const draft = mockConfigurationDraft(store, projectId, "live", release.id);
  if (
    !mockConfigurationReady(draft) ||
    body.configurationRevision !== draft.revision ||
    body.expectedRevision !== release.revision
  )
    throw httpError(409, "Complete the current mock draft and refresh the release revision");
  const previewValidation = admittedValidation(
    release.id,
    body.previewSkipReason,
    store.liveDeployments.get(projectId)?.releaseId ?? null,
    mockConfigurationDraft(store, projectId, "preview").setup.starts,
  );
  const frozen = structuredClone(draft.native);
  const failed = settle && frozen.environment.MOCK_BUILD_OUTCOME === "failed";
  const build: MockBuildRevision = {
    buildRevisionId: body.buildRevisionId,
    status: !settle ? "BUILDING" : failed ? "FAILED" : "READY",
    revision: "1",
    configurationRevision: draft.revision,
    draftConfigurationRevision: draft.revision,
    imageDigest: `sha256:${createHash("sha256").update(`${projectId}:${body.buildRevisionId}`).digest("hex")}`,
    buildProfileVersion: "mock-v2",
    providerSettled: true,
    autoDeploy: body.autoDeploy,
    autoDeployStatus: "",
    createdAt: nowIso(),
    phaseTimings: !settle ? [] : failed ? failedBuildPhases() : readyBuildPhases(),
    frozenConfiguration: frozen,
    failure: failed
      ? failure("Explicit MOCK_BUILD_OUTCOME=failed fixture. Choose ready in Setup · Live and publish again.")
      : null,
    expectedLiveRevision: body.expectedLiveRevision,
    previewValidation,
  };
  release.buildRequests.set(build.buildRevisionId, fingerprint);
  release.buildRevisions.push(build);
  release.activeBuildRevisionId = build.buildRevisionId;
  release.state = build.status;
  release.revision = String(Number(release.revision) + 1);
  if (settle && !failed) {
    release.readyBuildRevisionId = build.buildRevisionId;
    finishAutoDeploy(store, projectId, release, build);
  }
  return build;
}

function finishAutoDeploy(store: MockStore, projectId: string, release: MockRelease, build: MockBuildRevision) {
  if (!build.autoDeploy) return;
  try {
    build.autoDeploymentId = deployMockLive(store, projectId, {
      releaseId: release.id,
      buildRevisionId: build.buildRevisionId,
      configurationRevision: build.configurationRevision,
      expectedLiveRevision: build.expectedLiveRevision,
      deploymentId: `mock-auto-${build.buildRevisionId}`,
    }).id;
    build.autoDeployStatus = "ACCEPTED";
  } catch (error) {
    build.autoDeployStatus = "REJECTED";
    build.autoDeployFailure = { code: "MOCK_LIVE_CHANGED", message: (error as Error).message };
  }
}

export function recoverMockBuild(
  store: MockStore,
  projectId: string,
  releaseId: string,
  buildRevisionId: string,
  expectedRevision: string,
  requestId: string | null,
): void {
  const release = store.releases.get(`${projectId}:${releaseId}`);
  const build = release?.buildRevisions.find((item) => item.buildRevisionId === buildRevisionId);
  if (!release || !build) throw httpError(404, "Mock build not found");
  build.recoveryRequests ??= new Set();
  const identity = `${requestId}:${expectedRevision}`;
  if (build.recoveryRequests.has(identity)) return;
  if (
    !requestId ||
    release.activeBuildRevisionId !== buildRevisionId ||
    build.status !== "FAILED" ||
    expectedRevision !== build.revision
  )
    throw httpError(409, "Mock build is not recoverable at this revision");
  build.recoveryRequests.add(identity);
  build.status = "READY";
  build.failure = null;
  build.phaseTimings = readyBuildPhases();
  build.revision = String(Number(build.revision) + 1);
  release.readyBuildRevisionId = buildRevisionId;
  release.state = "READY";
  release.revision = String(Number(release.revision) + 1);
  finishAutoDeploy(store, projectId, release, build);
}

/** An ordinary turn pushes one commit to main, which Publishing then lists. */
export function recordAgentCommit(store: MockStore, session: MockSession, title: string) {
  const committedAt = nowIso();
  const sourceCommitSha = randomHex(40);
  session.headCommitSha = sourceCommitSha;
  session.revision += 1;
  session.updatedAt = committedAt;
  store.mainCommits.unshift({
    projectId: session.projectId,
    sourceCommitSha,
    title: title.trim().split("\n", 1)[0] || "Update project",
    committedAt,
  });
}
