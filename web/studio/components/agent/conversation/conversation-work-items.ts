// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The model and tool items of a turn's work log, grouped by repeated identical calls.
import type {
  ActivityStreamEvent,
  AgentMessage,
  AgentToolCall,
  LiveTextEntry,
  SequencedMessage,
} from "@/lib/session/agent-types";
import type {
  ConversationModelWorkItem,
  ConversationOperationStatus,
  ConversationToolGroup,
  ConversationToolWorkItem,
  ConversationTurn,
  PresentationContext,
} from "./conversation-presentation";
import { QUESTION_TOOLS, questionItem } from "./conversation-questions";
import { activityBelongsToLiveTurn, durationOf, messageInterval, timestamp } from "./conversation-timing";

const MODEL_TERMINAL = new Set(["model_completed", "model_failed"]);
export const MODEL_LIFECYCLE = new Set(["model_started", ...MODEL_TERMINAL]);
// Mirrors Go's modelFailureMarkerPrefix in session_executor.go.
export const MODEL_FAILURE_MARKER_PREFIX = "Model call failed: ";

/** Appends each assistant message's model call and its tool groups and questions to the turn. */
export function appendDurableWork(turn: ConversationTurn, context: PresentationContext) {
  const repeatOf = repeatNumbering(turn.messages);
  for (const entry of turn.messages) {
    if (entry.message.role === "assistant") {
      const model = anchoredModelItem(entry, context);
      turn.operations.push({ kind: "model", key: model.key, item: model });
    }
    const calls = callsInStartOrder(entry, context);
    for (const call of calls) {
      if (QUESTION_TOOLS.has(call.name)) turn.questions.push(questionItem(call, context));
    }
    const workItems = calls.filter((call) => !QUESTION_TOOLS.has(call.name)).map((call) => toolWorkItem(call, context));
    for (const group of repeatGroups(workItems, repeatOf)) {
      turn.operations.push({
        kind: "tool",
        key: `${group.key}:${group.calls.map((item) => item.call.id).join(",")}`,
        group,
      });
    }
  }
}

/** A message's tool calls in the order they started; calls with no known start keep their request order. */
function callsInStartOrder(entry: SequencedMessage, context: PresentationContext): AgentToolCall[] {
  return entry.message.toolCalls
    .map((call, index) => ({ call, index, startedAt: toolStartedAt(call.id, context) }))
    .sort((left, right) => {
      if (left.startedAt !== null && right.startedAt !== null && left.startedAt !== right.startedAt)
        return left.startedAt - right.startedAt;
      return left.index - right.index;
    })
    .map(({ call }) => call);
}

/** Numbers the identical tool calls of one turn: the third of four reads "Repeat 3/4". */
function repeatNumbering(messages: readonly SequencedMessage[]) {
  const totals = new Map<string, number>();
  for (const entry of messages) {
    for (const call of entry.message.toolCalls) {
      if (QUESTION_TOOLS.has(call.name)) continue;
      const fingerprint = toolCallFingerprint(call);
      totals.set(fingerprint, (totals.get(fingerprint) ?? 0) + 1);
    }
  }
  const seen = new Map<string, number>();
  return (fingerprint: string) => {
    const repeatTotal = totals.get(fingerprint) ?? 1;
    const repeatIndex = (seen.get(fingerprint) ?? 0) + 1;
    seen.set(fingerprint, repeatIndex);
    return repeatTotal > 1 ? { repeatIndex, repeatTotal } : { repeatIndex: null, repeatTotal: null };
  };
}

/** Consecutive identical calls share one group; a later identical call starts a new, numbered group. */
function repeatGroups(items: readonly ConversationToolWorkItem[], repeatOf: ReturnType<typeof repeatNumbering>) {
  const groups: ConversationToolGroup[] = [];
  for (const item of items) {
    const fingerprint = toolCallFingerprint(item.call);
    const previous = groups.at(-1);
    if (previous?.key === fingerprint) previous.calls.push(item);
    else groups.push({ key: fingerprint, name: item.call.name, calls: [item], ...repeatOf(fingerprint) });
  }
  return groups;
}

