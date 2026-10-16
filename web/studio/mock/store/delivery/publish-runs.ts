// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Publishes that run on the mock clock: a build and its automatic deployment, or a rollback's deployment,
// moved through their stages whenever the store is read, with a build log that grows and a person's
// cancellation. Development only; no provider build or deployment runs.
import type { Failure__Output } from "@/lib/generated/protobuf/superverse/v2/Failure";
import { rpcFailure } from "@/lib/server/grpc-client";
import { httpError } from "@/lib/server/request-body";
import { BUILD_PHASES, DEPLOYMENT_PHASES, type MockPhaseTiming } from "@/mock/store/clock";
import {
  deployMockLive,
  type MockBuildRevision,
  type MockDeployment,
  type MockPublishRun,
  type MockRelease,
} from "@/mock/store/publishing";
import type { MockStore } from "@/mock/store/store";

/** How long each stage after Check takes for a publish started from Studio: Lock settings, Build, Deploy, Go live. */
const STAGE_MS: MockPublishRun["stageMs"] = [2_500, 16_000, 9_000, 4_500];
/** A cancellation is confirmed this long after it was admitted. */
const CANCEL_MS = 3_000;
/** A new build log line at most this often. */
const LINE_MS = 1_100;

const iso = (ms: number) => new Date(ms).toISOString();

/** A run starting now, ending as the frozen configuration's MOCK_BUILD_OUTCOME says: failed, unhealthy or ready. */
export function mockRun(outcome: string | undefined, stageMs = STAGE_MS, startedAtMs = Date.now()): MockPublishRun {
  return {
    startedAtMs,
    stageMs: [...stageMs],
    outcome: outcome === "failed" ? "build-failed" : outcome === "unhealthy" ? "unhealthy" : "ready",
    settled: false,
  };
}

/** `layout`'s phases moved to begin at `startMs` and scaled into `spanMs`, those that ended by `cut`. */
function laidOut(layout: typeof BUILD_PHASES, startMs: number, spanMs: number, cut: number): MockPhaseTiming[] {
  const first = layout[0][1];
  const last = Math.max(...layout.map(([, offset, duration]) => offset + duration)) - first;
  const scale = spanMs / last;
  return layout.flatMap(([phase, offset, duration]) => {
    const start = startMs + (offset - first) * scale;
    const length = Math.max(1, duration * scale);
    return start + length <= cut ? [{ phase, startedAt: iso(start), durationMs: String(Math.round(length)) }] : [];
  });
}

const failure = (code: string, phase: string, message: string, remedy: Failure__Output["remedy"]) => ({
  code,
  phase,
  message,
  isRecoverable: true,
  remedy,
  owner: "FAILURE_OWNER_APPLICATION" as const,
});

/** A deployment's run: Deploy's phases, then Go live's, a failed health check, or a cancel's cleanup. */
function advanceDeployment(store: MockStore, deployment: MockDeployment, now: number) {
  const run = deployment.run!;
  const [, , deployMs, liveMs] = run.stageMs;
  const deployEnd = run.startedAtMs + deployMs;
  const cut = Math.min(now, run.cancelledAtMs ?? now);
  const goLive = DEPLOYMENT_PHASES.slice(DEPLOYMENT_PHASES.findIndex(([phase]) => phase === "VerifyReadiness"));
  const deploy = DEPLOYMENT_PHASES.slice(0, DEPLOYMENT_PHASES.length - goLive.length);
  const live = run.outcome === "unhealthy" ? [] : laidOut(goLive, deployEnd, liveMs, cut);
  deployment.phaseTimings = [...laidOut(deploy, run.startedAtMs, deployMs, cut), ...live];
  if (run.cancelledAtMs !== undefined) {
    if (now < run.cancelledAtMs + CANCEL_MS) return;
    deployment.status = "CANCELLED";
    deployment.phaseTimings.push({
      phase: "CleanupCancelledDeployment",
      startedAt: iso(run.cancelledAtMs),
      durationMs: String(CANCEL_MS),
    });
    run.settled = true;
  } else if (run.outcome === "unhealthy" && now >= deployEnd + liveMs) {
    deployment.status = "FAILED";
    deployment.failure = failure(
      "DEPLOYMENT_READINESS_FAILED",
      "VerifyReadiness",
      "The new version did not answer its health check, so Live kept running what it ran. Its last log line: the summary connection refused the request.",
      "FAILURE_REMEDY_ASK_AGENT",
    );
    run.settled = true;
  } else if (now >= deployEnd + liveMs) {
    deployment.status = "READY";
    deployment.url = `${store.origin}/api/mock-app?env=live&project=${deployment.projectId}`;
    store.liveDeployments.set(deployment.projectId, deployment);
    run.settled = true;
  }
}

