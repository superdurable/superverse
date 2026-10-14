// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowHistoryEvent } from "./flow-types";
import { asRecord, asRecordArray, hasFields, isRecord, stringField, type UnknownRecord } from "@/lib/shared/json";

/** Readers for the history payload fields that the timeline, graph and event views share. */

/** The Step source a Flow start schedules from. */
export const START_NODE_ID = "__start__";
/** The StepType Dex runs a Flow timeout handler as. */
export const FLOW_TIMEOUT_HANDLER_STEP_TYPE = "sys:timeout_handler";
/** A Step source an RPC scheduled: `__rpc/<rpc name>`. */
const RPC_SOURCE_PREFIX = "__rpc/";

export const STEP_METHOD_EVENT_TYPES: ReadonlySet<FlowHistoryEvent["type"]> = new Set([
  "StepWaitForCompleted",
  "StepWaitForFailed",
  "StepWaitForPending",
  "StepExecuteCompleted",
  "StepExecuteFailed",
  "StepExecutePending",
]);

/** The FlowID Dex generates for the SubFlow at `index` of a Step execution's WaitFor condition. */
export function generatedSubFlowID(parentFlowID: string, stepExecutionID: string, index: number): string {
  if (!parentFlowID || !stepExecutionID) return "";
  return `SubFlow:${parentFlowID}-${stepExecutionID}-${index}`;
}

/** Which Step method an event belongs to, or null for any other event. */
export function stepMethod(event: FlowHistoryEvent): "wait-for" | "execute" | null {
  if (event.type.startsWith("StepWaitFor")) return "wait-for";
  if (event.type.startsWith("StepExecute")) return "execute";
  return null;
}

/** The Step method context of a WaitFor or Execute event; empty for any other event. */
export function stepContext(event: FlowHistoryEvent): UnknownRecord {
  return asRecord(event.payload.context);
}

export function stepExecutionId(event: FlowHistoryEvent): string {
  return stringField(stepContext(event).stepExecutionId);
}

/** A start event that carries a continued start from an earlier run rather than a fresh start. */
export function isContinuedStart(event: FlowHistoryEvent): boolean {
  return event.type === "FlowStartedOrContinued" && hasFields(asRecord(event.payload.continuedStart));
}

/** The run this run continued from or was time travelled from; empty for a fresh start. */
export function previousRunId(event: FlowHistoryEvent): string {
  if (event.type === "TimeTravelFork") return stringField(event.payload.previousRunId);
  if (event.type !== "FlowStartedOrContinued") return "";
  return stringField(asRecord(event.payload.continuedStart).previousRunId);
}

/** The RPC name of a `__rpc/<name>` Step source, or '' for any other source. */
export function rpcSourceName(fromStepExecutionId: string): string {
  return fromStepExecutionId.startsWith(RPC_SOURCE_PREFIX) ? fromStepExecutionId.slice(RPC_SOURCE_PREFIX.length) : "";
}

/** Whether this is the RPC completion that a `__rpc/<name>` Step source names. */
export function isRpcSourceEvent(event: FlowHistoryEvent, fromStepExecutionId: string): boolean {
  return (
    fromStepExecutionId.startsWith(RPC_SOURCE_PREFIX) &&
    event.type === "RpcExecutionCompleted" &&
    event.payload.rpcName === rpcSourceName(fromStepExecutionId)
  );
}

/** The Step decision an Execute or RPC made; undefined for any other event. */
export function stepDecision(event: FlowHistoryEvent): UnknownRecord | undefined {
  const container =
    event.type === "StepExecuteCompleted"
      ? asRecord(event.payload.output)
      : event.type === "RpcExecutionCompleted"
        ? event.payload
        : {};
  return isRecord(container.stepDecision) ? container.stepDecision : undefined;
}

/** The Step movements a Step decision schedules; empty for an event that made no decision. */
export function nextSteps(event: FlowHistoryEvent): UnknownRecord[] {
  return asRecordArray(stepDecision(event)?.nextSteps);
}

/** The Flow starts and continuations recorded before this event, newest first. */
export function priorStartEvents(event: FlowHistoryEvent, history: FlowHistoryEvent[]): FlowHistoryEvent[] {
  return history
    .filter((candidate) => candidate.type === "FlowStartedOrContinued" && candidate.eventId < event.eventId)
    .sort((left, right) => right.eventId - left.eventId);
}
