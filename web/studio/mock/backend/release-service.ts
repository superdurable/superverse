// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock ReleaseService over the in-memory publishing fixtures; development only.
import { rpcFailure } from "@/lib/server/grpc-client";
import { httpError } from "@/lib/server/request-body";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import { REVISION_CHANGED } from "@/lib/shared/revision-conflict";
import {
  invalidateConfigurationDraft,
  mockConfigurationDraft,
  mockConfigurationSnapshot,
  mockCopyBasis,
  mockNextPublish,
  mockNextPublishValues,
} from "@/mock/store/configuration";
import { mockAccount } from "@/mock/store/accounts";
import { advanceMockPublishes, cancelMockBuild, mockBuildLogPage, mockRun } from "@/mock/store/delivery/publish-runs";
import { buildMockRelease, prepareMockRelease, recoverMockBuild } from "@/mock/store/publishing";
import { mockFlowDefinitions, mockReleaseSnapshot, withStatuses } from "@/mock/store/resource-snapshots";
import { getRelease, type MockStore } from "@/mock/store/store";
import { listReleaseCandidates, mockCommitVersion } from "@/mock/store/resource-snapshots";
import type { MockService } from "./mock-backend";
import { watchSnapshots } from "./store-watch";

function releaseBuild(store: MockStore, projectId: string, releaseId: string, buildRevisionId: string) {
  const build = getRelease(store, projectId, releaseId)?.buildRevisions.find(
    (item) => item.buildRevisionId === buildRevisionId,
  );
  if (!build) throw httpError(404, "Mock build not found");
  return build;
}

export const releaseService: MockService<"ReleaseService"> = {
  ListReleaseCandidates: ({ projectId }, { store }) => ({
    candidates: listReleaseCandidates(store, projectId).map((item) => ({
      sourceCommit: item.sourceCommitSha,
      subject: item.title,
      committedAt: isoToTimestamp(item.committedAt),
      version: mockCommitVersion(store, projectId, item.sourceCommitSha),
    })),
  }),
  PrepareRelease: ({ projectId, sourceCommit }, { store }) => ({
    releaseId: prepareMockRelease(store, projectId, sourceCommit).id,
  }),
  // A build Studio starts runs on the mock clock through its stages; the frozen copy's MOCK_BUILD_OUTCOME
  // decides whether it fails to build, fails its health check or goes Live.
  StartReleaseBuild: (request, { store }) => {
    const build = buildMockRelease(store, request.projectId, request, false);
    if (build) {
      build.run = mockRun(build.frozenConfiguration.environment.MOCK_BUILD_OUTCOME);
      build.startedBy = mockAccount(request.caller).email;
    }
    return { buildRevisionId: request.buildRevisionId };
  },
  RecoverReleaseBuild: ({ projectId, requestId, releaseId, buildRevisionId, expectedRevision }, { store }) => {
    recoverMockBuild(store, projectId, releaseId, buildRevisionId, expectedRevision, requestId || null);
    return {};
  },
  CancelReleaseBuild: ({ projectId, requestId, releaseId, buildRevisionId, caller }, { store }) => {
    const release = getRelease(store, projectId, releaseId);
    if (!release) throw httpError(404, "Mock Release not found");
    if (!requestId) throw httpError(400, "Request identity is required");
    cancelMockBuild(store, release, buildRevisionId, mockAccount(caller).accountId);
    return {};
  },
  // A running mock build's log grows with the clock; a settled one's is whole.
  GetReleaseBuildLog: ({ projectId, releaseId, buildRevisionId, cursor }, { store }) =>
    mockBuildLogPage(releaseBuild(store, projectId, releaseId, buildRevisionId), cursor),
  ListReleases: ({ projectId, statuses }, { store }) => ({
    releases: withStatuses(
      [...store.releases.values()].filter((item) => item.projectId === projectId).map(mockReleaseSnapshot),
      statuses,
    ),
    nextPageToken: "",
  }),
  // Builds the version's frozen copy from the layer at the revisions the page showed.
  DeriveLiveConfiguration: (
    { projectId, releaseId, expectedRevision, expectedPreviewConfigurationRevision },
    { store },
  ) => {
    const shown = mockConfigurationSnapshot(store, mockNextPublish(store, projectId), true, releaseId);
    if (
      expectedRevision !== shown.nextPublishRevision ||
      expectedPreviewConfigurationRevision !== shown.previewConfigurationRevision
    )
      throw rpcFailure(409, "Live's next publish changed", REVISION_CHANGED);
    const copy = mockConfigurationDraft(store, projectId, "live", releaseId);
    copy.native = structuredClone(mockNextPublishValues(store, projectId, releaseId));
    invalidateConfigurationDraft(copy);
    copy.live.copiedFrom = mockCopyBasis(store, projectId);
    return { configuration: mockConfigurationSnapshot(store, copy) };
  },
  // The Flow Definitions of a prepared source; none before preparation or on failure.
  GetRelease: async ({ projectId, releaseId }, { store }) => {
    const release = getRelease(store, projectId, releaseId);
    if (!release) throw httpError(404, "Release not found");
    return {
      release: mockReleaseSnapshot(release),
      flowDefinitions:
        release.preparedSource.status === "READY" && release.state !== "FAILED" ? await mockFlowDefinitions() : [],
    };
  },
  WatchRelease: ({ projectId, releaseId }, { store }, signal) =>
    watchSnapshots(signal, () => {
      advanceMockPublishes(store);
      const release = getRelease(store, projectId, releaseId);
      return release && mockReleaseSnapshot(release);
    }),
};