/** A build's run: the freeze, the builder's phases together at its end, then its automatic deployment. */
function advanceBuild(store: MockStore, release: MockRelease, build: MockBuildRevision, now: number) {
  const run = build.run!;
  const [lockMs, buildMs, deployMs, liveMs] = run.stageMs;
  const lockEnd = run.startedAtMs + lockMs;
  const buildEnd = lockEnd + buildMs;
  // A cancel admitted once the artifacts committed belongs to the deployment, which takes it over.
  const buildCancel = run.cancelledAtMs !== undefined && run.cancelledAtMs < buildEnd ? run.cancelledAtMs : undefined;
  const cut = Math.min(now, buildCancel ?? now);
  build.phaseTimings = laidOut(BUILD_PHASES.slice(0, 1), run.startedAtMs, lockMs, cut);
  if (buildCancel !== undefined) {
    if (now < buildCancel + CANCEL_MS) return;
    build.status = "CANCELLED";
    release.state = "CANCELLED";
    run.settled = true;
    return;
  }
  if (now < buildEnd) return;
  run.settled = true;
  if (run.outcome === "build-failed") {
    build.phaseTimings.push(...laidOut(BUILD_PHASES.slice(1, 6), lockEnd, buildMs, buildEnd));
    build.status = "FAILED";
    build.failure = failure(
      "BUILD_FAILED",
      "AwaitReleaseBuildRevision",
      "The application did not compile: cmd/server/main.go:41: undefined: summaryTone.",
      "FAILURE_REMEDY_ASK_AGENT",
    );
    release.state = "FAILED";
    return;
  }
  build.phaseTimings.push(...laidOut(BUILD_PHASES.slice(1), lockEnd, buildMs, buildEnd));
  build.status = "READY";
  release.state = "READY";
  release.readyBuildRevisionId = build.buildRevisionId;
  release.revision = String(Number(release.revision) + 1);
  try {
    const deployment = deployMockLive(
      store,
      release.projectId,
      {
        releaseId: release.id,
        buildRevisionId: build.buildRevisionId,
        configurationRevision: build.configurationRevision,
        expectedLiveRevision: build.expectedLiveRevision,
        deploymentId: `mock-auto-${build.buildRevisionId.slice(6, 29)}`,
      },
      false,
    );
    deployment.updatedAt = iso(buildEnd);
    deployment.run = { startedAtMs: buildEnd, stageMs: [0, 0, deployMs, liveMs], outcome: run.outcome, settled: false };
    if (run.cancelledAtMs !== undefined) {
      deployment.run.cancelledAtMs = run.cancelledAtMs;
      deployment.status = "CANCELLING";
      deployment.cancellation = { actorAccountId: "mock-owner", requestedAt: iso(run.cancelledAtMs) };
    }
    build.autoDeploymentId = deployment.id;
    build.autoDeployStatus = "ACCEPTED";
    advanceDeployment(store, deployment, now);
  } catch (error) {
    build.autoDeployStatus = "REJECTED";
    build.autoDeployFailure = { code: "MOCK_LIVE_CHANGED", message: (error as Error).message };
  }
}

/** Moves every running publish of the store to where the clock says it is. */
export function advanceMockPublishes(store: MockStore) {
  const now = Date.now();
  for (const release of store.releases.values())
    for (const build of release.buildRevisions)
      if (build.run && !build.run.settled) advanceBuild(store, release, build, now);
  for (const deployment of store.deployments.values())
    if (deployment.run && !deployment.run.settled) advanceDeployment(store, deployment, now);
}

const refused = (message: string, reason: string, metadata: Record<string, string> = {}) =>
  rpcFailure(409, message, {
    reason,
    domain: "superverse.v2",
    metadata: {
      remedy: reason === "RELEASE_BUILD_DEPLOYING" ? "FAILURE_REMEDY_CANCEL_DEPLOYMENT" : "FAILURE_REMEDY_APPLY_AGAIN",
      ...metadata,
    },
  });

/**
 * A person's cancellation of a build until its artifacts commit; once they did, the refusal names the
 * deployment to cancel instead. A repeated request is accepted unchanged.
 */
export function cancelMockBuild(
  store: MockStore,
  release: MockRelease,
  buildRevisionId: string,
  actorAccountId: string,
) {
  advanceMockPublishes(store);
  const build = release.buildRevisions.find((item) => item.buildRevisionId === buildRevisionId);
  if (!build) throw httpError(404, "Mock build not found");
  if (build.cancellation || build.status === "CANCELLED") return;
  if (build.status === "BUILDING" && build.run) {
    build.run.cancelledAtMs = Date.now();
    build.cancellation = { actorAccountId, requestedAt: iso(build.run.cancelledAtMs) };
    return;
  }
  if (build.status === "READY" && build.autoDeploymentId)
    throw refused(
      "The build is published and its deployment is admitted; cancel the deployment instead",
      "RELEASE_BUILD_DEPLOYING",
      {
        deploymentId: build.autoDeploymentId,
      },
    );
  throw refused("The build is not in progress, so there is nothing to cancel", "RELEASE_BUILD_NOT_CANCELLABLE");
}

