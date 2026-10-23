// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type {
  ActivityStreamEvent,
  AgentMessage,
  AgentToolCall,
  LiveTextEntry,
  SequencedMessage,
} from "@/lib/session/agent-types";
import {
  activityBelongsToLiveTurn,
  liveWaitMs,
  measureTurn,
  messageTimes,
  timestamp,
  workDuration,
} from "./conversation-timing";
import { MODEL_LIFECYCLE, appendDurableWork, appendUnanchoredModels } from "./conversation-work-items";

export type ConversationOperationStatus = "running" | "waiting" | "complete" | "failed" | "unknown";
export type ConversationStreamState = "streaming" | "complete";

export interface ConversationPendingWait {
  kind: "approval" | "question" | "timer";
  startedAt: string | null;
  callId?: string | null;
}

export type Interval = readonly [number, number];

export interface ConversationModelWorkItem {
  key: string;
  messageSequence: number | null;
  status: ConversationOperationStatus;
  reasoning: LiveTextEntry | null;
  durationMs: number | null;
  interval: Interval | null;
  clockStartedAt: number | null;
  summary: string;
  attemptCount: number | null;
  metrics: AgentMessage["modelCall"];
}

export interface ConversationToolWorkItem {
  call: AgentToolCall;
  /** What the call works on: the call's own target, else the one its live events carry; may be empty. */
  target: string;
  status: ConversationOperationStatus;
  /** The failure code of a failed call, or a business qualifier such as `pushed` or `blocked`; may be empty. */
  resultCode: string;
  durationMs: number | null;
  attemptCount: number | null;
  interval: Interval | null;
  clockStartedAt: number | null;
}

export interface ConversationToolGroup {
  key: string;
  name: string;
  calls: ConversationToolWorkItem[];
  repeatIndex: number | null;
  repeatTotal: number | null;
}

export type ConversationWorkItem =
  | { kind: "model"; key: string; item: ConversationModelWorkItem }
  | { kind: "tool"; key: string; group: ConversationToolGroup };

/** A request_user_input call of the history: its first question (the call's target) and how it ended. */
export interface ConversationQuestionItem {
  key: string;
  callId: string;
  target: string;
  status: "pending" | "answered" | "cancelled" | "failed";
}

export interface ConversationTurn {
  key: string;
  messages: SequencedMessage[];
  /** The turn's work log in display order: each model call followed by the tool groups it requested. */
  operations: ConversationWorkItem[];
  questions: ConversationQuestionItem[];
  /** The model fallbacks of this turn, each told to the person as one line. */
  fallbacks: ActivityStreamEvent[];
  activities: ActivityStreamEvent[];
  liveActivities: ActivityStreamEvent[];
  historicalActivities: ActivityStreamEvent[];
  durationMs: number | null;
  workDurationMs: number | null;
  waitDurationMs: number | null;
  clock?: {
    observedAt: number;
    startedAt: number | null;
    running: boolean;
    waitStarts: readonly (number | null)[];
    completedWaitMs: number;
  };
}

export interface ConversationPresentation {
  turns: ConversationTurn[];
  earlierActivity: (ActivityStreamEvent | LiveTextEntry)[];
}

export interface ConversationPresentationInput {
  messages: readonly SequencedMessage[];
  reasoning: readonly LiveTextEntry[];
  activities: readonly ActivityStreamEvent[];
  pendingWaits: readonly ConversationPendingWait[];
  isModelRunning: boolean;
  isExecutionLive: boolean;
}

const LIVE_WORK_ACTIVITY = new Set([
  ...MODEL_LIFECYCLE,
  "model_fallback",
  "model_tool_call",
  "tool_progress",
  "tool_failed",
  "tool_completed",
]);
const HISTORICAL_ACTIVITY = new Set(["compacted", "compaction_failed", "snapshot_required"]);

/**
 * Groups the durable conversation into turns, one per user message, and lays
 * out each turn's work log from the messages and the activity Stream: model
 * calls, tool calls grouped by repeats, questions, and live durations.
 */
