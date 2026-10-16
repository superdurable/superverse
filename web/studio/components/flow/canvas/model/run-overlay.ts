// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowDefinitionConditionKind } from "@/lib/api/flow/flow-definition";
import { formatCoarseElapsed } from "@/lib/shared/format";

/**
 * Run state as an overlay on a definition, never as a second graph. The definition is keyed by
 * StepType and a run by StepExecutionID, so `runStateFor` rolls every execution of a StepType into
 * one card. Definition mode is the absence of an overlay, not a run with every status blank.
 */

/** Normalised phase statuses. An unrecognised backend value must render, never crash. */
const PHASE_STATUSES = ["notStarted", "pending", "waiting", "running", "completed", "failed", "canceled"] as const;

export type PhaseStatus = (typeof PHASE_STATUSES)[number];

const PHASE_LABEL: Record<PhaseStatus, string> = {
  notStarted: "not started",
  pending: "pending",
  waiting: "waiting",
  running: "running",
  completed: "done",
  failed: "failed",
  canceled: "canceled",
};

const OPEN_STATUSES: ReadonlySet<PhaseStatus> = new Set(["waiting", "running", "pending"]);
const BLOCKED_STATUSES: ReadonlySet<PhaseStatus> = new Set(["waiting", "pending"]);

/** Still in progress: blocked on its WaitFor or running. */
export function isOpenStatus(status: PhaseStatus): boolean {
  return OPEN_STATUSES.has(status);
}

/** Blocked on a WaitFor: nothing is consuming anything yet. */
function isBlockedStatus(status: PhaseStatus): boolean {
  return BLOCKED_STATUSES.has(status);
}

export interface RunCondition {
  kind: FlowDefinitionConditionKind;
  label: string;
  /**
   * Which condition won. Dex consumes messages only from the winning condition and leaves the
   * losers queued, so "waiting on A or B" without the winner misreports durable state.
   */
  satisfied: boolean;
}

/** One Step execution; an overlay lists them in the order Dex started them. */
export interface StepExecution {
  stepExecutionId: string;
  stepType: string;
  /** `__rpc/<name>` when an external call started this Step. */
  fromStepExecutionId?: string;
  /** Null when the Step has no WaitFor at all. */
  waitFor: { status: PhaseStatus; conditions: RunCondition[] } | null;
  execute: { status: PhaseStatus };
  attempts: number;
  /** The error the last failed attempt returned, and the type the worker named for it. */
  lastFailure?: string;
  lastFailureType?: string;
  startedAt?: number;
  /** Where a completed execution said it would go next, so the canvas can draw it before it starts. */
  nextStepTypes?: string[];
  /** The decision verb this execution actually returned, not every verb the definition allows. */
  decisionType?: string;
}

export interface RunOverlay {
  /** In the order Dex started them. */
  executions: StepExecution[];
  /** The clock elapsed values are measured against. */
  now: number;
}

/** One card's run facts: its executions, the latest one and the per-status histogram behind its bar. */
interface StepRunState {
  executions: StepExecution[];
  current: StepExecution;
  bar: { status: PhaseStatus; count: number }[];
  planned: false;
}

/** A Step named by a completed execution's decision, with no execution of its own yet. */
interface PlannedRunState {
  planned: true;
}

export type RunState = StepRunState | PlannedRunState;

/**
 * The status a Step card shows. WaitFor is re-evaluated many times, so its having run means
 * "started", while Execute runs once, so its having run means "completed"; the card leads with
 * whichever phase is still open.
 */
export function cardStatus(e: StepExecution): PhaseStatus {
  if (e.execute.status === "failed" || e.waitFor?.status === "failed") return "failed";
  if (e.execute.status === "canceled" || e.waitFor?.status === "canceled") return "canceled";
  if (e.waitFor !== null && isBlockedStatus(e.waitFor.status)) return e.waitFor.status;
  if (e.execute.status === "completed") return "completed";
  if (e.execute.status === "running" || e.waitFor?.status === "completed") return "running";
  return e.execute.status;
}

/** Every execution of a StepType, in the order Dex started them. */
function executionsOf(overlay: RunOverlay, stepType: string): StepExecution[] {
  return overlay.executions.filter((e) => e.stepType === stepType);
}

export function runStateFor(overlay: RunOverlay, stepType: string): RunState | null {
  const executions = executionsOf(overlay, stepType);
  const current = executions.at(-1);
  if (current === undefined) {
    const predicted = overlay.executions.some(
      (e) => e.execute.status === "completed" && (e.nextStepTypes ?? []).includes(stepType),
    );
    return predicted ? { planned: true } : null;
  }

  const tally = new Map<PhaseStatus, number>();
  for (const e of executions) {
    const s = cardStatus(e);
    tally.set(s, (tally.get(s) ?? 0) + 1);
  }
  // Ordered by status so the bar reads the same way regardless of arrival order.
  const bar = PHASE_STATUSES.flatMap((status) => {
    const count = tally.get(status);
    return count === undefined ? [] : [{ status, count }];
  });
  return { executions, current, bar, planned: false };
}