/** A person's cancellation of a deployment until its health check passed; Live is untouched. */
export function cancelMockDeployment(store: MockStore, deployment: MockDeployment, actorAccountId: string) {
  advanceMockPublishes(store);
  if (deployment.cancellation || deployment.status === "CANCELLED") return;
  const checked = deployment.phaseTimings.some((timing) => timing.phase === "VerifyReadiness");
  if (deployment.status !== "DEPLOYING" || !deployment.run || checked)
    throw refused(
      "The deployment is live or going live, so it can no longer be cancelled",
      "DEPLOYMENT_NOT_CANCELLABLE",
    );
  deployment.run.cancelledAtMs = Date.now();
  deployment.status = "CANCELLING";
  deployment.cancellation = { actorAccountId, requestedAt: iso(deployment.run.cancelledAtMs) };
}

/** The builder's output of a mock build, with a configured value redacted as the server redacts it. */
const BUILD_OUTPUT = [
  "#1 [internal] load build definition from Dockerfile",
  "#1 transferring dockerfile: 1.21kB done",
  "#2 [internal] load metadata for docker.io/library/golang:1.25-alpine",
  "#3 [builder 1/6] FROM docker.io/library/golang:1.25-alpine",
  "#3 CACHED",
  "#4 [builder 2/6] WORKDIR /src",
  "#5 [builder 3/6] COPY go.mod go.sum ./",
  "#6 [builder 4/6] RUN --mount=type=cache,target=/go/pkg/mod go mod download",
  "#6 go: downloading github.com/superdurable/dex/sdk-go v1.10.1",
  "#6 go: downloading github.com/superdurable/dex-connectors-library/llm v0.21.0",
  "#6 38 of 41 modules from cache",
  "#7 [builder 5/6] COPY . .",
  "#8 analysis: 2 Flows, 9 Steps, 1 connection name",
  "#8 environment: SUMMARY_MODEL=gpt-5-mini OPENAI_API_KEY=[redacted]",
  "#9 [builder 6/6] RUN go build -trimpath -o /out/app ./cmd/server",
  "#9 compiling 214 packages for linux/amd64",
  "#9 compiled in 41.2s",
  "#10 [runtime 1/2] COPY --from=builder /out/app /app",
  "#11 exporting to image",
  "#11 pushing layers 48.2 MB",
];
const FAILED_OUTPUT = [
  "#9 cmd/server/main.go:41:12: undefined: summaryTone",
  '#9 ERROR: process "go build" exited with code 1',
];
const DONE_OUTPUT = ["#11 pushed sha256 digest of the image", "Build complete; publishing the Flow definitions"];

/**
 * One page of a build's log from `cursor`, a line index: lines appear while the build runs, the rest when it
 * ends, and the log is complete once the build settled and every line was read.
 */
export function mockBuildLogPage(build: MockBuildRevision, cursor: string) {
  const run = build.run;
  const settled = build.status !== "BUILDING";
  let lines: string[];
  if (!run) {
    lines = [
      "Mock build log: development fixture, no provider build ran.",
      ...build.phaseTimings.map((timing) => `${timing.phase} finished in ${Number(timing.durationMs) / 1000}s`),
      ...(build.failure ? [`error: ${build.failure.message}`] : []),
    ];
  } else {
    const lockEnd = run.startedAtMs + run.stageMs[0];
    const until = Math.min(Date.now(), run.cancelledAtMs ?? Date.now());
    const every = Math.min(LINE_MS, run.stageMs[1] / BUILD_OUTPUT.length);
    const shown =
      settled && build.status !== "CANCELLED"
        ? run.outcome === "build-failed"
          ? 16
          : BUILD_OUTPUT.length
        : Math.max(0, Math.min(BUILD_OUTPUT.length, Math.floor((until - lockEnd) / every)));
    lines = BUILD_OUTPUT.slice(0, shown);
    if (build.status === "FAILED") lines.push(...FAILED_OUTPUT);
    if (build.status === "READY") lines.push(...DONE_OUTPUT);
    if (build.status === "CANCELLED") lines.push("Build stopped: cancelled by a person");
  }
  const from = Math.min(Number(cursor) || 0, lines.length);
  return { lines: lines.slice(from), nextCursor: String(lines.length), complete: settled, retryAfterMs: 2_000 };
}
