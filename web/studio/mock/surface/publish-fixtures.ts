// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Named publishing fixtures a mock page can ask for with `?publishFixture=`, so each stage of a publish,
// a failed one, a cancelled one and a rollback can be seen without waiting for one. Development only.
import { randomHex } from "@/mock/store/clock";
import {
  invalidateConfigurationDraft,
  mockConfigurationDraft,
  mockCopyBasis,
  mockNextPublishValues,
} from "@/mock/store/configuration";
import { advanceMockPublishes, mockRun } from "@/mock/store/delivery/publish-runs";
import { buildMockRelease, deployMockLive, prepareMockRelease, type MockPublishRun } from "@/mock/store/publishing";
import { SEED_HEAD_COMMIT } from "@/mock/store/seed";
import type { MockStore } from "@/mock/store/store";
import { seedSetupFixture } from "./setup-fixtures";

/** A stage that holds: it lasts an hour, so the page shows it as long as anyone looks. */
const HOLD = 3_600_000;
const SECOND = 1_000;
const MINUTE = 60 * SECOND;
const STAGES: MockPublishRun["stageMs"] = [2_500, 16_000, 9_000, 4_500];
/** The seed Project's older main commit, which the later publish fixture puts on Live. */
const OLDER_COMMIT = "dddddddddddddddddddddddddddddddddddddddd";

/** A publish of the seed head: how long its stages take, how long ago it began, how it ends and when it was cancelled. */
interface Seed {
  stageMs: MockPublishRun["stageMs"];
  ago: number;
  outcome?: "failed" | "unhealthy";
  /** Milliseconds after it began that a person cancelled it. */
  cancelAfter?: number;
  /** Then roll Live back to the older version, this long ago, holding in Deploy. */
  rollbackAgo?: number;
}

const SEEDS = {
  // Nothing published since Live: the head waits ahead of Live, as the later-publish Setup fixture leaves it.
  ready: null,
  locking: { stageMs: [HOLD, 16_000, 9_000, 4_500], ago: SECOND },
  building: { stageMs: [2_500, HOLD, 9_000, 4_500], ago: 2_500 + 9 * SECOND },
  deploying: { stageMs: [2_500, 16_000, HOLD, 4_500], ago: 18_500 + 5 * MINUTE },
  "going-live": { stageMs: [2_500, 16_000, 9_000, HOLD], ago: 27_500 + MINUTE },
  published: { stageMs: STAGES, ago: 12 * MINUTE },
  failed: { stageMs: STAGES, ago: 6 * MINUTE, outcome: "unhealthy" },
  "build-failed": { stageMs: STAGES, ago: 6 * MINUTE, outcome: "failed" },
  "cancelled-build": { stageMs: STAGES, ago: 4 * MINUTE, cancelAfter: 2_500 + 8_000 },
  "cancelled-deploy": { stageMs: STAGES, ago: 4 * MINUTE, cancelAfter: 18_500 + 4_000 },
  rollback: { stageMs: STAGES, ago: 30 * MINUTE, rollbackAgo: 4 * MINUTE },
} as const satisfies Record<string, Seed | null>;

/** The fixture each Project shows, so a page asking for another one starts it over. */
const shown = globalThis as { superverseMockPublishFixtures?: Map<string, string> };

/** Builds the head's frozen copy from Live's next publish, as DeriveLiveConfiguration does. */
function frozenCopy(store: MockStore, projectId: string) {
  const copy = mockConfigurationDraft(store, projectId, "live", SEED_HEAD_COMMIT);
  copy.native = structuredClone(mockNextPublishValues(store, projectId));
  invalidateConfigurationDraft(copy);
  copy.live.copiedFrom = mockCopyBasis(store, projectId);
  return copy;
}

/** Drops what an earlier publishing fixture of the Project left, back to the later-publish fixture's Live. */
function reset(store: MockStore, projectId: string) {
  const head = store.releases.get(`${projectId}:${SEED_HEAD_COMMIT}`);
  if (head) {
    head.buildRevisions = head.buildRevisions.filter((build) => !build.run);
    head.state = "DRAFT";
    head.readyBuildRevisionId = "";
  }
  for (const [key, deployment] of store.deployments)
    if (deployment.projectId === projectId && deployment.run) store.deployments.delete(key);
  const older = [...store.deployments.values()].find(
    (item) => item.projectId === projectId && item.releaseId === OLDER_COMMIT,
  );
  if (!older) return;
  store.liveDeployments.set(projectId, older);
  // Live's publish happened an hour ago, before every fixture's publish.
  const hourAgo = new Date(Date.now() - 60 * MINUTE).toISOString();
  older.updatedAt = hourAgo;
  for (const build of store.releases.get(`${projectId}:${OLDER_COMMIT}`)?.buildRevisions ?? [])
    build.createdAt = hourAgo;
}

