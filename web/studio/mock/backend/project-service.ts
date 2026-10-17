// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock ProjectService over the in-memory store; development only.
import type { AccountSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/AccountSnapshot";
import type { ProjectConfigurationTarget__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationTarget";
import { rpcFailure } from "@/lib/server/grpc-client";
import { httpError } from "@/lib/server/request-body";
import { REVISION_CHANGED } from "@/lib/shared/revision-conflict";
import { MOCK_EXAMPLES } from "@/mock/fixture/examples";
import { mockAccount } from "@/mock/store/accounts";
import { startFromMockExample } from "@/mock/store/agent-scripts";
import {
  mockConfigurationDraft,
  mockConfigurationReady,
  mockConfigurationSnapshot,
  mockNextPublishValues,
} from "@/mock/store/configuration";
import { removeMockLive } from "@/mock/store/publishing";
import { mockRecentTriggerEvents } from "@/mock/store/setup/recent-events";
import { mockProjectSnapshot, withStatuses } from "@/mock/store/resource-snapshots";
import {
  createProject,
  deleteProject,
  getProject,
  getRelease,
  getSession,
  listProjects,
  type MockStore,
} from "@/mock/store/store";
import {
  admitMockProject,
  mockTeamSpaceProjects,
  registerMockProject,
  requireMockProjectDeletion,
  requireMockProjectRole,
} from "@/mock/store/tenancy/project-members";
import { mockEnvironmentEvents } from "@/mock/store/delivery/environment-events";
import { advanceMockPublishes } from "@/mock/store/delivery/publish-runs";
import { readableTeamSpace, mockTenancy } from "@/mock/store/tenancy/team-spaces";
import { withCallerRole } from "@/mock/store/tenancy/tenancy-snapshots";
import { startMockSession } from "./agent-service";
import type { MockService } from "./mock-backend";
import { watchSnapshots } from "./store-watch";

function project(store: MockStore, projectId: string) {
  const snapshot = mockProjectSnapshot(store, projectId);
  if (!snapshot) throw httpError(404, "Project not found");
  return snapshot;
}

// Like Go, a Project is read with the caller's role in it, which reading needs.
function viewed(store: MockStore, account: AccountSnapshot__Output, projectId: string) {
  const tenancy = mockTenancy(store, account);
  requireMockProjectRole(tenancy, account, projectId);
  return withCallerRole(project(store, projectId), tenancy, account);
}

// Like Go, a Preview configuration is the one of the source prepared for the Session's HEAD, which
// no read prepares; a Live one belongs to a prepared Release, and Live's next-publish layer is the
// Project's, checked against a Release or, without one, the Preview's source.
function configuration(store: MockStore, target: ProjectConfigurationTarget__Output | null) {
  const preview = target?.scope === "PROJECT_SCOPE_PREVIEW";
  if (!target || (!preview && target.scope !== "PROJECT_SCOPE_LIVE"))
    throw httpError(400, "Configuration scope is required");
  const { projectId, releaseId, nextPublish } = target;
  const session = getSession(store, projectId);
  const named = releaseId ? getRelease(store, projectId, releaseId) : nextPublish && session;
  if (!getProject(store, projectId) || (preview ? !session : !named))
    throw httpError(404, "Mock configuration source not found");
  if (!preview) return mockConfigurationDraft(store, projectId, "live", nextPublish ? "" : releaseId);
  const prepared = session?.preview.preparedSource;
  if (prepared?.status !== "PREVIEW_SOURCE_STATUS_READY" || prepared.sourceCommit !== session?.headCommitSha)
    throw httpError(409, "Prepare the current source");
  return mockConfigurationDraft(store, projectId, "preview");
}

export const projectService: MockService<"ProjectService"> = {
  // Like Go, the first message comes with its message ID, and the new Session starts with it and the model.
  CreateProject: (
    { requestId, name, managedRepository, teamSpaceId, initialMessage, messageId, model, caller },
    { store },
  ) => {
    if (!managedRepository) throw httpError(400, "managed_repository is required");
    if (Boolean(initialMessage.trim()) !== Boolean(messageId))
      throw httpError(400, "initial_message and message_id are set together");
    const example = managedRepository.example;
    const offered = example && MOCK_EXAMPLES.find((item) => item.exampleId === example.exampleId);
    if (example && !offered)
      throw rpcFailure(409, "This example is not offered; pick another one", {
        reason: "EXAMPLE_NOT_OFFERED",
        domain: "superverse.v2",
        metadata: { remedy: "FAILURE_REMEDY_APPLY_AGAIN" },
      });
    const account = mockAccount(caller);
    const tenancy = mockTenancy(store, account);
    admitMockProject(tenancy, account, teamSpaceId);
    const started = offered ? { exampleId: offered.exampleId, tree: offered.tree } : undefined;
    const { project: created, replayed } = createProject(store, name, requestId || null, started);
    registerMockProject(tenancy, account, teamSpaceId, created.id);
    if (!replayed) startMockSession(store, created.id, model, initialMessage);
    if (!replayed && offered) startFromMockExample(store, created.id, offered.title, offered.card);
    return { projectId: created.id };
  },
  // Every Project of the Teamspace, with the caller's role in each.
  ListProjects: ({ statuses, teamSpaceId, caller }, { store }) => {
    if (!teamSpaceId) throw httpError(400, "team_space_id is required");
    const account = mockAccount(caller);
    const tenancy = mockTenancy(store, account);
    readableTeamSpace(tenancy, account, teamSpaceId);
    return {
      projects: withStatuses(
        mockTeamSpaceProjects(tenancy, teamSpaceId, listProjects(store)).map((item) =>
          withCallerRole(project(store, item.id), tenancy, account),
        ),
        statuses,
      ),
      nextPageToken: "",
    };
  },
  GetProject: ({ projectId, caller }, { store }) => ({ project: viewed(store, mockAccount(caller), projectId) }),
  DeleteProject: ({ projectId, expectedRevision, caller }, { store }) => {
    const account = mockAccount(caller);
    requireMockProjectDeletion(mockTenancy(store, account), account, projectId);
    if (String(project(store, projectId).revision) !== expectedRevision)
      throw rpcFailure(409, "Project changed", REVISION_CHANGED);
    deleteProject(store, projectId);
    return { project: viewed(store, account, projectId) };
  },
  // A DELETED snapshot is the Project Watch's last frame.
  WatchProject: ({ projectId, caller }, { store }, signal) => {
    const account = mockAccount(caller);
    const tenancy = mockTenancy(store, account);
    requireMockProjectRole(tenancy, account, projectId);
    return watchSnapshots(
      signal,
      () => {
        advanceMockPublishes(store);
        const snapshot = mockProjectSnapshot(store, projectId);
        return snapshot && withCallerRole(snapshot, tenancy, account);
      },
      (snapshot) => snapshot.status === "PROJECT_STATUS_DELETED",
    );
  },
  GetProjectConfiguration: ({ target }, { store }) => {
    const draft = configuration(store, target);
    const source = getProject(store, draft.projectId);
    if (source && (source.configurationReadFailures ?? 0) > 0) {
      source.configurationReadFailures! -= 1;
      throw httpError(503, "Superverse service is unavailable");
    }
    const nextPublish = Boolean(target?.nextPublish);
    return {
      configuration: mockConfigurationSnapshot(store, draft, nextPublish, nextPublish ? target?.releaseId : ""),
    };
  },
  // The mock timelines are typed fixtures over the Project's commits, one page.
  ListEnvironmentEvents: ({ projectId, scope }, { store }) => ({
    events: mockEnvironmentEvents(store, projectId, scope === "PROJECT_SCOPE_LIVE"),
    nextPageToken: "",
  }),
  ListRecentTriggerEvents: () => mockRecentTriggerEvents(Date.now()),
  ValidateProjectConfiguration: ({ target, requestId, expectedRevision }, { store }) => {
    const draft = configuration(store, target);
    const nextPublish = Boolean(target?.nextPublish);
    if (expectedRevision !== draft.revision) throw httpError(409, "Mock configuration revision changed");
    const source = nextPublish ? (target?.releaseId ?? "") : "";
    if (
      !mockConfigurationReady(nextPublish ? { native: mockNextPublishValues(store, draft.projectId, source) } : draft)
    )
      throw httpError(409, "Review the current source configuration and credentials before deployment.");
    draft.validation = { requestId, configurationRevision: draft.revision };
    return { configuration: mockConfigurationSnapshot(store, draft, nextPublish, source) };
  },
  // Removing Live is fenced on the Live pointer the page observed.
  RemoveLive: ({ projectId, deploymentId, expectedLiveRevision }, { store }) => {
    project(store, projectId);
    const live = store.liveDeployments.get(projectId);
    if (live) {
      if (live.id !== deploymentId || live.generation !== expectedLiveRevision)
        throw rpcFailure(409, "Live deployment changed", REVISION_CHANGED);
      removeMockLive(store, projectId);
    }
    return { project: project(store, projectId) };
  },
};
