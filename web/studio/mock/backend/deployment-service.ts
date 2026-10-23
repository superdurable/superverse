// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock DeploymentService over the in-memory publishing fixtures, and the mock EvolveService that measures
// what is Live against example goals and keeps the agent's proposal; development only.
import { httpError } from "@/lib/server/request-body";
import { mockAccount } from "@/mock/store/accounts";
import { decideMockProposal, getMockProposal } from "@/mock/store/delivery/evolve-proposals";
import { mockEvolve, startMockReview } from "@/mock/store/delivery/goal-measurement";
import { advanceMockPublishes, cancelMockDeployment, mockRun } from "@/mock/store/delivery/publish-runs";
import { deployMockLive } from "@/mock/store/publishing";
import { mockDeploymentSnapshot, withStatuses } from "@/mock/store/resource-snapshots";
import type { MockStore } from "@/mock/store/store";
import type { MockService } from "./mock-backend";
import { watchSnapshots } from "./store-watch";

function deployment(store: MockStore, projectId: string, deploymentId: string) {
  return store.deployments.get(`${projectId}:${deploymentId}`) ?? null;
}

export const deploymentService: MockService<"DeploymentService"> = {
  // A rollback runs on the mock clock through Deploy and Go live.
  StartDeployment: (request, { store }) => {
    const deployment = deployMockLive(store, request.projectId, request, false);
    deployment.run ??= mockRun(undefined, [0, 0, 9_000, 4_500]);
    deployment.startedBy ??= mockAccount(request.caller).email;
    return { deploymentId: deployment.id };
  },
  GetDeployment: ({ projectId, deploymentId }, { store }) => {
    const current = deployment(store, projectId, deploymentId);
    if (!current) throw httpError(404, "Deployment not found");
    return { deployment: mockDeploymentSnapshot(current) };
  },
  ListDeployments: ({ projectId, statuses }, { store }) => ({
    deployments: withStatuses(
      [...store.deployments.values()].filter((item) => item.projectId === projectId).map(mockDeploymentSnapshot),
      statuses,
    ),
    nextPageToken: "",
  }),
  CancelDeployment: ({ projectId, deploymentId, requestId, caller }, { store }) => {
    const current = deployment(store, projectId, deploymentId);
    if (!current) throw httpError(404, "Deployment not found");
    if (!requestId) throw httpError(400, "Request identity is required");
    cancelMockDeployment(store, current, mockAccount(caller).accountId);
    return {};
  },
  WatchDeployment: ({ projectId, deploymentId }, { store }, signal) =>
    watchSnapshots(signal, () => {
      advanceMockPublishes(store);
      const current = deployment(store, projectId, deploymentId);
      return current && mockDeploymentSnapshot(current);
    }),
};

export const evolveService: MockService<"EvolveService"> = {
  GetEvolve: ({ projectId }, { store }) => ({ evolve: mockEvolve(store, projectId) }),
  StartEvolveReview: ({ projectId, requestId, caller }, { store }) =>
    startMockReview(store, projectId, requestId, mockAccount(caller).accountId),
  DecideEvolveProposal: ({ projectId, caller, ...request }, { store }) =>
    decideMockProposal(store, projectId, request, mockAccount(caller).accountId),
  GetEvolveProposal: ({ projectId, reviewId }, { store }) => getMockProposal(store, projectId, reviewId),
};