function toolWorkItem(call: AgentToolCall, context: PresentationContext): ConversationToolWorkItem {
  const result = context.resultByCall.get(call.id) ?? null;
  const activities = context.activity.byCall.get(call.id) ?? [];
  const waiting = !result && context.pendingWaitByCall.has(call.id);
  const start = timestamp(activities.find((event) => event.value.kind === "model_tool_call")?.createdAt);
  const end = timestamp(
    activities.findLast((event) => event.value.kind === "tool_completed" || event.value.kind === "tool_failed")
      ?.createdAt,
  );
  const liveInterval =
    context.isExecutionLive && !result && !waiting && start !== null ? ([start, end ?? context.nowMs] as const) : null;
  const interval =
    (result ? messageInterval(result) : null) ??
    (liveInterval && liveInterval[1] >= liveInterval[0] ? liveInterval : null);
  return {
    call,
    target: call.target || (activities.findLast((event) => event.value.target !== "")?.value.target ?? ""),
    status: result ? toolResultStatus(result.message) : liveToolStatus(activities, waiting, context.isExecutionLive),
    resultCode: result?.message.toolExecution?.resultCode ?? "",
    durationMs: result?.message.toolExecution?.durationMs ?? durationOf(interval),
    attemptCount: maximumAttempt(activities),
    interval,
    clockStartedAt: liveInterval && end === null ? start : null,
  };
}

function liveToolStatus(
  activities: readonly ActivityStreamEvent[],
  waiting: boolean,
  isExecutionLive: boolean,
): ConversationOperationStatus {
  if (activities.at(-1)?.value.kind === "tool_failed") return "failed";
  if (waiting) return "waiting";
  return isExecutionLive ? "running" : "unknown";
}

function toolResultStatus(message: AgentMessage): ConversationOperationStatus {
  switch (message.toolExecution?.outcome) {
    case "succeeded":
      return "complete";
    case "known_failure":
      return "failed";
    case "unknown":
    case undefined:
      return "unknown";
  }
}

function toolStartedAt(callId: string, context: PresentationContext): number | null {
  return (
    timestamp(context.resultByCall.get(callId)?.message.startedAt) ??
    timestamp(context.activity.byCall.get(callId)?.find((entry) => entry.value.kind === "model_tool_call")?.createdAt)
  );
}

/**
 * The model call that produced an assistant message. While the Agent is
 * calling the model and this call has started but not finished, its interval
 * runs to now; otherwise it is the message's own start-to-commit interval.
 */
function anchoredModelItem(entry: SequencedMessage, context: PresentationContext): ConversationModelWorkItem {
  const activities = (context.activity.bySequence.get(entry.sequence) ?? []).filter((event) =>
    MODEL_LIFECYCLE.has(event.value.kind),
  );
  const source = activities[0]?.source;
  if (source) context.usedModelSources.add(source);
  const failed = activities.some((event) => event.value.kind === "model_failed");
  const terminal = activities.some((event) => MODEL_TERMINAL.has(event.value.kind));
  const start = modelStartedAt(activities);
  const running = context.isModelRunning && !terminal && start !== null;
  const interval = running && context.nowMs >= start ? ([start, context.nowMs] as const) : messageInterval(entry);
  return {
    key: `model-${String(entry.sequence)}`,
    messageSequence: entry.sequence,
    status: failed ? "failed" : running ? "running" : "complete",
    reasoning: source ? claimReasoning(source, context) : null,
    durationMs: durationOf(interval),
    interval,
    clockStartedAt: running ? start : null,
    summary: modelSummary(entry.message.toolCalls, failed, running),
    attemptCount: maximumAttempt(activities),
    metrics: entry.message.modelCall,
  };
}

/**
 * Model calls no durable message anchors yet: the call now streaming, or one
 * whose message is not in the loaded history. Each joins the turn its message
 * will belong to, or the live turn; otherwise its events and reasoning are
 * returned as earlier activity.
 */
export function appendUnanchoredModels(context: PresentationContext) {
  const unplaced: (ActivityStreamEvent | LiveTextEntry)[] = [];
  for (const [source, activities] of context.activity.bySource) {
    if (context.usedModelSources.has(source)) continue;
    const sequence = activities.find((event) => event.value.messageSequence !== null)?.value.messageSequence;
    if (
      sequence !== null &&
      sequence !== undefined &&
      !activities.some((event) => event.value.kind === "model_started") &&
      context.activity.bySequence
        .get(sequence)
        ?.some((event) => event.source !== source && event.value.kind === "model_started")
    )
      continue;
    const turn = turnForUnanchoredModel(activities, context);
    if (turn) {
      const model = unanchoredModelItem(source, activities, context);
      turn.operations.push({ kind: "model", key: model.key, item: model });
      continue;
    }
    unplaced.push(...activities);
    const reasoning = claimReasoning(source, context);
    if (reasoning) unplaced.push(reasoning);
  }
  return unplaced;
}