/** A publish of the seed head that began `seed.ago` ago, run on the mock clock as Studio's publishes are. */
function seedPublish(store: MockStore, projectId: string, seed: Seed) {
  const release = prepareMockRelease(store, projectId, SEED_HEAD_COMMIT);
  const copy = frozenCopy(store, projectId);
  const startedAtMs = Date.now() - seed.ago;
  const build = buildMockRelease(
    store,
    projectId,
    {
      requestId: `publish-fixture-${randomHex(6)}`,
      releaseId: SEED_HEAD_COMMIT,
      buildRevisionId: `build-${randomHex(8)}-${randomHex(4)}-4${randomHex(3)}-8${randomHex(3)}-${randomHex(12)}`,
      configurationRevision: copy.revision,
      expectedRevision: release.revision,
      autoDeploy: true,
      expectedLiveRevision: store.liveDeployments.get(projectId)?.generation ?? "0",
      previewSkipReason: "",
    },
    false,
  );
  if (!build) return;
  build.run = mockRun(seed.outcome, seed.stageMs, startedAtMs);
  build.createdAt = new Date(startedAtMs).toISOString();
  build.startedBy = "owner@example.test";
  // A cancel during the build cancels the build; a later one, the deployment its run starts.
  if (seed.cancelAfter !== undefined) {
    build.run.cancelledAtMs = startedAtMs + seed.cancelAfter;
    if (build.run.cancelledAtMs < startedAtMs + seed.stageMs[0] + seed.stageMs[1])
      build.cancellation = {
        actorAccountId: "mock-owner",
        requestedAt: new Date(build.run.cancelledAtMs).toISOString(),
      };
  }
  advanceMockPublishes(store);
}

/** Rolls Live back to the older commit's build, begun `ago` ago and held in Deploy. */
function seedRollback(store: MockStore, projectId: string, ago: number) {
  const older = store.releases.get(`${projectId}:${OLDER_COMMIT}`);
  const build = older?.buildRevisions.find((item) => item.buildRevisionId === older.readyBuildRevisionId);
  if (!older || !build) return;
  const deployment = deployMockLive(
    store,
    projectId,
    {
      releaseId: OLDER_COMMIT,
      buildRevisionId: build.buildRevisionId,
      configurationRevision: build.configurationRevision,
      expectedLiveRevision: store.liveDeployments.get(projectId)?.generation ?? "0",
      deploymentId: `rollback-${randomHex(8)}`,
    },
    false,
  );
  deployment.run = mockRun(undefined, [0, 0, HOLD, 4_500], Date.now() - ago);
  deployment.updatedAt = new Date(Date.now() - ago).toISOString();
  deployment.startedBy = "owner@example.test";
  advanceMockPublishes(store);
}

/**
 * Seeds the named publishing fixture into the seed Project over the later-publish Setup fixture: none yet,
 * a publish of the head held in one stage, published, failed at its health check or its build, cancelled during its
 * build or its deployment, or a rollback held in Deploy. Asking for another fixture starts it over; an
 * unknown name does nothing.
 */
export function seedPublishFixture(store: MockStore, projectId: string, name: string | undefined) {
  if (!name || !Object.hasOwn(SEEDS, name)) return;
  const fixtures = (shown.superverseMockPublishFixtures ??= new Map());
  if (fixtures.get(projectId) === name) return;
  seedSetupFixture(store, projectId, "later-publish");
  if (!store.liveDeployments.has(projectId)) return;
  reset(store, projectId);
  fixtures.set(projectId, name);
  const seed: Seed | null = SEEDS[name as keyof typeof SEEDS];
  if (!seed) return;
  seedPublish(store, projectId, seed);
  if (seed.rollbackAgo !== undefined) seedRollback(store, projectId, seed.rollbackAgo);
}
