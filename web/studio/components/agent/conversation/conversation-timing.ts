// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// When a turn's work ran: message and work intervals, waits, and the turn's measurement and live clock.
import type { ActivityStreamEvent, SequencedMessage } from "@/lib/session/agent-types";
import type {
  ConversationPendingWait,
  ConversationTurn,
  ConversationWorkItem,
  Interval,
  LiveTurnBoundary,
} from "./conversation-presentation";

/** Each wait's start activity and the activities that resolve it. */
const WAIT_ACTIVITY_PAIRS = [
  ["tool_approval_requested", "tool_approval_resolved"],
  ["user_input_requested", "user_input_answered", "user_input_cancelled"],
  ["timer_started", "timer_resolved"],
] as const;

/** The model and tool calls of a work log, for its counts and status summary. */
export function workLogCalls(operations: readonly ConversationWorkItem[]) {
  return {
    models: operations.flatMap((operation) => (operation.kind === "model" ? [operation.item] : [])),
    tools: operations.flatMap((operation) => (operation.kind === "tool" ? operation.group.calls : [])),
  };
}

/**
 * Hides tool results and tool-only assistant messages from the transcript and
 * measures the turn: its span, its union of work intervals and its waits. A
 * turn with running work or live clocks gets a clock for later ticks.
 */
export function measureTurn(turn: ConversationTurn, pendingWaits: readonly ConversationPendingWait[], nowMs: number) {
  const callIds = new Set(turn.messages.flatMap((entry) => entry.message.toolCalls.map((call) => call.id)));
  turn.messages = turn.messages.filter(
    (entry) =>
      entry.message.role !== "tool" &&
      !(entry.message.role === "assistant" && !entry.message.content && entry.message.toolCalls.length > 0),
  );
  const times = messageTimes(turn);
  const start = times[0] ?? null;
  const waits = pendingWaits.filter((wait) => !wait.callId || callIds.has(wait.callId));
  const { models, tools } = workLogCalls(turn.operations);
  const running =
    models.some((model) => model.status === "running") ||
    tools.some((call) => call.status === "running" || call.status === "waiting") ||
    waits.length > 0;
  const end = running ? nowMs : times.length > 0 ? Math.max(...times) : null;
  turn.durationMs = start === null || end === null || end < start ? null : end - start;
  turn.workDurationMs = workDuration(turn.operations);
  const waitStarts = waits.map((wait) => timestamp(wait.startedAt));
  const completedWaits = completedWaitDurations(turn.activities);
  turn.waitDurationMs =
    waits.length === 0 && completedWaits.length === 0 ? null : liveWaitMs(waitStarts, nowMs) + sum(completedWaits);
  const hasLiveClock = [...models, ...tools].some((item) => item.clockStartedAt !== null);
  if (running || hasLiveClock) {
    turn.clock = { observedAt: nowMs, startedAt: start, running, waitStarts, completedWaitMs: sum(completedWaits) };
  }
}

/** The durations of the turn's waits that have both a start and a resolving activity. */
function completedWaitDurations(activities: readonly ActivityStreamEvent[]): number[] {
  const durations: number[] = [];
  for (const [startedKind, ...resolvedKinds] of WAIT_ACTIVITY_PAIRS) {
    for (const started of activities.filter((event) => event.value.kind === startedKind)) {
      const startedAt = timestamp(started.createdAt);
      const resolved = activities.find(
        (event) =>
          resolvedKinds.includes(event.value.kind as never) &&
          timestamp(event.createdAt) !== null &&
          (timestamp(event.createdAt) ?? 0) >= (startedAt ?? Number.POSITIVE_INFINITY) &&
          (started.value.callId === null || event.value.callId === started.value.callId),
      );
      const resolvedAt = timestamp(resolved?.createdAt);
      if (startedAt !== null && resolvedAt !== null && resolvedAt >= startedAt) durations.push(resolvedAt - startedAt);
    }
  }
  return durations;
}

/** The time the work log's model and tool calls ran, counting overlapping parallel work once. */
export function workDuration(operations: readonly ConversationWorkItem[]): number | null {
  const { models, tools } = workLogCalls(operations);
  return unionDuration([...models, ...tools].map((item) => item.interval));
}

/** How long the still-pending waits have waited so far. */
export function liveWaitMs(waitStarts: readonly (number | null)[], nowMs: number): number {
  return sum(waitStarts.map((start) => (start === null || nowMs < start ? 0 : nowMs - start)));
}

function unionDuration(intervals: readonly (Interval | null)[]): number | null {
  const valid = intervals
    .filter((value): value is Interval => value !== null && value[1] >= value[0])
    .map(([start, end]) => [start, end] as [number, number])
    .sort((left, right) => left[0] - right[0]);
  if (valid.length === 0) return null;
  let total = 0;
  let [start, end] = valid[0];
  for (const [nextStart, nextEnd] of valid.slice(1)) {
    if (nextStart <= end) end = Math.max(end, nextEnd);
    else {
      total += end - start;
      start = nextStart;
      end = nextEnd;
    }
  }
  return total + end - start;
}

export function activityBelongsToLiveTurn(
  boundary: LiveTurnBoundary | undefined,
  createdAt: string | undefined,
): boolean {
  const activityTime = timestamp(createdAt);
  return boundary !== undefined && activityTime !== null && activityTime >= boundary.observedThrough;
}

export function messageInterval(entry: SequencedMessage): Interval | null {
  const start = timestamp(entry.message.startedAt);
  const end = timestamp(entry.message.createdAt);
  return start === null || end === null || end < start ? null : [start, end];
}

/** The parseable creation times of the turn's messages. */
export function messageTimes(turn: ConversationTurn): number[] {
  return turn.messages.map((entry) => timestamp(entry.message.createdAt)).filter((value) => value !== null);
}

export function timestamp(value: string | null | undefined): number | null {
  const parsed = Date.parse(value ?? "");
  return Number.isFinite(parsed) ? parsed : null;
}

export function durationOf(interval: Interval | null): number | null {
  return interval ? interval[1] - interval[0] : null;
}

function sum(values: readonly number[]): number {
  return values.reduce((total, value) => total + value, 0);
}