export function buildConversationPresentation(
  input: ConversationPresentationInput,
  nowMs = Date.now(),
): ConversationPresentation {
  const messages = [...input.messages].sort((left, right) => left.sequence - right.sequence);
  const activities = [...input.activities].sort(
    (left, right) => (timestamp(left.createdAt) ?? 0) - (timestamp(right.createdAt) ?? 0),
  );
  const { turns, sequenceToTurn } = groupDurableTurns(messages);
  const context: PresentationContext = {
    messages,
    turns,
    sequenceToTurn,
    ...indexToolCalls(turns),
    activity: indexActivities(activities),
    liveTurn: liveTurnForUnanchoredActivity(turns, input.isExecutionLive),
    reasoningBySource: new Map(input.reasoning.map((entry) => [entry.source, entry])),
    usedReasoning: new Set(),
    usedModelSources: new Set(),
    pendingWaitByCall: new Map(
      input.pendingWaits.flatMap((wait) => (wait.callId ? [[wait.callId, wait] as const] : [])),
    ),
    isModelRunning: input.isModelRunning,
    isExecutionLive: input.isExecutionLive,
    nowMs,
  };
  const earlierActivity = assignActivitiesToTurns(activities, context);
  for (const turn of turns) appendDurableWork(turn, context);
  earlierActivity.push(...appendUnanchoredModels(context));
  for (const reasoning of input.reasoning) {
    if (!context.usedReasoning.has(reasoning.source)) earlierActivity.push(reasoning);
  }
  for (const turn of turns) measureTurn(turn, input.pendingWaits, nowMs);
  return { turns, earlierActivity };
}

/** Update live durations only. Completed turns and message/tool parsing stay cached. */
export function advanceConversationTurn(turn: ConversationTurn, nowMs: number): ConversationTurn {
  const clock = turn.clock;
  if (!clock) return turn;
  const now = Math.max(clock.observedAt, nowMs);
  function advance<T extends ConversationModelWorkItem | ConversationToolWorkItem>(item: T): T {
    const start = item.clockStartedAt;
    if (start === null || now < start) return item;
    return { ...item, interval: [start, now] as const, durationMs: now - start };
  }
  const operations = turn.operations.map((operation): ConversationWorkItem =>
    operation.kind === "model"
      ? { ...operation, item: advance(operation.item) }
      : { ...operation, group: { ...operation.group, calls: operation.group.calls.map(advance) } },
  );
  return {
    ...turn,
    operations,
    durationMs:
      clock.running && clock.startedAt !== null && now >= clock.startedAt ? now - clock.startedAt : turn.durationMs,
    workDurationMs: workDuration(operations),
    waitDurationMs: turn.waitDurationMs === null ? null : clock.completedWaitMs + liveWaitMs(clock.waitStarts, now),
  };
}

export interface LiveTurnBoundary {
  turn: ConversationTurn;
  observedThrough: number;
}

interface ActivityIndex {
  byCall: Map<string, ActivityStreamEvent[]>;
  /** Model lifecycle activities, by the model call (source) that produced them. */
  bySource: Map<string, ActivityStreamEvent[]>;
  bySequence: Map<number, ActivityStreamEvent[]>;
}

export interface PresentationContext {
  messages: readonly SequencedMessage[];
  turns: readonly ConversationTurn[];
  sequenceToTurn: ReadonlyMap<number, ConversationTurn>;
  resultByCall: ReadonlyMap<string, SequencedMessage>;
  callToTurn: ReadonlyMap<string, ConversationTurn>;
  activity: ActivityIndex;
  liveTurn: LiveTurnBoundary | undefined;
  reasoningBySource: ReadonlyMap<string, LiveTextEntry>;
  usedReasoning: Set<string>;
  usedModelSources: Set<string>;
  pendingWaitByCall: ReadonlyMap<string, ConversationPendingWait>;
  isModelRunning: boolean;
  isExecutionLive: boolean;
  nowMs: number;
}

function groupDurableTurns(messages: readonly SequencedMessage[]) {
  const turns: ConversationTurn[] = [];
  const sequenceToTurn = new Map<number, ConversationTurn>();
  let current: ConversationTurn | null = null;
  for (const entry of messages) {
    // Only an idle notification opens a display turn. The durable consumption
    // flag keeps active-turn notifications with their work, including after reload.
    // Older entries have no flag and retain their existing display boundary.
    if (
      entry.message.role === "user" ||
      (entry.message.role === "notification" && !entry.message.notificationContinuesTurn) ||
      entry.message.executorChanged ||
      current === null
    ) {
      current = {
        key: `turn-${String(entry.sequence)}`,
        messages: [],
        operations: [],
        questions: [],
        fallbacks: [],
        activities: [],
        liveActivities: [],
        historicalActivities: [],
        durationMs: null,
        workDurationMs: null,
        waitDurationMs: null,
      };
      turns.push(current);
    }
    current.messages.push(entry);
    sequenceToTurn.set(entry.sequence, current);
  }
  return { turns, sequenceToTurn };
}

