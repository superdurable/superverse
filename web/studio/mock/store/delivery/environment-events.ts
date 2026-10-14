// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock environment timelines: typed events over the Project's mock commits and its publishes,
// newest first.
import type { EnvironmentEvent } from "@/lib/generated/protobuf/superverse/v2/EnvironmentEvent";
import type { Failure } from "@/lib/generated/protobuf/superverse/v2/Failure";
import type { PhaseTiming } from "@/lib/generated/protobuf/superverse/v2/PhaseTiming";
import { isoToTimestamp } from "@/lib/shared/protobuf";
import type { MockPhaseTiming } from "@/mock/store/clock";
import type { MockDeployment } from "@/mock/store/publishing";
import type { MockStore } from "@/mock/store/store";
import { listReleaseCandidates, mockCommitVersion } from "@/mock/store/resource-snapshots";

const MINUTE = 60_000;

const FAILED_START: Failure = {
  code: "PREVIEW_CONFIGURATION_FAILED",
  phase: "start",
  message: "The Preview needs configuration the project does not have yet. Complete Setup, then apply it.",
  isRecoverable: true,
  remedy: "FAILURE_REMEDY_OPEN_SETUP",
  owner: "FAILURE_OWNER_CONFIGURATION",
};

function phase(name: string, startedAt: number, durationMs: number): PhaseTiming {
  return { phase: name, startedAt: isoToTimestamp(new Date(startedAt).toISOString()), durationMs: String(durationMs) };
}

const timings = (list: MockPhaseTiming[]): PhaseTiming[] =>
  list.map((timing) => ({ ...timing, startedAt: isoToTimestamp(timing.startedAt) }));

const OUTCOMES = {
  READY: "START_OUTCOME_READY",
  FAILED: "START_OUTCOME_FAILED",
  CANCELLED: "START_OUTCOME_CANCELLED",
  RUNNING: "START_OUTCOME_RUNNING",
} as const;

/**
 * Live's history of the publishes the mock ran: each build that deploys automatically as PUBLISHED, or
 * PUBLISH_CANCELLED when it or its deployment was cancelled, and each deployment no build started as a
 * deployment of an existing version, a rollback when Live ran a newer one.
 */
function publishedEvents(store: MockStore, projectId: string): EnvironmentEvent[] {
  const deployments = [...store.deployments.values()].filter((item) => item.projectId === projectId);
  const deploymentOf = (id: string | undefined) => deployments.find((item) => item.id === id);
  const started = new Set<MockDeployment>();
  const events: EnvironmentEvent[] = [];
  for (const release of store.releases.values()) {
    if (release.projectId !== projectId) continue;
    for (const build of release.buildRevisions.filter((item) => item.autoDeploy && item.startedBy)) {
      const deployment = deploymentOf(build.autoDeploymentId);
      if (deployment) started.add(deployment);
      const cancelled = build.status === "CANCELLED" || deployment?.status === "CANCELLED";
      const state = deployment?.status ?? build.status;
      events.push({
        kind: cancelled ? "KIND_PUBLISH_CANCELLED" : "KIND_PUBLISHED",
        occurredAt: isoToTimestamp(build.createdAt),
        actorAccountId: "mock-publisher",
        actorDisplay: build.startedBy,
        commit: release.id,
        outcome: OUTCOMES[state === "READY" || state === "FAILED" || state === "CANCELLED" ? state : "RUNNING"],
        failure: build.failure ?? deployment?.failure ?? null,
        phaseTimings: timings([...build.phaseTimings, ...(deployment?.phaseTimings ?? [])]),
        releaseId: release.id,
        deploymentId: deployment?.id ?? "",
        buildRevisionId: build.buildRevisionId,
        version: mockCommitVersion(store, projectId, release.id),
        previewSkipReason: build.previewValidation.skipReason ?? "",
      });
    }
  }
  for (const deployment of deployments.filter((item) => item.startedBy && !started.has(item)))
    events.push({
      kind: deployment.status === "CANCELLED" ? "KIND_PUBLISH_CANCELLED" : "KIND_DEPLOYED_EXISTING_VERSION",
      occurredAt: isoToTimestamp(deployment.updatedAt),
      actorAccountId: "mock-publisher",
      actorDisplay: deployment.startedBy,
      commit: deployment.releaseId,
      outcome:
        OUTCOMES[
          deployment.status === "READY" || deployment.status === "FAILED" || deployment.status === "CANCELLED"
            ? deployment.status
            : "RUNNING"
        ],
      phaseTimings: timings(deployment.phaseTimings),
      releaseId: deployment.releaseId,
      deploymentId: deployment.id,
      version: deployment.version ?? null,
      rolledBack: true,
    });
  return events.toSorted((a, b) => Number(b.occurredAt?.seconds ?? 0) - Number(a.occurredAt?.seconds ?? 0));
}

