// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowHistoryEvent } from "@/lib/api/flow/flow-types";
import { stringField } from "@/lib/shared/json";
import {
  START_NODE_ID,
  STEP_METHOD_EVENT_TYPES,
  isContinuedStart,
  isRpcSourceEvent,
  nextSteps,
  rpcSourceName,
  stepContext,
  stepExecutionId,
  stepMethod,
} from "@/lib/api/flow/flow-history";

/**
 * The arrows the timeline draws for the selected event: where its Step execution came from
 * (lineage) and how long its WaitFor condition took to let Execute run (condition wait).
 */
export interface TimelineLink {
  kind: "lineage" | "condition-wait";
  stepExecutionId: string;
  fromEventId: number;
  toEventId: number;
  label: string;
  elapsedDurationMs: number | null;
  /** The rail column, so overlapping arrows do not share one. */
  lane: number;
}

type UnplacedLink = Omit<TimelineLink, "lane">;

export function newestTimelineEvents(events: FlowHistoryEvent[]): FlowHistoryEvent[] {
  return [...events].sort((left, right) => right.eventId - left.eventId);
}

/** The event's 1-based position in chronological order. */
export function displayEventNumber(events: FlowHistoryEvent[], selectedEvent: FlowHistoryEvent): number {
  return chronological(events).findIndex((event) => event.eventId === selectedEvent.eventId) + 1;
}

/** Every event's 1-based position in chronological order, by event ID. */
export function displayEventNumbers(events: FlowHistoryEvent[]): Map<number, number> {
  return new Map(chronological(events).map((event, index) => [event.eventId, index + 1]));
}

/** The lineage arrow into the selected event and the condition-wait arrows that touch it. */
export function buildSelectedTimelineLinks(
  events: FlowHistoryEvent[],
  selectedEventId: number | undefined,
): TimelineLink[] {
  if (selectedEventId === undefined) return [];
  const selectedLinks = [...lineageLinks(events), ...conditionWaitLinks(events)].filter((link) =>
    link.kind === "lineage"
      ? link.toEventId === selectedEventId
      : link.fromEventId === selectedEventId || link.toEventId === selectedEventId,
  );
  return assignLanes(selectedLinks);
}

function chronological(events: FlowHistoryEvent[]): FlowHistoryEvent[] {
  return [...events].sort((left, right) => left.eventId - right.eventId);
}

function conditionWaitLinks(events: FlowHistoryEvent[]): UnplacedLink[] {
  const openWaitFor = new Map<string, FlowHistoryEvent>();
  const links: UnplacedLink[] = [];
  for (const event of chronological(events)) {
    const executionId = stepExecutionId(event);
    if (!executionId) continue;
    if (event.type === "StepWaitForCompleted") {
      openWaitFor.set(executionId, event);
      continue;
    }
    if (stepMethod(event) !== "execute") continue;
    const waitForEvent = openWaitFor.get(executionId);
    if (!waitForEvent) continue;
    links.push({
      kind: "condition-wait",
      stepExecutionId: executionId,
      fromEventId: waitForEvent.eventId,
      toEventId: event.eventId,
      label: `${executionId}: WaitForCondition started to Execute`,
      elapsedDurationMs: elapsedMilliseconds(waitForEvent.eventTime, executeStartedTime(event)),
    });
    openWaitFor.delete(executionId);
  }
  return links;
}

function lineageLinks(events: FlowHistoryEvent[]): UnplacedLink[] {
  const ordered = chronological(events);
  const firstStepEvents = new Map<string, FlowHistoryEvent>();
  for (const event of ordered) {
    if (!STEP_METHOD_EVENT_TYPES.has(event.type)) continue;
    const executionId = stepExecutionId(event);
    if (executionId && !firstStepEvents.has(executionId)) firstStepEvents.set(executionId, event);
  }

  const links: UnplacedLink[] = [];
  for (const [executionId, firstEvent] of firstStepEvents) {
    const source = findLineageSourceEvent(ordered, firstEvent);
    if (!source || source.eventId >= firstEvent.eventId) continue;
    links.push({
      kind: "lineage",
      stepExecutionId: executionId,
      fromEventId: source.eventId,
      toEventId: firstEvent.eventId,
      label: `${lineageSourceLabel(source)} to ${executionId} first event`,
      elapsedDurationMs: null,
    });
  }
  return links;
}

/**
 * The event that scheduled a Step execution: the Flow start, an RPC, or the decision of the
 * Step execution it came from. A Step carried over from an earlier run links to the continued start.
 */
function findLineageSourceEvent(
  chronologicalEvents: FlowHistoryEvent[],
  target: FlowHistoryEvent,
): FlowHistoryEvent | undefined {
  const priorEvents = chronologicalEvents.filter((event) => event.eventId < target.eventId);
  const sourceExecutionId = stringField(stepContext(target).fromStepExecutionId);
  const flowStart = priorEvents.findLast((event) => event.type === "FlowStartedOrContinued");
  if (sourceExecutionId === START_NODE_ID) return flowStart;
  if (rpcSourceName(sourceExecutionId) !== "") {
    return priorEvents.findLast((event) => isRpcSourceEvent(event, sourceExecutionId));
  }

  const sourceStepEvents = priorEvents.filter((event) => stepExecutionId(event) === sourceExecutionId);
  const targetStepType = stringField(stepContext(target).stepType);
  const exactSource = sourceStepEvents.findLast((event) => producesStepType(event, targetStepType));
  if (exactSource) return exactSource;
  if (sourceStepEvents.length > 0) return sourceStepEvents.at(-1);
  if (flowStart && isContinuedStart(flowStart)) return flowStart;
  return undefined;
}

function producesStepType(event: FlowHistoryEvent, targetStepType: string): boolean {
  return nextSteps(event).some((movement) => movement.stepType === targetStepType);
}

function lineageSourceLabel(event: FlowHistoryEvent): string {
  if (event.type === "FlowStartedOrContinued") {
    return isContinuedStart(event) ? "Flow continued" : "Flow start";
  }
  if (event.type === "RpcExecutionCompleted") {
    return `RPC ${String(event.payload.rpcName || "")}`.trim();
  }
  if (event.type === "StepExecuteFailed") return `${stepExecutionId(event)} failure recovery`;
  return `${stepExecutionId(event)} decision`;
}

function executeStartedTime(event: FlowHistoryEvent): string | null {
  return stringField(stepContext(event).startedTime) || event.eventTime;
}

function elapsedMilliseconds(start: string | null, end: string | null): number | null {
  if (!start || !end) return null;
  const startMs = Date.parse(start);
  const endMs = Date.parse(end);
  if (!Number.isFinite(startMs) || !Number.isFinite(endMs)) return null;
  return Math.max(0, endMs - startMs);
}

/** Puts each arrow in the first lane that is free by the time it starts. */
function assignLanes(links: UnplacedLink[]): TimelineLink[] {
  const laneEndEventIds: number[] = [];
  return [...links]
    .sort((left, right) => left.fromEventId - right.fromEventId || left.toEventId - right.toEventId)
    .map((link) => {
      let lane = laneEndEventIds.findIndex((endEventId) => endEventId <= link.fromEventId);
      if (lane < 0) lane = laneEndEventIds.length;
      laneEndEventIds[lane] = link.toEventId;
      return { ...link, lane };
    });
}