function indexToolCalls(turns: readonly ConversationTurn[]) {
  const resultByCall = new Map<string, SequencedMessage>();
  const callToTurn = new Map<string, ConversationTurn>();
  for (const turn of turns) {
    for (const entry of turn.messages) {
      if (entry.message.role === "tool" && entry.message.toolCallId) resultByCall.set(entry.message.toolCallId, entry);
      for (const call of entry.message.toolCalls) callToTurn.set(call.id, turn);
    }
  }
  return { resultByCall, callToTurn };
}

function indexActivities(activities: readonly ActivityStreamEvent[]): ActivityIndex {
  const index: ActivityIndex = { byCall: new Map(), bySource: new Map(), bySequence: new Map() };
  for (const activity of activities) {
    const { callId, kind, messageSequence } = activity.value;
    if (callId) appendTo(index.byCall, callId, activity);
    if (MODEL_LIFECYCLE.has(kind)) appendTo(index.bySource, activity.source, activity);
    if (messageSequence !== null) appendTo(index.bySequence, messageSequence, activity);
  }
  return index;
}

/**
 * Places each activity in the turn of its tool call or message. A historical
 * activity without one goes to the nearest turn in time, and live work after
 * the last durable message to the live turn. Returns the activities no turn
 * owns, except model lifecycle events, which become unanchored model items.
 */
function assignActivitiesToTurns(activities: readonly ActivityStreamEvent[], context: PresentationContext) {
  const unplaced: (ActivityStreamEvent | LiveTextEntry)[] = [];
  for (const activity of activities) {
    const { callId, kind, messageSequence } = activity.value;
    let turn = callId
      ? context.callToTurn.get(callId)
      : messageSequence === null
        ? undefined
        : context.sequenceToTurn.get(messageSequence);
    if (turn === undefined && HISTORICAL_ACTIVITY.has(kind))
      turn = nearestTurnByTime(context.turns, activity.createdAt);
    const isLiveFallback =
      turn === undefined &&
      LIVE_WORK_ACTIVITY.has(kind) &&
      activityBelongsToLiveTurn(context.liveTurn, activity.createdAt);
    if (isLiveFallback) turn = context.liveTurn?.turn;
    if (turn) {
      turn.activities.push(activity);
      if (kind === "model_fallback") turn.fallbacks.push(activity);
      else if (isLiveFallback && !MODEL_LIFECYCLE.has(kind)) turn.liveActivities.push(activity);
      if (HISTORICAL_ACTIVITY.has(kind)) turn.historicalActivities.push(activity);
    } else if (!MODEL_LIFECYCLE.has(kind) && kind !== "input_consumed") {
      unplaced.push(activity);
    }
  }
  return unplaced;
}

function liveTurnForUnanchoredActivity(
  turns: readonly ConversationTurn[],
  isExecutionLive: boolean,
): LiveTurnBoundary | undefined {
  const turn = turns.at(-1);
  if (!isExecutionLive || turn === undefined) return undefined;
  const times = messageTimes(turn);
  return times.length === 0 ? undefined : { turn, observedThrough: Math.max(...times) };
}

function nearestTurnByTime(turns: readonly ConversationTurn[], createdAt: string): ConversationTurn | undefined {
  const activityTime = timestamp(createdAt);
  if (activityTime === null) return undefined;
  let nearest: ConversationTurn | undefined;
  let nearestDistance = Number.POSITIVE_INFINITY;
  for (const turn of turns) {
    const times = messageTimes(turn);
    if (times.length === 0) continue;
    const start = Math.min(...times);
    const end = Math.max(...times);
    const distance = activityTime < start ? start - activityTime : activityTime > end ? activityTime - end : 0;
    if (distance < nearestDistance) {
      nearest = turn;
      nearestDistance = distance;
    }
  }
  return nearest;
}

function appendTo<K, V>(map: Map<K, V[]>, key: K, value: V) {
  const values = map.get(key);
  if (values) values.push(value);
  else map.set(key, [value]);
}