/**
 * The events of one environment: the Preview's recent starts of the newest two commits, or Live's
 * publishes the mock ran, then its seeded history: a rollback, a cancelled publish and the publish of the
 * older commit.
 */
export function mockEnvironmentEvents(store: MockStore, projectId: string, live: boolean): EnvironmentEvent[] {
  const commits = listReleaseCandidates(store, projectId);
  const [newest, older, oldest = older] = commits;
  if (!newest || !older) return [];
  const now = Date.now();
  const at = (minutesAgo: number) => isoToTimestamp(new Date(now - minutesAgo * MINUTE).toISOString());
  const version = (commit: string) => mockCommitVersion(store, projectId, commit);
  if (live) {
    return [
      ...publishedEvents(store, projectId),
      {
        kind: "KIND_DEPLOYED_EXISTING_VERSION",
        occurredAt: at(20),
        actorAccountId: "mock-operator",
        actorDisplay: "operator@example.test",
        commit: oldest.sourceCommitSha,
        outcome: "START_OUTCOME_READY",
        deploymentId: "deployment-rollback",
        releaseId: oldest.sourceCommitSha,
        version: version(oldest.sourceCommitSha),
        rolledBack: true,
      },
      {
        kind: "KIND_PUBLISH_CANCELLED",
        occurredAt: at(55),
        actorAccountId: "mock-owner",
        actorDisplay: "owner@example.test",
        commit: newest.sourceCommitSha,
        outcome: "START_OUTCOME_CANCELLED",
        releaseId: newest.sourceCommitSha,
        buildRevisionId: "build-cancelled",
        phaseTimings: [
          phase("CONFIGURATION_FREEZE", now - 56 * MINUTE, 400),
          phase("COMPILATION", now - 55.5 * MINUTE, 21_000),
        ],
        version: version(newest.sourceCommitSha),
      },
      {
        kind: "KIND_PUBLISHED",
        occurredAt: at(90),
        actorAccountId: "mock-owner",
        actorDisplay: "owner@example.test",
        commit: older.sourceCommitSha,
        outcome: "START_OUTCOME_READY",
        deploymentId: "deployment-published",
        releaseId: older.sourceCommitSha,
        buildRevisionId: "build-published",
        configurationRevision: "3",
        phaseTimings: [
          phase("COMPILATION", now - 92 * MINUTE, 41_000),
          phase("VerifyReadiness", now - 90 * MINUTE, 9_000),
        ],
        version: version(older.sourceCommitSha),
      },
    ];
  }
  return [
    {
      kind: "KIND_PREVIEW_CONFIGURATION_APPLIED",
      occurredAt: at(4),
      actorAccountId: "mock-owner",
      actorDisplay: "owner@example.test",
      commit: newest.sourceCommitSha,
      configurationRevision: "4",
      outcome: "START_OUTCOME_READY",
      phaseTimings: [phase("application", now - 4 * MINUTE, 23_000)],
      version: version(newest.sourceCommitSha),
    },
    {
      kind: "KIND_PREVIEW_STARTED",
      occurredAt: at(32),
      commit: older.sourceCommitSha,
      outcome: "START_OUTCOME_REFUSED",
      failure: FAILED_START,
      version: version(older.sourceCommitSha),
    },
  ];
}
