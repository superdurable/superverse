// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ActiveStepExecution, FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { isHandlerTimeoutPolicy, subFlowReusePolicyLabel, subFlowStatusName } from "@/lib/flow/enum-labels";
import { asRecord, isRecord, stringField, type UnknownRecord } from "@/lib/shared/json";
import {
  FLOW_TIMEOUT_HANDLER_STEP_TYPE,
  START_NODE_ID,
  STEP_METHOD_EVENT_TYPES,
  generatedSubFlowID,
  nextSteps,
  previousRunId,
  rpcSourceName,
  stepContext,
  stepDecision,
  stepExecutionId,
  stepMethod,
} from "@/lib/api/flow/flow-history";

/** One node of the execution graph Diagnostics reconstructs from history and live state. */
export interface StepGraphNode {
  id: string;
  label: string;
  kind: "source" | "step" | "subflow";
  status: "Source" | "Active" | "Waiting" | "Pending" | "Completed" | "Failed" | "Canceled";
  previousRunId?: string;
  stepType?: string;
  fromStepExecutionId?: string;
  movement?: Record<string, unknown>;
  isPlanned?: boolean;
  waitFor?: FlowHistoryEvent;
  execute?: FlowHistoryEvent;
  pendingWaitFor?: FlowHistoryEvent;
  pendingExecute?: FlowHistoryEvent;
  active?: ActiveStepExecution;
  parentStepId?: string;
  flowId?: string;
  subFlowStatus?: string;
  reusePolicy?: string;
}

export interface StepGraphEdge {
  id: string;
  source: string;
  target: string;
}

const cancellableStatuses = new Set<StepGraphNode["status"]>(["Active", "Waiting", "Pending"]);

export function buildStepGraph(
  events: FlowHistoryEvent[],
  activeSteps: ActiveStepExecution[] = [],
  parentFlowID = "",
): { nodes: StepGraphNode[]; edges: StepGraphEdge[] } {
  const nodes = new Map<string, StepGraphNode>();
  const plannedNodesByLineage = new Map<string, string[]>();
  const started = events.find((event) => event.type === "FlowStartedOrContinued");
  nodes.set(START_NODE_ID, {
    id: START_NODE_ID,
    label: "Flow start",
    kind: "source",
    status: "Source",
    previousRunId: started ? previousRunId(started) : "",
  });

  for (const event of events) {
    if (STEP_METHOD_EVENT_TYPES.has(event.type)) {
      addStepEvent(nodes, plannedNodesByLineage, event);
    }
    const decision = stepDecision(event);
    if (!decision) continue;
    cancelMatchingNodes(nodes, decision, stringField(stepContext(event).fromStepExecutionId));
    addPlannedNodes(nodes, plannedNodesByLineage, event);
  }

  for (const active of activeSteps) {
    const existing =
      nodes.get(active.stepExecutionId) ??
      takePlannedNode(nodes, plannedNodesByLineage, active.fromStepExecutionId || START_NODE_ID, active.stepType);
    if (existing?.isPlanned) nodes.delete(existing.id);
    const isCanceled = existing?.status === "Canceled";
    nodes.set(active.stepExecutionId, {
      id: active.stepExecutionId,
      label: active.stepType || active.stepExecutionId,
      kind: "step",
      status: isCanceled ? "Canceled" : active.phase === "Waiting" ? "Waiting" : "Active",
      stepType: active.stepType,
      fromStepExecutionId: active.fromStepExecutionId || START_NODE_ID,
      movement: existing?.movement ?? active.movement,
      waitFor: existing?.waitFor,
      execute: existing?.execute,
      pendingWaitFor: existing?.pendingWaitFor,
      pendingExecute: existing?.pendingExecute,
      active: isCanceled ? undefined : active,
    });
  }

  for (const stepNode of [...nodes.values()]) {
    if (stepNode.kind !== "step") continue;
    addSubFlowNodes(nodes, stepNode, parentFlowID);
  }

  for (const node of [...nodes.values()]) {
    const source = node.fromStepExecutionId ?? "";
    const rpcName = rpcSourceName(source);
    if (rpcName === "" || nodes.has(source)) continue;
    nodes.set(source, { id: source, label: `RPC · ${rpcName}`, kind: "source", status: "Source" });
  }

  hideNonHandlerTimeoutNodes(nodes, events);

  const edges: StepGraphEdge[] = [];
  for (const node of nodes.values()) {
    if (node.kind === "subflow") {
      edges.push({
        id: `${node.parentStepId}->${node.id}`,
        source: node.parentStepId ?? START_NODE_ID,
        target: node.id,
      });
      continue;
    }
    if (node.kind !== "step") continue;
    const requestedSource = node.fromStepExecutionId || START_NODE_ID;
    const source = nodes.has(requestedSource) ? requestedSource : START_NODE_ID;
    edges.push({ id: `${source}->${node.id}`, source, target: node.id });
  }

  return { nodes: [...nodes.values()], edges };
}

