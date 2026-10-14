// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowApiClient } from "@/lib/api/transport/flow-api-client";
import { closeDecisionVerb } from "@/lib/flow/enum-labels";
import type { FlowDefinitionConditionKind } from "@/lib/api/flow/flow-definition";
import { getFlowState, getFlowSummary, loadRunHistory } from "@/lib/api/flow/flow-diagnostics";
import { isOpenFlowStatusCode } from "@/lib/api/flow/flow-status";
import type { ActiveStepExecution, FlowHistoryEvent, FlowSummary } from "@/lib/api/flow/flow-types";
import { asRecord, numberField, stringField } from "@/lib/shared/json";
import type { PhaseStatus, RunCondition, RunOverlay, StepExecution } from "./run-overlay";

const STEP_EVENT_TYPES = new Set([
  "StepWaitForCompleted",
  "StepWaitForFailed",
  "StepWaitForPending",
  "StepExecuteCompleted",
  "StepExecuteFailed",
  "StepExecutePending",
]);

/** The current run of a Flow as a canvas overlay: its history, plus live state while it runs. */
export async function loadCurrentRun(
  client: FlowApiClient,
  flowId: string,
  signal?: AbortSignal,
): Promise<{
  summary: FlowSummary;
  overlay: RunOverlay;
}> {
  const summary = await getFlowSummary(client, flowId, "", signal);
  const { events } = await loadRunHistory(client, flowId, summary.runId, 0, signal);
  let activeSteps: ActiveStepExecution[] = [];
  if (isOpenFlowStatusCode(summary.flowStatusCode)) {
    try {
      activeSteps = (await getFlowState(client, flowId, summary.runId, signal)).activeStepExecutions;
    } catch {
      // Live state is an enrichment; the recorded history still draws the run.
    }
  }
  return { summary, overlay: overlayFromHistory(events, activeSteps) };
}

/** Step executions in the order Dex started them: the order history and then live state first name them. */
function overlayFromHistory(events: FlowHistoryEvent[], activeSteps: ActiveStepExecution[]): RunOverlay {
  const executions = new Map<string, StepExecution>();
  const newExecution = (
    stepExecutionId: string,
    stepType: string,
    fromStepExecutionId: string,
    startedAt?: number,
  ): StepExecution => {
    const execution: StepExecution = {
      stepExecutionId,
      stepType: stepType || stepExecutionId,
      fromStepExecutionId: fromStepExecutionId || undefined,
      waitFor: null,
      execute: { status: "notStarted" },
      attempts: 1,
      ...(startedAt === undefined ? {} : { startedAt }),
    };
    executions.set(stepExecutionId, execution);
    return execution;
  };

  for (const event of events) {
    if (!STEP_EVENT_TYPES.has(event.type)) continue;
    const context = asRecord(event.payload.context);
    const stepExecutionId = stringField(context.stepExecutionId);
    if (!stepExecutionId) continue;
    const execution =
      executions.get(stepExecutionId) ??
      newExecution(
        stepExecutionId,
        stringField(context.stepType),
        stringField(context.fromStepExecutionId),
        epochMs(context.startedTime) ?? epochMs(event.eventTime),
      );
    applyEvent(execution, event);
  }

  for (const active of activeSteps) {
    const execution =
      executions.get(active.stepExecutionId) ??
      newExecution(active.stepExecutionId, active.stepType, active.fromStepExecutionId);
    applyActive(execution, active);
  }

  return { executions: [...executions.values()], now: Date.now() };
}

