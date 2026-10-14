// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { PhaseTiming__Output } from "@/lib/generated/protobuf/superverse/v2/PhaseTiming";
import {
  stageOfPhase,
  stagesOf,
  type PublishFacts,
  type PublishStage,
  type PublishStageId,
} from "@/lib/project/publishing/publish-stages";
import { buildValidationText } from "@/lib/project/setup/preview-validation";
import { formatElapsedDuration } from "@/lib/shared/format";
import { timestampToISO } from "@/lib/shared/protobuf";
import { DEPLOYMENT_PHASE_LABELS, RELEASE_PHASE_LABELS } from "@/components/publishing/timings/phase-labels";

/** One line of the inline log: a line the platform recorded, with its time and level, or the build's own output. */
interface LogLine {
  at: string | null;
  level: "info" | "warn" | "error" | null;
  text: string;
}

/** The log of one stage, in the order the stages run. */
export interface LogGroup {
  stage: PublishStage;
  lines: LogLine[];
}

const LABELS: Readonly<Record<string, string>> = { ...RELEASE_PHASE_LABELS, ...DEPLOYMENT_PHASE_LABELS };

/** A measured phase as a line: what ended and how long it took, at its start. */
function phaseLine(timing: PhaseTiming__Output, detail = ""): LogLine {
  const label = LABELS[timing.phase] ?? timing.phase;
  return {
    at: timestampToISO(timing.startedAt),
    level: "info",
    text: `${label}: done in ${formatElapsedDuration(Number(timing.durationMs))}${detail}`,
  };
}

const byStart = (a: PhaseTiming__Output, b: PhaseTiming__Output) =>
  Date.parse(timestampToISO(a.startedAt) ?? "") - Date.parse(timestampToISO(b.startedAt) ?? "");

/**
 * The inline log grouped by stage: the admission under Check, the configuration freeze under Lock settings,
 * the build's own output (`buildLines`) and its artifact publication under Build, and the deployment's
 * phases, cancellation and failure under Deploy and Go live; the deployment keeps no log of its own. A
 * line of a phase outside the bar, such as the cleanup after a cancel, joins the stage the publish stopped in.
 */
export function logGroups(facts: PublishFacts, buildLines: readonly string[], liveVersion: string): LogGroup[] {
  const lines = new Map<PublishStageId, LogLine[]>();
  const add = (stage: PublishStageId, line: LogLine) => lines.set(stage, [...(lines.get(stage) ?? []), line]);
  const { build, deployment } = facts;
  const cancelLine = (requestedAt: Parameters<typeof timestampToISO>[0]): LogLine => ({
    at: timestampToISO(requestedAt),
    level: "warn",
    text: `Cancel requested; Live still runs ${liveVersion}`,
  });
  let told = false;
  // Where a cancellation, a failure or a phase outside the bar is told: the stage reached last.
  let last: PublishStageId = facts.kind === "rollback" ? "deploy" : "lock";
  if (build) {
    add("check", {
      at: timestampToISO(build.createdAt),
      level: "info",
      text: `Admitted: ${buildValidationText(build.previewValidation)}`,
    });
    for (const timing of build.phaseTimings.toSorted(byStart)) {
      if (timing.phase === "CONFIGURATION_FREEZE")
        add("lock", phaseLine(timing, `; settings locked as configuration revision ${build.configurationRevision}`));
      else if (timing.phase === "ARTIFACT_PUBLICATION") add("build", phaseLine(timing));
    }
    if (build.phaseTimings.some((timing) => timing.phase === "CONFIGURATION_FREEZE")) last = "build";
    const output = buildLines.map((text): LogLine => ({ at: null, level: null, text }));
    lines.set("build", [...output, ...(lines.get("build") ?? [])]);
    if (build.cancellation) add(last, cancelLine(build.cancellation.requestedAt));
    if (build.failure) add(last, { at: null, level: "error", text: build.failure.message });
    if (build.autoDeployFailure) add("deploy", { at: null, level: "error", text: build.autoDeployFailure.message });
  }
  if (deployment) {
    // A cancellation is told before what ran from the moment it was asked, the cleanup after it.
    const cancelled = deployment.cancellation && Date.parse(timestampToISO(deployment.cancellation.requestedAt) ?? "");
    for (const timing of deployment.phaseTimings.toSorted(byStart)) {
      const startedAt = Date.parse(timestampToISO(timing.startedAt) ?? "");
      if (cancelled && deployment.cancellation && startedAt >= cancelled && !told) {
        told = true;
        add(last, cancelLine(deployment.cancellation.requestedAt));
      }
      const stage: PublishStageId = stageOfPhase(timing.phase)?.id ?? last;
      if (stage === "deploy" || stage === "live") last = stage;
      add(stage, phaseLine(timing));
    }
    if (deployment.cancellation && !told) add(last, cancelLine(deployment.cancellation.requestedAt));
    if (deployment.failure) add(last, { at: null, level: "error", text: deployment.failure.message });
  }
  return stagesOf(facts.kind).flatMap((stage) => {
    const own = lines.get(stage.id);
    return own?.length ? [{ stage, lines: own }] : [];
  });
}

/** A log line's time of day with seconds, in the viewer's zone. */
export function clockTime(at: string): string {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hourCycle: "h23",
  }).format(date);
}