function turnForUnanchoredModel(activities: readonly ActivityStreamEvent[], context: PresentationContext) {
  const sequence = activities.find((event) => event.value.messageSequence !== null)?.value.messageSequence ?? null;
  if (sequence !== null) {
    const turn = context.sequenceToTurn.get(sequence);
    if (turn) return turn;
    // The call committing a message after the last loaded one continues the last turn. Sequences
    // have gaps, so the next message is any later one.
    const lastSequence = context.messages.at(-1)?.sequence;
    if (lastSequence !== undefined && sequence > lastSequence) return context.turns.at(-1);
  }
  return activityBelongsToLiveTurn(context.liveTurn, activities[0]?.createdAt) ? context.liveTurn?.turn : undefined;
}

/** An unanchored model call runs from its start until its terminal event, or until now while it has none. */
function unanchoredModelItem(
  source: string,
  activities: readonly ActivityStreamEvent[],
  context: PresentationContext,
): ConversationModelWorkItem {
  const sequence = activities.find((event) => event.value.messageSequence !== null)?.value.messageSequence;
  // The model call and RecordPlatformModelResult are separate Steps, so their
  // Stream sources differ. Their announced message sequence joins the terminal
  // activity; the durable marker also recovers when the Stream was lost.
  const terminal =
    activities.findLast((event) => MODEL_TERMINAL.has(event.value.kind)) ??
    (sequence === null || sequence === undefined
      ? undefined
      : context.activity.bySequence.get(sequence)?.findLast((event) => event.value.kind === "model_failed"));
  const marker = context.messages.find(
    (entry) =>
      entry.sequence === sequence &&
      entry.message.role === "system" &&
      entry.message.content.startsWith(MODEL_FAILURE_MARKER_PREFIX),
  );
  const failed = terminal?.value.kind === "model_failed" || marker !== undefined;
  const running = !failed && terminal === undefined && context.isModelRunning;
  const start = modelStartedAt(activities);
  const end = timestamp(
    terminal?.createdAt ?? marker?.message.createdAt ?? (running ? undefined : activities.at(-1)?.createdAt),
  );
  const effectiveEnd = end ?? context.nowMs;
  const interval = start === null || effectiveEnd < start ? null : ([start, effectiveEnd] as const);
  return {
    key: `model-${source}`,
    messageSequence: null,
    status: failed ? "failed" : terminal ? "complete" : running ? "running" : "unknown",
    reasoning: claimReasoning(source, context),
    durationMs: durationOf(interval),
    interval,
    clockStartedAt: running ? start : null,
    summary: modelSummary(null, failed, running),
    attemptCount: maximumAttempt(activities),
    metrics: null,
  };
}

/** The reasoning streamed for a model call, which only that call's item shows. */
function claimReasoning(source: string, context: PresentationContext): LiveTextEntry | null {
  const reasoning = context.reasoningBySource.get(source) ?? null;
  if (reasoning) context.usedReasoning.add(source);
  return reasoning;
}

function modelStartedAt(activities: readonly ActivityStreamEvent[]): number | null {
  return timestamp(activities.find((event) => event.value.kind === "model_started")?.createdAt);
}

/** What a model call did; toolCalls is null for a call whose message is not durable yet. */
function modelSummary(toolCalls: readonly AgentToolCall[] | null, failed: boolean, running: boolean): string {
  if (failed) return "Model failed";
  if (running) return "Model running";
  if (toolCalls === null) return "Model replied";
  if (toolCalls.some((call) => QUESTION_TOOLS.has(call.name))) return "Model asked for input";
  if (toolCalls.length === 1) return `Model requested ${toolCalls[0].name}`;
  if (toolCalls.length > 1) return `Model requested ${String(toolCalls.length)} tools`;
  return "Model replied";
}

/** Two calls repeat each other when they run the same tool on the same target. */
function toolCallFingerprint(call: AgentToolCall): string {
  return `${call.name}:${call.target}`;
}

function maximumAttempt(activities: readonly ActivityStreamEvent[]): number | null {
  return activities.reduce<number | null>(
    (maximum, event) => (event.value.attempt == null ? maximum : Math.max(maximum ?? 0, event.value.attempt)),
    null,
  );
}