/** Steps whose latest execution is still open, in the order Dex started them. */
export function activeStepTypes(overlay: RunOverlay): string[] {
  return [...new Set(overlay.executions.map((e) => e.stepType))].filter((stepType) => {
    const latest = executionsOf(overlay, stepType).at(-1);
    return latest !== undefined && isOpenStatus(cardStatus(latest));
  });
}

/** Decision verbs surprising enough to name on the card, in plain words. `goTo` never is. */
const SURPRISING: Record<string, string> = {
  deadEnd: "This path of the process ends here",
  forceFail: "The process stopped here and counts as failed",
  forceComplete: "The process finished early here",
  forceCompleteIfChannelsEmpty: "The process finished here because nothing else was waiting",
};

export interface Reason {
  text: string;
  tone: "blocked" | "failed" | "terminal";
  /** The engine's own words behind the line, such as an error or a Channel name; absent when the line is all. */
  detail?: string;
}

/**
 * The why-line a Step card and the run drawer show for a StepType's latest execution: why it is
 * blocked or failed, else a surprising terminal decision it returned. Healthy Steps and Steps that
 * have not run get none, so a green run reads quiet.
 */
export function stepReason(overlay: RunOverlay, stepType: string): Reason | null {
  const latest = executionsOf(overlay, stepType).at(-1);
  if (latest === undefined) return null;
  return reasonLine(latest, overlay.now) ?? terminalReason(latest.decisionType);
}

/** One templated line explaining why a Step is blocked or failed; the engine's words go to `detail`. */
function reasonLine(exec: StepExecution, nowMs: number): Reason | null {
  const w = exec.waitFor;
  const since = exec.startedAt === undefined ? "" : ` · ${formatCoarseElapsed(exec.startedAt, nowMs)}`;

  if (w !== null && w.status === "failed") {
    return { text: "Failed while waiting", tone: "failed", detail: exec.lastFailure };
  }
  if (exec.execute.status === "failed") {
    const n = exec.attempts > 1 ? ` after ${exec.attempts} tries` : "";
    return { text: `Failed${n}`, tone: "failed", detail: exec.lastFailure };
  }
  if (w !== null && isBlockedStatus(w.status)) {
    const pending = w.conditions.filter((c) => !c.satisfied);
    // The Go analyser resolves no conditions, so a blocked wait can legitimately list none.
    if (pending.length === 0) return { text: `Waiting${since}`, tone: "blocked" };
    const chans = pending.filter((c) => c.kind === "channel").map((c) => c.label);
    const timers = pending.filter((c) => c.kind === "timer").map((c) => c.label);
    const subs = pending.filter((c) => c.kind === "subflow").map((c) => c.label);
    const parts: string[] = [];
    if (chans.length > 0) parts.push("a person");
    if (subs.length > 0) parts.push(subs.length === 1 ? "another process" : `${subs.length} other processes`);
    if (timers.length > 0) parts.push("a timer");
    const detail = [
      chans.length > 0 && `Channel ${chans.join(" or ")}`,
      subs.length > 0 && `SubFlow ${subs.join(" or ")}`,
      timers.length > 0 && `Timer ${timers.join(" or ")}`,
    ].filter(Boolean);
    return {
      text: `Waiting for ${parts.join(" or ")}${since}`,
      tone: "blocked",
      detail: `Waits on ${detail.join(", or ")}`,
    };
  }
  return null;
}

/** A surprising terminal decision, named from the verb the execution actually returned. */
function terminalReason(verb: string | undefined): Reason | null {
  if (verb === undefined) return null;
  const said = SURPRISING[verb];
  return said === undefined ? null : { text: said, tone: "terminal", detail: `Decision ${verb}` };
}

/** The hover text for the segmented bar: a count per status, plus the wait-kind breakdown. */
export function barDecode(bar: { status: PhaseStatus; count: number }[], execs: StepExecution[]): string {
  const lines = bar.map((s) => `${s.count} ${PHASE_LABEL[s.status]}`);
  const kinds = new Map<string, number>();
  for (const e of execs) {
    for (const c of e.waitFor?.conditions ?? []) {
      if (c.satisfied) kinds.set(c.kind, (kinds.get(c.kind) ?? 0) + 1);
    }
  }
  if (kinds.size > 0) {
    lines.push("");
    for (const [k, n] of kinds) lines.push(`${n} satisfied by ${k}`);
  }
  return lines.join("\n");
}