export function stepGraphSelection(
  nodes: StepGraphNode[],
  edges: StepGraphEdge[],
  selectedEvent: FlowHistoryEvent | null,
): {
  selectedStepExecutionID: string;
  previousStepExecutionIDs: Set<string>;
  nextStepExecutionIDs: Set<string>;
  incomingEdgeIDs: Set<string>;
  outgoingEdgeIDs: Set<string>;
} {
  const selectedStepExecutionID = selectedEvent ? stepExecutionId(selectedEvent) : "";
  const stepExecutionIDs = new Set(
    nodes.filter((node) => node.kind === "step" && !node.isPlanned).map((node) => node.id),
  );
  const previousStepExecutionIDs = new Set<string>();
  const nextStepExecutionIDs = new Set<string>();
  const incomingEdgeIDs = new Set<string>();
  const outgoingEdgeIDs = new Set<string>();
  if (!stepExecutionIDs.has(selectedStepExecutionID)) {
    return {
      selectedStepExecutionID: "",
      previousStepExecutionIDs,
      nextStepExecutionIDs,
      incomingEdgeIDs,
      outgoingEdgeIDs,
    };
  }

  for (const edge of edges) {
    if (edge.target === selectedStepExecutionID && stepExecutionIDs.has(edge.source)) {
      previousStepExecutionIDs.add(edge.source);
      incomingEdgeIDs.add(edge.id);
    }
    if (edge.source === selectedStepExecutionID && stepExecutionIDs.has(edge.target)) {
      nextStepExecutionIDs.add(edge.target);
      outgoingEdgeIDs.add(edge.id);
    }
  }
  return {
    selectedStepExecutionID,
    previousStepExecutionIDs,
    nextStepExecutionIDs,
    incomingEdgeIDs,
    outgoingEdgeIDs,
  };
}

function hideNonHandlerTimeoutNodes(nodes: Map<string, StepGraphNode>, events: FlowHistoryEvent[]): void {
  const started = events.find((event) => event.type === "FlowStartedOrContinued");
  if (isHandlerTimeoutPolicy(started?.payload.flowTimeoutPolicy)) return;
  for (const [id, node] of nodes) {
    if (node.stepType === FLOW_TIMEOUT_HANDLER_STEP_TYPE) {
      nodes.delete(id);
    }
  }
}

function addStepEvent(
  nodes: Map<string, StepGraphNode>,
  plannedNodesByLineage: Map<string, string[]>,
  event: FlowHistoryEvent,
): void {
  const info = stepContext(event);
  const id = stringField(info.stepExecutionId);
  if (!id) return;
  const stepType = stringField(info.stepType);
  const fromStepExecutionID = stringField(info.fromStepExecutionId) || START_NODE_ID;
  const existing = nodes.get(id) ?? takePlannedNode(nodes, plannedNodesByLineage, fromStepExecutionID, stepType);
  if (existing?.isPlanned) nodes.delete(existing.id);
  const failed = event.type.endsWith("Failed");
  const pending = event.type.endsWith("Pending");
  const waitFor = stepMethod(event) === "wait-for" && !pending ? event : existing?.waitFor;
  const execute = stepMethod(event) === "execute" && !pending ? event : existing?.execute;
  const status =
    existing?.status === "Canceled"
      ? "Canceled"
      : pending
        ? "Pending"
        : failed
          ? "Failed"
          : execute
            ? "Completed"
            : "Waiting";
  nodes.set(id, {
    id,
    label: stepType || id,
    kind: "step",
    status,
    stepType,
    fromStepExecutionId: fromStepExecutionID,
    movement: existing?.movement,
    waitFor,
    execute,
    pendingWaitFor: event.type === "StepWaitForPending" ? event : existing?.pendingWaitFor,
    pendingExecute: event.type === "StepExecutePending" ? event : existing?.pendingExecute,
  });
}

