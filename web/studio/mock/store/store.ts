// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Explicit in-memory development resources; never a production or acceptance backend.
import type { ConfigurationRequirementGap__Output } from "@/lib/generated/protobuf/superverse/v2/ConfigurationRequirementGap";
import type { PreparedPreviewSourceSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/PreparedPreviewSourceSnapshot";
import { httpError } from "@/lib/server/request-body";
import { requirementGapName } from "@/lib/shared/problem/requirement-gaps";
import { createAgent } from "./agent-scripts";
import { appendMessage, mockNotification, type MockAgent } from "./agent-state";
import { nowIso, randomHex, type MockPhaseTiming } from "./clock";
import { MOCK_DEFINITION_REVISION } from "@/mock/fixture/flow-definitions";
import {
  invalidateConfigurationDraft,
  mockConfigurationDraft,
  mockStartGaps,
  recordPushedStart,
  type MockConfigurationDraft,
} from "./configuration";
import type { MockDeployment, MockRelease } from "./publishing";
import { listReleaseCandidates } from "./resource-snapshots";
import { createSeedConversation } from "./seed-conversation";
import {
  createFailedSeedProject,
  createSeedProject,
  createSeedSession,
  createOtherSeedProject,
  holdWorkspace,
  mockMessage,
  previewStartPhases,
  SEED_HEAD_COMMIT,
} from "./seed";

interface MockRepository {
  githubRepositoryId: number;
  owner: string;
  name: string;
  defaultBranch: string;
  state: "ACTIVE" | "FAILED" | "ARCHIVED";
}

export interface MockProject {
  id: string;
  revision: number;
  displayName: string;
  state: "ACTIVE" | "SETUP_FAILED" | "DELETED";
  repository: MockRepository;
  createdAt: string;
  updatedAt: string;
  /** An explicitly requested, finite development-only configuration read outage. */
  configurationReadFailures?: number;
}

export interface MockPreparedPreviewSource extends Pick<
  PreparedPreviewSourceSnapshot__Output,
  "status" | "requestId" | "workspaceRequestId" | "generation" | "definitionRevision"
> {
  sourceCommit: string | null;
}

/** The newest automatic deployment of a pushed commit; the mock never queues one. */
interface MockPreviewDeployment {
  commit: string;
  status: "READY" | "INCOMPATIBLE";
  updatedAt: string;
}

// Statuses are the persisted values; snapshots add the protobuf enum prefixes. A Project has one
// Session, which the Project addresses.
export interface MockSession {
  revision: number;
  projectId: string;
  headCommitSha: string | null;
  /** The latest Sandbox instance: READY until its deadline, expiresAt; CLOSED once reclaimed, files kept. */
  workspace: {
    workspaceRequestId: string;
    state: "READY" | "CLOSED";
    generation: number;
    expiresAt: string | null;
    /** The phases of the instance's start; a paused instance keeps them until the next one replaces them. */
    phaseTimings: MockPhaseTiming[];
  };
  state: "ACTIVE" | "ARCHIVED";
  preview: {
    state: "STOPPED" | "READY";
    workspaceRequestId: string;
    generation: number;
    revision: number;
    preparedSource: MockPreparedPreviewSource | null;
    sourceCommit: string;
    definitionRevision: string;
    servingRevision: number;
    url: string | null;
    automaticDeployment: MockPreviewDeployment | null;
    /** What the configuration lacks for the commit the last refused deployment prepared. */
    configurationGaps: ConfigurationRequirementGap__Output[];
    /** The phases of the current or last start. */
    phaseTimings: MockPhaseTiming[];
  };
  createdAt: string;
  updatedAt: string;
}

export interface MockMainCommit {
  projectId: string;
  sourceCommitSha: string;
  title: string;
  committedAt: string;
}

export interface MockStore {
  origin: string;
  projects: Map<string, MockProject>;
  // The Session of each Project and its Agent, by Project ID.
  sessions: Map<string, MockSession>;
  agents: Map<string, MockAgent>;
  // Keyed `${projectId}:${releaseId}`; a Release ID is its source commit.
  releases: Map<string, MockRelease>;
  // Keyed `${projectId}:${scope}:${releaseId}`; the Preview has no Release ID.
  configurations: Map<string, MockConfigurationDraft>;
  // The deployment each Project serves Live, by Project ID.
  liveDeployments: Map<string, MockDeployment>;
  // Keyed `${projectId}:${deploymentId}`.
  deployments: Map<string, MockDeployment>;
  // Admitted Project creations by request ID.
  projectAdmissions: Map<string, string>;
  // Newest first.
  mainCommits: MockMainCommit[];
}

function createStore(origin: string): MockStore {
  const project = createSeedProject();
  const failedProject = createFailedSeedProject();
  const otherProject = createOtherSeedProject();
  // The seed Project's Preview served its head before it stopped, so its processes are its own.
  const session = createSeedSession(project.id);
  session.preview.sourceCommit = SEED_HEAD_COMMIT;
  const agent = createSeedConversation(nowIso(-90_000));
  return {
    origin,
    projects: new Map([
      [project.id, project],
      [failedProject.id, failedProject],
      [otherProject.id, otherProject],
    ]),
    sessions: new Map([[project.id, session]]),
    agents: new Map([[project.id, agent]]),
    releases: new Map(),
    configurations: new Map(),
    liveDeployments: new Map(),
    deployments: new Map(),
    projectAdmissions: new Map(),
    mainCommits: [
      {
        projectId: project.id,
        sourceCommitSha: SEED_HEAD_COMMIT,
        title: "Add the text summary flow",
        committedAt: session.updatedAt,
      },
      {
        projectId: project.id,
        sourceCommitSha: "dddddddddddddddddddddddddddddddddddddddd",
        title: "Shorten the summary instructions",
        committedAt: nowIso(-30_000),
      },
    ],
  };
}

// One store per server process: Next bundles pages and route handlers separately, each with its own copy
// of this module, so the store lives on the process's global object.
const holder = globalThis as typeof globalThis & { superverseStudioMockStore?: MockStore };

/** The store, whose Preview and Live URLs point at the Studio origin the browser uses. */
export function getStudioMockStore(origin?: string): MockStore {
  const store = (holder.superverseStudioMockStore ??= createStore(origin ?? ""));
  if (origin) store.origin = origin;
  return store;
}

export function listProjects(store: MockStore): MockProject[] {
  return [...store.projects.values()].filter((project) => project.state !== "DELETED");
}

/**
 * A Project on a new private managed repository. Mock Setup completes at once, so the Project is
 * ready and has its one Session, with an empty conversation, as ProjectFlow starts it.
 */
export function createProject(
  store: MockStore,
  name: string,
  idempotencyKey: string | null,
): { project: MockProject; replayed: boolean } {
  const admittedProjectId = idempotencyKey ? store.projectAdmissions.get(idempotencyKey) : null;
  if (admittedProjectId) {
    return { project: store.projects.get(admittedProjectId)!, replayed: true };
  }
  const id = randomHex(4);
  if (store.projects.has(id)) throw httpError(409, "Project ID already exists");
  const timestamp = nowIso();
  const project: MockProject = {
    ...createSeedProject(),
    id,
    displayName: name.trim() || "Untitled app",
    createdAt: timestamp,
    updatedAt: timestamp,
    repository: {
      ...createSeedProject().repository,
      githubRepositoryId: Number.parseInt(id, 16),
      name: `mock-${id}`,
    },
  };
  store.projects.set(id, project);
  if (idempotencyKey) store.projectAdmissions.set(idempotencyKey, id);
  store.mainCommits.unshift({
    projectId: id,
    sourceCommitSha: randomHex(40),
    title: "Initialize project",
    committedAt: timestamp,
  });
  const session: MockSession = {
    ...createSeedSession(id),
    headCommitSha: listReleaseCandidates(store, id)[0]?.sourceCommitSha ?? null,
  };
  store.sessions.set(id, session);
  store.agents.set(id, createAgent());
  return { project, replayed: false };
}

export function getProject(store: MockStore, projectId: string): MockProject | null {
  return store.projects.get(projectId) ?? null;
}

export function deleteProject(store: MockStore, projectId: string): MockProject | null {
  const project = store.projects.get(projectId);
  if (!project) return null;
  project.state = "DELETED";
  project.revision += 1;
  project.repository.state = "ARCHIVED";
  project.updatedAt = nowIso();
  delete project.configurationReadFailures;
  // The deletion completes the Project's Session, as its cleanup does.
  const session = store.sessions.get(projectId);
  if (session) {
    session.state = "ARCHIVED";
    session.revision += 1;
  }
  return project;
}

/** The Project's Session; a Project whose Setup failed has none. */
export function getSession(store: MockStore, projectId: string): MockSession | null {
  return store.sessions.get(projectId) ?? null;
}

const KEEP_RUNNING_MS = 7 * 24 * 60 * 60 * 1_000;

/**
 * "Keep running for 7 days": like Go, the READY workspace of an active Session stays until at least
 * seven days from now; anything else keeps its deadline and answers false.
 */
export function keepWorkspaceRunning(store: MockStore, projectId: string): boolean {
  const session = store.sessions.get(projectId);
  if (!session || session.state !== "ACTIVE" || session.workspace.state !== "READY") return false;
  const deadline = nowIso(KEEP_RUNNING_MS);
  if (!session.workspace.expiresAt || deadline > session.workspace.expiresAt) {
    session.workspace.expiresAt = deadline;
    session.revision += 1;
    session.updatedAt = nowIso();
  }
  return true;
}

export function preparePreviewSource(
  store: MockStore,
  projectId: string,
  requestId: string,
): MockPreparedPreviewSource | null {
  const session = store.sessions.get(projectId);
  if (!session || session.state !== "ACTIVE") return null;
  // Preparing source needs the workspace, which a paused Session starts again.
  holdWorkspace(session);
  const workspaceRequestId = session.workspace.workspaceRequestId;
  session.preview.workspaceRequestId = workspaceRequestId;
  session.preview.generation = session.workspace.generation;
  session.preview.revision += 1;
  session.preview.preparedSource = {
    status: "PREVIEW_SOURCE_STATUS_READY",
    requestId,
    workspaceRequestId,
    generation: String(session.workspace.generation),
    sourceCommit: session.headCommitSha,
    definitionRevision: MOCK_DEFINITION_REVISION,
  };
  const draft = mockConfigurationDraft(store, projectId, "preview");
  if (draft.sourceCommit !== session.headCommitSha) {
    draft.sourceCommit = session.headCommitSha;
    draft.appManifest.sourceCommit = session.headCommitSha;
    invalidateConfigurationDraft(draft);
  }
  session.updatedAt = nowIso();
  session.revision += 1;
  return session.preview.preparedSource;
}

export function startPreview(store: MockStore, projectId: string): MockSession["preview"] | null {
  const session = store.sessions.get(projectId);
  if (!session) return null;
  session.preview = {
    ...session.preview,
    state: "READY",
    sourceCommit: session.preview.preparedSource?.sourceCommit ?? "",
    definitionRevision: session.preview.preparedSource?.definitionRevision ?? "",
    servingRevision: session.preview.servingRevision + 1,
    revision: session.preview.revision + 1,
    url: `${store.origin}/api/mock-app?env=preview&project=${projectId}`,
    phaseTimings: previewStartPhases(),
  };
  session.updatedAt = nowIso();
  return session.preview;
}

/**
 * With "Update the Preview after every push" on, the push builds its processes, whatever the
 * configuration, as Go does, and the Preview deploys the commit the Agent pushed, as the turn end hands
 * it over. The applied configuration, or the draft on a first start, serves it at once; otherwise the
 * configuration lacks the fixture requirement, which a configuration notice tells unless the last
 * deployment was refused for the same gaps, and the Preview keeps what it served.
 */
export function deployPushedCommit(store: MockStore, session: MockSession, agent: MockAgent) {
  const commit = session.headCommitSha;
  if (!agent.description.isPreviewAutoDeployEnabled || !commit) return;
  const draft = mockConfigurationDraft(store, session.projectId, "preview");
  const gaps = mockStartGaps(draft, false);
  const repeated =
    session.preview.automaticDeployment?.status === "INCOMPATIBLE" &&
    JSON.stringify(session.preview.configurationGaps) === JSON.stringify(gaps);
  session.preview.automaticDeployment = { commit, status: gaps.length ? "INCOMPATIBLE" : "READY", updatedAt: nowIso() };
  session.preview.configurationGaps = gaps;
  session.preview.preparedSource = {
    status: "PREVIEW_SOURCE_STATUS_READY",
    requestId: `auto-deploy-${randomHex(6)}`,
    workspaceRequestId: session.workspace.workspaceRequestId,
    generation: String(session.workspace.generation),
    sourceCommit: commit,
    definitionRevision: MOCK_DEFINITION_REVISION,
  };
  draft.sourceCommit = commit;
  draft.appManifest.sourceCommit = commit;
  if (gaps.length === 0) {
    recordPushedStart(draft, commit, true);
    startPreview(store, session.projectId);
    return;
  }
  recordPushedStart(draft, commit, false);
  if (repeated) return;
  const names = gaps.map(requirementGapName);
  const summary = `The Preview of ${commit.slice(0, 12)} needs configuration: ${names.join(", ")}.`;
  const notification = mockNotification({
    id: `preview/${session.workspace.workspaceRequestId}/deploy-preview-${agent.nextSequence - 1}/notice`,
    kind: "KIND_DELIVERY",
    source: "SOURCE_PREVIEW",
    summary,
    outcome: "OUTCOME_NOTICE",
    owner: "OWNER_CONFIGURATION",
    code: "PREVIEW_CONFIGURATION_INCOMPATIBLE",
    phase: "deploy-preview",
    commit,
    resourceId: session.workspace.workspaceRequestId,
    gaps: names,
    gapCount: names.length,
  });
  appendMessage(agent, mockMessage("ROLE_NOTIFICATION", summary, { notification }));
}

export function stopPreview(store: MockStore, projectId: string): MockSession["preview"] | null {
  const session = store.sessions.get(projectId);
  if (!session) return null;
  session.preview = { ...session.preview, state: "STOPPED", url: null };
  session.preview.revision += 1;
  session.updatedAt = nowIso();
  return session.preview;
}

export function getRelease(store: MockStore, projectId: string, releaseId: string): MockRelease | null {
  return store.releases.get(`${projectId}:${releaseId}`) ?? null;
}