function applyEvent(execution: StepExecution, event: FlowHistoryEvent): void {
  const context = asRecord(event.payload.context);
  const failed = event.type.endsWith("Failed");
  const pending = event.type.endsWith("Pending");
  const status: PhaseStatus = failed
    ? "failed"
    : pending
      ? event.type.startsWith("StepWaitFor")
        ? "pending"
        : "running"
      : "completed";
  if (event.type.startsWith("StepWaitFor")) {
    execution.waitFor = { status, conditions: conditionsFromEvent(event) };
  }
  if (event.type.startsWith("StepExecute")) {
    execution.execute = { status };
    const decision = asRecord(asRecord(event.payload.output).stepDecision);
    const decisionType = decisionKindFromStepDecision(decision);
    if (decisionType) execution.decisionType = decisionType;
    const next = nextStepTypes(decision);
    if (next.length > 0) execution.nextStepTypes = next;
    if (failed) {
      execution.lastFailure = failureMessage(event.payload.output) ?? execution.lastFailure;
    }
  }
  const attempt = Math.max(
    numberField(context.finalAttempt),
    numberField(asRecord(context.lastFailureInfo).attempt),
    numberField(asRecord(asRecord(event.payload.output).failure).attempt),
  );
  if (attempt > 0) execution.attempts = Math.max(execution.attempts, attempt);
  const started = epochMs(context.startedTime);
  if (started !== undefined) execution.startedAt = started;
}

function applyActive(execution: StepExecution, active: ActiveStepExecution): void {
  if (active.phase === "Waiting") {
    execution.waitFor = { status: "waiting", conditions: conditionsFromWaiting(active.waitingCondition) };
  } else if (execution.execute.status === "notStarted") {
    execution.execute = { status: "running" };
  }
  if (active.lastFailureInfo) {
    execution.lastFailure = failureMessage(active.lastFailureInfo) ?? execution.lastFailure;
    const attempt = numberField(asRecord(active.lastFailureInfo).attempt);
    if (attempt > 0) execution.attempts = Math.max(execution.attempts, attempt);
  }
}

function conditionsFromEvent(event: FlowHistoryEvent): RunCondition[] {
  const fromOutput = conditionsFromWaiting(asRecord(event.payload.output).waitForCondition);
  if (fromOutput.length > 0) return fromOutput;
  if (event.type === "StepWaitForPending") {
    return [{ kind: "unknown", label: "waiting", satisfied: false }];
  }
  return [];
}

function conditionsFromWaiting(value: unknown): RunCondition[] {
  const waiting = asRecord(value);
  return [
    ...conditionRows(waiting.channelConditions, "channel"),
    ...conditionRows(waiting.timerConditions, "timer"),
    ...conditionRows(waiting.subFlowConditions, "subflow"),
  ];
}

function conditionRows(value: unknown, kind: FlowDefinitionConditionKind): RunCondition[] {
  if (!Array.isArray(value)) return [];
  return value.map((entry) => {
    const data = asRecord(entry);
    return {
      kind,
      label:
        stringField(data.channelName) ||
        stringField(data.timerId) ||
        stringField(data.subFlowId) ||
        stringField(data.label) ||
        kind,
      satisfied: data.satisfied === true || data.completed === true,
    };
  });
}

function nextStepTypes(decision: Record<string, unknown>): string[] {
  if (!Array.isArray(decision.nextSteps)) return [];
  return decision.nextSteps.map((step) => stringField(asRecord(step).stepType)).filter(Boolean);
}

/** The wire StepDecision has no `type`; derive the verb from nextSteps and closeDecision. */
function decisionKindFromStepDecision(decision: Record<string, unknown>): string {
  const next = nextStepTypes(decision);
  const closeVerb = closeDecisionVerb(asRecord(decision.closeDecision).closeDecisionType);
  if (next.length > 0 && closeVerb) return `goTo · ${closeVerb}`;
  if (next.length > 0) return "goTo";
  return closeVerb;
}

function failureMessage(value: unknown): string | undefined {
  const failure = asRecord(asRecord(value).failure);
  const direct = asRecord(value);
  return stringField(failure.message) || stringField(direct.message) || undefined;
}

function epochMs(value: unknown): number | undefined {
  if (typeof value !== "string" || value === "") return undefined;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? undefined : parsed;
}
