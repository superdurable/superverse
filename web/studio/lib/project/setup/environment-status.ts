// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { DeploymentSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/DeploymentSnapshot";
import type { Failure__Output } from "@/lib/generated/protobuf/superverse/v2/Failure";
import type { PreviewSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewSnapshot";
import type { ProjectConfigurationSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import type { ReleaseBuildRevisionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ReleaseBuildRevisionSnapshot";
import { timestampToISO } from "@/lib/shared/protobuf";
import { cannotCheckText, failureProblem, type Problem } from "@/lib/shared/problem/problem";
import { versionLabel } from "@/lib/shared/version/version";
import { gapAnchor } from "./configuration-items";

/**
 * The phases of a Preview start in order, its Flow's Steps from the configuration check to the
 * route, with their labels. Phase is an open set: a code not listed is shown as the server sent it.
 */
export const PREVIEW_PHASE_LABELS: Readonly<Record<string, string>> = {
  configuration: "Validating configuration",
  environment: "Starting engine",
  grant: "Authorizing",
  application: "Building app",
  route: "Exposing route",
};
const PREVIEW_PHASES = Object.keys(PREVIEW_PHASE_LABELS);

/**
 * How a status looks: `fail` red, `attention` amber, `pulse` blue and pulsing while it moves, `ok` the
 * environment's own colour while it runs as it should (grey Preview, green Live), and `plain` a hollow
 * mark while it is stopped or not there yet.
 */
export type StatusTone = "fail" | "attention" | "pulse" | "ok" | "plain";

/**
 * Where a status's word leads to put it right: the Interface, which starts, stops and explains the
 * Preview; Tools ▸ Setup, at the anchor of the first item to fix; or Tools ▸ Publishing.
 */
export type StatusFix = { to: "interface" } | { to: "setup"; anchor?: string } | { to: "publishing" };

/** The Preview's statuses, in precedence order: the first that applies wins. */
export type PreviewStatusKind = "FAILED" | "STOPPING" | "STARTING" | "MISSING" | "CANNOT_CHECK" | "RUNNING" | "STOPPED";

/** Live's statuses, in precedence order. */
export type LiveStatusKind = "PUBLISHING" | "FAILED" | "RUNNING" | "NOT_PUBLISHED";

/**
 * The one status an environment tab shows. The tab says a word only when something is off, and the
 * word leads to the fix when there is one; `detail` is the whole status, for the hover hint and the
 * tab's accessible name.
 */
export interface EnvironmentStatus<Kind extends string> {
  kind: Kind;
  tone: StatusTone;
  /** The word the tab shows, or null while the environment runs as it should. */
  word: string | null;
  /** Where the word leads, or null when there is nothing to do but wait. */
  fix: StatusFix | null;
  detail: string;
  problem: Problem | null;
}

function status<Kind extends PreviewStatusKind | LiveStatusKind>(
  kind: Kind,
  tone: StatusTone,
  word: string | null,
  fix: StatusFix | null,
  detail: string,
  problem: Problem | null = null,
): EnvironmentStatus<Kind> {
  return { kind, tone, word, fix, detail, problem };
}

const INTERFACE: StatusFix = { to: "interface" };
const PUBLISHING: StatusFix = { to: "publishing" };

const plural = (count: number, one: string, many: string) => `${count} ${count === 1 ? one : many}`;

/**
 * Which step of the five a start is at, 1-based, with that step's label: the newest phase it began.
 * Null before the first phase begins.
 */
function previewStep(preview: PreviewSnapshot__Output): { step: number; label: string } | null {
  const begun = preview.phaseTimings
    .map((timing) => PREVIEW_PHASES.indexOf(timing.phase))
    .filter((index) => index >= 0);
  if (begun.length === 0) return null;
  const index = Math.max(...begun);
  return { step: index + 1, label: PREVIEW_PHASE_LABELS[PREVIEW_PHASES[index]] };
}

/** The label of the phase the Preview is in now, the newest one without a duration; null when none is. */
function phaseInProgress(preview: PreviewSnapshot__Output): string | null {
  const newest = preview.phaseTimings
    .filter((timing) => Number(timing.durationMs) === 0)
    .toSorted(
      (a, b) =>
        (Date.parse(timestampToISO(b.startedAt) ?? "") || 0) - (Date.parse(timestampToISO(a.startedAt) ?? "") || 0),
    )[0];
  return newest ? (PREVIEW_PHASE_LABELS[newest.phase] ?? newest.phase) : null;
}

/**
 * The Preview's one status, from its snapshot and the configuration of its prepared source (null
 * while that is read). A failure counts only while the newest recorded start failed or was refused,
 * so a failed start never shows as starting. Running says no word; a start, a stop, a failure, Setup
 * that keeps the Preview from starting and a stopped Preview do. Unapplied changes are normal while
 * editing: they say no word, and only `setupNeedsYou` marks them.
 */
export function previewStatusOf(
  preview: PreviewSnapshot__Output | null,
  configuration: ProjectConfigurationSnapshot__Output | null,
): EnvironmentStatus<PreviewStatusKind> {
  const newest = preview?.recentStarts.at(-1);
  if (preview?.failure && (newest?.outcome === "START_OUTCOME_FAILED" || newest?.outcome === "START_OUTCOME_REFUSED")) {
    const problem = failureProblem(preview.failure, { operation: "Starting the Preview" });
    return status("FAILED", "fail", "failed", INTERFACE, `failed: ${problem.title}`, problem);
  }
  if (preview?.status === "PREVIEW_STATUS_STOPPING") {
    // A stop whose cleanup failed waits for Retry Preview cleanup, on the Interface.
    if (preview.failure) {
      const problem = failureProblem(preview.failure, { operation: "Stopping the Preview" });
      return status("STOPPING", "fail", "stop failed", INTERFACE, `stopping did not finish: ${problem.title}`, problem);
    }
    const phase = phaseInProgress(preview);
    return status("STOPPING", "pulse", "stopping", null, phase ? `stopping · ${phase}` : "stopping");
  }
  if (preview?.status === "PREVIEW_STATUS_STARTING")
    return status("STARTING", "pulse", "starting", null, startingDetail(preview));
  if (preview?.automaticDeployment?.status === "PREVIEW_DEPLOYMENT_STATUS_RUNNING")
    return status("STARTING", "pulse", "updating", null, "updating to the latest push");
  if (configuration && configuration.status !== "CONFIGURATION_STATUS_READY") {
    const gaps = configuration.requirementGaps;
    if (gaps.length > 0)
      return status(
        "MISSING",
        "attention",
        "needs setup",
        { to: "setup", anchor: gapAnchor(gaps[0]) },
        `missing ${plural(gaps.length, "item", "items")}`,
      );
    return status(
      "CANNOT_CHECK",
      "attention",
      "cannot check setup",
      { to: "setup" },
      cannotCheckText(configuration.validationErrors),
    );
  }
  if (preview?.status === "PREVIEW_STATUS_READY")
    return status("RUNNING", "ok", null, null, `running · ${versionLabel(preview.servingVersion)}`);
  const never = !preview || preview.status === "PREVIEW_STATUS_NOT_STARTED";
  const words = never ? "not started" : "stopped";
  return status("STOPPED", "plain", words, INTERFACE, words);
}

/**
 * Whether Setup's Preview column needs a person: the prepared version lacks items, or saved changes wait to
 * be applied. Tools and its Setup entry carry a mark while it does.
 */
export function setupNeedsYou(configuration: ProjectConfigurationSnapshot__Output | null): boolean {
  if (!configuration) return false;
  const gaps = configuration.status !== "CONFIGURATION_STATUS_READY" && configuration.requirementGaps.length > 0;
  return gaps || configuration.unappliedChangeCount > 0;
}

/** A start in words: "starting · step 2 of 5, Starting engine", or "starting" before its first phase. */
function startingDetail(preview: PreviewSnapshot__Output): string {
  const step = previewStep(preview);
  return step ? `starting · step ${step.step} of ${PREVIEW_PHASES.length}, ${step.label}` : "starting";
}

/** The newest build or deployment of the Project's publishing, whichever began last. */
export type NewestPublish =
  | { kind: "build"; build: ReleaseBuildRevisionSnapshot__Output }
  | { kind: "deployment"; deployment: DeploymentSnapshot__Output };

/** The newer of a build and a deployment by when each began; either may be absent. */
export function newestPublish(
  build: ReleaseBuildRevisionSnapshot__Output | null,
  deployment: DeploymentSnapshot__Output | null,
): NewestPublish | null {
  const began = (value: { createdAt: Parameters<typeof timestampToISO>[0] } | null) =>
    value ? Date.parse(timestampToISO(value.createdAt) ?? "") || 0 : -1;
  if (build && began(build) > began(deployment)) return { kind: "build", build };
  return deployment ? { kind: "deployment", deployment } : null;
}

function publishFailure(newest: NewestPublish | null): Failure__Output | null {
  if (newest?.kind === "build" && newest.build.status === "RELEASE_STATUS_FAILED") return newest.build.failure;
  if (newest?.kind === "deployment" && newest.deployment.status === "DEPLOYMENT_STATUS_FAILED")
    return newest.deployment.failure;
  return null;
}

/**
 * Live's one status: publishing while a deployment is active, failed when the newest build or
 * deployment failed, running with its version and how far it is behind, or not published yet. Only
 * publishing and a failed publish say a word; both lead to Tools ▸ Publishing.
 */
export function liveStatusOf(
  project: ProjectSnapshot__Output | null,
  newest: NewestPublish | null,
  behind: string,
): EnvironmentStatus<LiveStatusKind> {
  if (project?.activeDeployment) return status("PUBLISHING", "pulse", "publishing", PUBLISHING, "publishing");
  const failure = publishFailure(newest);
  if (failure) {
    const problem = failureProblem(failure, {
      operation: newest?.kind === "build" ? "Building the version" : "Deploying Live",
    });
    return status("FAILED", "fail", "publish failed", PUBLISHING, `publish failed: ${problem.title}`, problem);
  }
  const live = project?.live && !project.live.isRemoving ? project.live : null;
  if (live)
    return status(
      "RUNNING",
      "ok",
      null,
      null,
      `running · ${versionLabel(live.version)}${behind ? ` · ${behind}` : ""}`,
    );
  return status("NOT_PUBLISHED", "plain", null, null, "not published yet");
}

/**
 * How long a restart takes, in words: the median of the recorded starts that reached READY, each the sum of
 * its phases, or "about 1 minute" without one.
 */
export function startDurationWords(preview: PreviewSnapshot__Output | null): string {
  const durations = (preview?.recentStarts ?? [])
    .filter((start) => start.outcome === "START_OUTCOME_READY" && start.phaseTimings.length > 0)
    .map((start) => start.phaseTimings.reduce((sum, timing) => sum + Number(timing.durationMs), 0))
    .toSorted((a, b) => a - b);
  if (durations.length === 0) return "about 1 minute";
  const seconds = Math.max(1, Math.round(durations[Math.floor(durations.length / 2)] / 1000));
  if (seconds < 60) return `about ${seconds} seconds`;
  const minutes = Math.round(seconds / 60);
  return `about ${minutes} minute${minutes === 1 ? "" : "s"}`;
}
