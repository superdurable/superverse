// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { DeploymentSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/DeploymentSnapshot";
import type { DeploymentStatus__Output } from "@/lib/generated/protobuf/superverse/v2/DeploymentStatus";
import type { PhaseTiming__Output } from "@/lib/generated/protobuf/superverse/v2/PhaseTiming";
import type { ReleaseBuildRevisionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseBuildRevisionSnapshot";
import type { ReleaseStatus__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseStatus";
import { timestampToISO } from "@/lib/shared/protobuf";

/**
 * The five stages of a publish in the order they run, each with the phases its build or its deployment
 * records (observability.md, Publishing stages). The backend freezes the configuration before it builds,
 * so Lock settings comes before Build. Check has no phase: it is the popover's admission, done once the
 * build revision exists.
 */
export const PUBLISH_STAGES = [
  { id: "check", label: "Check", of: "build", phases: [] },
  { id: "lock", label: "Lock settings", of: "build", phases: ["CONFIGURATION_FREEZE"] },
  {
    id: "build",
    label: "Build",
    of: "build",
    phases: [
      "QUEUE",
      "HOST_PREPARATION",
      "DEPENDENCY_PREPARATION",
      "ANALYSIS",
      "COMPILATION",
      "IMAGE_PUBLICATION",
      "ARTIFACT_PUBLICATION",
    ],
  },
  {
    id: "deploy",
    label: "Deploy",
    of: "deployment",
    phases: [
      "AdmitDeployment",
      "AcquireProjectDeployment",
      "VerifyReleaseAndConfiguration",
      "PublishDefinitions",
      "PrepareLiveExecution",
      "PrepareLiveTemporal",
      "PrepareLiveAccess",
      "StartLiveDex",
      "ApplyApplication",
    ],
  },
  {
    id: "live",
    label: "Go live",
    of: "deployment",
    phases: [
      "VerifyReadiness",
      "CommitCurrentLive",
      "StopReplacedWorker",
      "RequestReplacedRetirement",
      "ReleaseProjectDeployment",
    ],
  },
] as const satisfies readonly { id: string; label: string; of: "build" | "deployment"; phases: readonly string[] }[];

export type PublishStage = (typeof PUBLISH_STAGES)[number];
export type PublishStageId = PublishStage["id"];

/** The health check: once it succeeded the Flow commits the new Live next, so Cancel is disabled. */
const HEALTH_CHECK = "VerifyReadiness";

/**
 * What a publish is: a version (all five stages), Live's own version again with its waiting settings (all
 * five; the build records LIVE_CODE), or a rollback, an existing build deployed again (Deploy and Go live).
 */
export type PublishKind = "version" | "settings" | "rollback";

/** One publish as its Watches report it: the build revision (none for a rollback) and its deployment. */
export interface PublishFacts {
  kind: PublishKind;
  build: ReleaseBuildRevisionSnapshot__Output | null;
  deployment: DeploymentSnapshot__Output | null;
}

/** Where a publish stands as a whole. */
export type PublishOutcome = "running" | "cancelling" | "published" | "failed" | "cancelled";

/** Where one stage stands; stopped is the stage a cancellation ended in. */
export type StageState = "done" | "running" | "waiting" | "failed" | "stopped";

/** One stage of the bar: its state and, once it has measured phases, its wall-clock span. */
export interface StageView {
  stage: PublishStage;
  state: StageState;
  durationMs: number | null;
}

const BUILD_RUNNING: ReadonlySet<ReleaseStatus__Output> = new Set([
  "RELEASE_STATUS_PENDING",
  "RELEASE_STATUS_DRAFT",
  "RELEASE_STATUS_BUILDING",
  "RELEASE_STATUS_RECOVERING",
]);

/** A deployment that served Live: Live now, or replaced since by a newer publish. */
const WENT_LIVE: ReadonlySet<DeploymentStatus__Output> = new Set([
  "DEPLOYMENT_STATUS_READY",
  "DEPLOYMENT_STATUS_RETIRING",
  "DEPLOYMENT_STATUS_RETIRED",
]);

const DEPLOYMENT_RUNNING: ReadonlySet<DeploymentStatus__Output> = new Set([
  "DEPLOYMENT_STATUS_PENDING",
  "DEPLOYMENT_STATUS_DEPLOYING",
  "DEPLOYMENT_STATUS_RECOVERING",
]);

const timed = (timings: readonly PhaseTiming__Output[], phase: string) =>
  timings.some((timing) => timing.phase === phase);

/** The stages a publish of `kind` shows: a rollback deploys a build that already exists. */
export function stagesOf(kind: PublishKind): readonly PublishStage[] {
  return kind === "rollback" ? PUBLISH_STAGES.filter((stage) => stage.of === "deployment") : PUBLISH_STAGES;
}

/** Whether a stage is done: its last phase has a timing, or what it produces exists. */
function stageDone(stage: PublishStage, { build, deployment }: PublishFacts): boolean {
  const built = build?.status === "RELEASE_STATUS_READY";
  const buildTimings = build?.phaseTimings ?? [];
  const deploymentTimings = deployment?.phaseTimings ?? [];
  const live = Boolean(deployment && WENT_LIVE.has(deployment.status));
  switch (stage.id) {
    case "check":
      return build !== null;
    case "lock":
      return built || timed(buildTimings, "CONFIGURATION_FREEZE");
    case "build":
      return built || timed(buildTimings, "ARTIFACT_PUBLICATION");
    case "deploy":
      return live || timed(deploymentTimings, "ApplyApplication") || timed(deploymentTimings, HEALTH_CHECK);
    case "live":
      return live;
  }
}

/** Whether a person's cancellation of the build or the deployment is admitted. */
function cancellationAdmitted({ build, deployment }: PublishFacts): boolean {
  return Boolean(
    (build?.cancellation && BUILD_RUNNING.has(build.status)) || deployment?.status === "DEPLOYMENT_STATUS_CANCELLING",
  );
}

/** Where a publish stands: a cancellation or a failure ends it before Go live does. */
export function publishOutcome(facts: PublishFacts): PublishOutcome {
  const { build, deployment } = facts;
  if (build?.status === "RELEASE_STATUS_CANCELLED" || deployment?.status === "DEPLOYMENT_STATUS_CANCELLED")
    return "cancelled";
  if (cancellationAdmitted(facts)) return "cancelling";
  if (deployment && WENT_LIVE.has(deployment.status)) return "published";
  const rejected =
    build?.autoDeployStatus === "AUTO_DEPLOY_STATUS_REJECTED" ||
    build?.autoDeployStatus === "AUTO_DEPLOY_STATUS_UNKNOWN";
  const deploymentEnded =
    deployment?.status === "DEPLOYMENT_STATUS_FAILED" || deployment?.status === "DEPLOYMENT_STATUS_REMOVED";
  if (build?.status === "RELEASE_STATUS_FAILED" || rejected || deploymentEnded) return "failed";
  return "running";
}

/** A stage's wall-clock span, from its first phase's start to its last phase's end; overlaps count once. */
function stageSpan(stage: PublishStage, facts: PublishFacts): number | null {
  const timings = (stage.of === "build" ? facts.build?.phaseTimings : facts.deployment?.phaseTimings) ?? [];
  return timingSpan(timings.filter((timing) => (stage.phases as readonly string[]).includes(timing.phase)));
}

/**
 * The bar: every stage done up to the first that is not, which runs, failed or stopped by the outcome, and
 * the rest waiting.
 */
export function publishStages(facts: PublishFacts): StageView[] {
  const outcome = publishOutcome(facts);
  const current: StageState =
    outcome === "failed"
      ? "failed"
      : outcome === "cancelled"
        ? "stopped"
        : outcome === "published"
          ? "done"
          : "running";
  let reached = false;
  return stagesOf(facts.kind).map((stage) => {
    const done = !reached && stageDone(stage, facts);
    const state: StageState = done ? "done" : reached ? "waiting" : current;
    if (!done) reached = true;
    return { stage, state, durationMs: done ? stageSpan(stage, facts) : null };
  });
}

/** The wall-clock span of measured phases, from the first one's start to the last one's end; null without one. */
export function timingSpan(timings: readonly PhaseTiming__Output[]): number | null {
  if (timings.length === 0) return null;
  const starts = timings.map((timing) => Date.parse(timestampToISO(timing.startedAt) ?? ""));
  const ends = timings.map((timing, index) => starts[index] + Number(timing.durationMs));
  const span = Math.max(...ends) - Math.min(...starts);
  return Number.isFinite(span) ? span : null;
}

/** A publish's whole span, from its first measured phase to its last; null before one is measured. */
export function publishSpan(facts: PublishFacts): number | null {
  return timingSpan([...(facts.build?.phaseTimings ?? []), ...(facts.deployment?.phaseTimings ?? [])]);
}

/** What Cancel stops: the build until its automatic deployment is admitted, then that deployment. */
export type CancelTarget = { kind: "build" } | { kind: "deployment"; deploymentId: string };

/**
 * What Cancel stops now, or why it cannot: nothing runs, or the health check passed and the Flow commits
 * the new Live next.
 */
export function cancelTargetOf(facts: PublishFacts): CancelTarget | "too-late" | null {
  const { build, deployment } = facts;
  if (publishOutcome(facts) !== "running") return null;
  if (deployment && DEPLOYMENT_RUNNING.has(deployment.status))
    return timed(deployment.phaseTimings, HEALTH_CHECK)
      ? "too-late"
      : { kind: "deployment", deploymentId: deployment.deploymentId };
  if (build?.autoDeployStatus === "AUTO_DEPLOY_STATUS_ACCEPTED" && build.autoDeploymentId)
    return { kind: "deployment", deploymentId: build.autoDeploymentId };
  if (build && (BUILD_RUNNING.has(build.status) || build.status === "RELEASE_STATUS_READY")) return { kind: "build" };
  return null;
}

/** The stage a measured phase belongs to, for grouping log lines; null for one outside the bar. */
export function stageOfPhase(phase: string): PublishStage | null {
  return PUBLISH_STAGES.find((stage) => (stage.phases as readonly string[]).includes(phase)) ?? null;
}