function addPlannedNodes(
  nodes: Map<string, StepGraphNode>,
  plannedNodesByLineage: Map<string, string[]>,
  event: FlowHistoryEvent,
): void {
  const producerID = stepExecutionId(event);
  nextSteps(event).forEach((movement, index) => {
    const stepType = stringField(movement.stepType);
    if (!stepType) return;
    const fromStepExecutionID =
      stringField(movement.fromStepExecutionIdInternalOnly) ||
      stringField(movement.fromStepExecutionId) ||
      producerID ||
      START_NODE_ID;
    const id = `__planned:${event.eventId}:${index}`;
    nodes.set(id, {
      id,
      label: stepType,
      kind: "step",
      status: "Pending",
      stepType,
      fromStepExecutionId: fromStepExecutionID,
      movement,
      isPlanned: true,
    });
    const key = lineageKey(fromStepExecutionID, stepType);
    plannedNodesByLineage.set(key, [...(plannedNodesByLineage.get(key) ?? []), id]);
  });
}

function cancelMatchingNodes(
  nodes: Map<string, StepGraphNode>,
  decision: UnknownRecord,
  producerFromStepExecutionID: string,
): void {
  const globalStepTypes = stringSet(decision.cancelStepTypes);
  const siblingStepTypes = stringSet(decision.cancelSiblingStepTypes);
  if (globalStepTypes.size === 0 && siblingStepTypes.size === 0) return;
  for (const [id, node] of nodes) {
    if (node.kind !== "step" || !cancellableStatuses.has(node.status)) continue;
    const isGlobalMatch = globalStepTypes.has(node.stepType ?? "");
    const isSiblingMatch =
      siblingStepTypes.has(node.stepType ?? "") && node.fromStepExecutionId === producerFromStepExecutionID;
    if (isGlobalMatch || isSiblingMatch) {
      nodes.set(id, { ...node, status: "Canceled", active: undefined });
    }
  }
}

function takePlannedNode(
  nodes: Map<string, StepGraphNode>,
  plannedNodesByLineage: Map<string, string[]>,
  fromStepExecutionID: string,
  stepType: string,
): StepGraphNode | undefined {
  const key = lineageKey(fromStepExecutionID, stepType);
  const ids = plannedNodesByLineage.get(key) ?? [];
  const index = ids.findIndex((id) => {
    const node = nodes.get(id);
    return node?.isPlanned === true && node.status !== "Canceled";
  });
  if (index < 0) return undefined;
  const [id] = ids.splice(index, 1);
  if (ids.length === 0) plannedNodesByLineage.delete(key);
  return nodes.get(id);
}

function lineageKey(fromStepExecutionID: string, stepType: string): string {
  return `${fromStepExecutionID}\u0000${stepType}`;
}

function stringSet(value: unknown): Set<string> {
  if (!Array.isArray(value)) return new Set();
  return new Set(value.filter((entry): entry is string => typeof entry === "string"));
}

/** The WaitFor condition a Step node waited on: its recorded WaitFor output, else the live state. */
export function waitingConditionOf(node: StepGraphNode): UnknownRecord {
  const recorded = asRecord(node.waitFor?.payload.output).waitForCondition;
  return isRecord(recorded) ? recorded : (node.active?.waitingCondition ?? {});
}

function addSubFlowNodes(nodes: Map<string, StepGraphNode>, stepNode: StepGraphNode, parentFlowID: string): void {
  const waitingCondition = waitingConditionOf(stepNode);
  const conditions = Array.isArray(waitingCondition.subFlowConditions) ? waitingCondition.subFlowConditions : [];
  // Execute input carries the results once the WaitFor closed; until then the active Step
  // reports the SubFlows completed so far, keyed by position.
  const results = asRecord(asRecord(stepNode.execute?.payload.input).conditionResults).subFlowResults;
  const activeResults = asRecord(stepNode.active?.completedConditions?.completedSubFlowResults);
  conditions.forEach((condition, index) => {
    if (!isRecord(condition)) return;
    const closedResult = Array.isArray(results) ? results[index] : undefined;
    const result = isRecord(closedResult) ? closedResult : asRecord(activeResults[String(index)]);
    const flowId = generatedSubFlowID(parentFlowID, stepNode.id, index);
    if (!flowId) return;
    const status = subFlowStatusName(result.flowStatus ?? 1);
    const options = asRecord(condition.options);
    const id = `__subflow:${stepNode.id}:${index}`;
    nodes.set(id, {
      id,
      label: flowId,
      kind: "subflow",
      status: ["FAILED", "CANCELED", "SERVER_SIDE_TIMEOUT_INTERNAL_ONLY", "TERMINATED"].includes(status)
        ? "Failed"
        : status === "RUNNING"
          ? "Waiting"
          : "Completed",
      parentStepId: stepNode.id,
      flowId,
      subFlowStatus: status,
      reusePolicy: subFlowReusePolicyLabel(options.reusePolicy),
    });
  });
}
