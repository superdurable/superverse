// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Disposable Session presentation state. The typed resource snapshot owns durable state.
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import {
  AgentStatus,
  EventKind,
  EventStream,
  hasRunningWork,
  isSubagentRunning,
  type ActivityStreamEvent,
  type AgentEvent,
  type AgentSnapshot,
  type AssistantStreamEvent,
  type LiveTextEntry,
  type ReasoningStreamEvent,
  type SequencedMessage,
} from "./agent-types";
import { appendActivityEvent, mergeSequencedMessages } from "./message-merge";
import { isDraftOver, type RequirementsDraft } from "./draft/requirements-draft";

const MAXIMUM_ACTIVITY_EVENTS = 200;
const MAXIMUM_MODEL_SOURCES = 256;
const MAXIMUM_PROVISIONAL_TEXT_EVENTS = 128;
const MAXIMUM_PROVISIONAL_TEXT_CHARACTERS = 256 * 1024;

export interface SessionState {
  resource: SessionSnapshot__Output | null;
  /** The Session admitted a Stop and still runs work, which ends once the stop reaches it. */
  stopping: boolean;
  unavailable: boolean;
  /** The reads that found the Session not started yet; each schedules the next. */
  sessionStartReads: number;
  snapshot: AgentSnapshot | null;
  messages: SequencedMessage[];
  loading: boolean;
  /** False after a failed read until the next snapshot arrives; the snapshot shown may be stale. */
  snapshotCurrent: boolean;
  streamState: "idle" | "connecting" | "connected" | "reconnecting";
  streamGeneration: number;
  mutating: boolean;
  mutationKind: string | null;
  historyLoading: boolean;
  historyLoadMode: "page" | "beginning" | null;
  historyLoadedCount: number;
  hasEarlierMessages: boolean;
  answeringQuestionCallId: string | null;
  answeredQuestionCallId: string | null;
  assistant: LiveTextEntry | null;
  reasoning: LiveTextEntry[];
  activities: ActivityStreamEvent[];
  /** The newest draft of the card the agent is writing, until the Snapshot says its call is over; best effort. */
  requirementsDraft: RequirementsDraft | null;
  /** The newest live status line of each running subagent, by task ID; best effort. */
  subagentLines: ReadonlyMap<string, string>;
  /** The message sequence each model call (source) will commit. */
  modelSequences: ReadonlyMap<string, number>;
  /** The model calls a Stop ended: text and activity they still stream are dropped. */
  stoppedSources: ReadonlySet<string>;
  committedSequence: number;
  /** Text whose model call has not been seen on the activity stream yet. */
  provisionalText: TextAction[];
  error: string | null;
  commandError: string | null;
}

export type SessionAction =
  | { type: "resource"; resource: SessionSnapshot__Output; snapshot: AgentSnapshot | null; hasEarlierMessages: boolean }
  | { type: "archived"; messages: SequencedMessage[]; hasEarlierMessages: boolean }
  | { type: "historyLoading"; loading: boolean; mode?: "page" | "beginning"; loadedCount?: number }
  | { type: "streamConnecting" }
  | { type: "streamConnected" }
  | { type: "streamFailed" }
  | { type: "textEvent"; stream: EventStream; event: AssistantStreamEvent | ReasoningStreamEvent }
  | { type: "activityEvent"; event: ActivityStreamEvent }
  | { type: "requirementsDraft"; draft: RequirementsDraft }
  | { type: "subagentActivity"; taskId: string; event: AgentEvent }
  | { type: "mutating"; mutating: boolean; kind?: string }
  | { type: "stopRequested" }
  | { type: "answeringQuestions"; callId: string | null }
  | { type: "questionsAnswered"; callId: string }
  | { type: "commandError"; error: string }
  | { type: "sessionStarting" }
  | { type: "executionStopped"; reason: string }
  | { type: "error"; error: string; unavailable: boolean };

type TextAction = Extract<SessionAction, { type: "textEvent" }>;

export const initialSessionState: SessionState = {
  resource: null,
  stopping: false,
  unavailable: false,
  sessionStartReads: 0,
  snapshot: null,
  messages: [],
  loading: true,
  snapshotCurrent: false,
  streamState: "idle",
  streamGeneration: 0,
  mutating: false,
  mutationKind: null,
  historyLoading: false,
  historyLoadMode: null,
  historyLoadedCount: 0,
  hasEarlierMessages: false,
  answeringQuestionCallId: null,
  answeredQuestionCallId: null,
  assistant: null,
  reasoning: [],
  activities: [],
  requirementsDraft: null,
  subagentLines: new Map(),
  modelSequences: new Map(),
  stoppedSources: new Set(),
  committedSequence: 0,
  provisionalText: [],
  error: null,
  commandError: null,
};

function appendText(
  current: LiveTextEntry | undefined,
  event: AssistantStreamEvent | ReasoningStreamEvent,
  isComplete: boolean,
): LiveTextEntry {
  return current?.source === event.source
    ? { ...current, value: current.value + event.value }
    : { source: event.source, createdAt: event.createdAt, value: event.value, isComplete };
}

function isModelFinished(activities: readonly ActivityStreamEvent[], source: string): boolean {
  return activities.some(
    (event) =>
      event.source === source &&
      (event.value.kind === EventKind.MODEL_COMPLETED || event.value.kind === EventKind.MODEL_FAILED),
  );
}

function isUncommittedModelSource(state: SessionState, source: string): boolean {
  const sequence = state.modelSequences.get(source);
  return sequence !== undefined && sequence > state.committedSequence && !state.stoppedSources.has(source);
}

/** A Stop ends every model call seen so far; one that is cancelled can stream on for a while. */
function stopModelSources(state: SessionState): ReadonlySet<string> {
  const stopped = [...state.stoppedSources, ...state.modelSequences.keys()];
  return new Set(stopped.slice(Math.max(0, stopped.length - MAXIMUM_MODEL_SOURCES)));
}

/** Streamed text survives only until its model call's message is committed. */
function reconcileModelText(state: SessionState): SessionState {
  return {
    ...state,
    assistant: state.assistant && isUncommittedModelSource(state, state.assistant.source) ? state.assistant : null,
    reasoning: state.reasoning.filter((entry) => isUncommittedModelSource(state, entry.source)),
  };
}

function applyTextEvent(state: SessionState, action: TextAction): SessionState {
  const { event } = action;
  if (state.stoppedSources.has(event.source)) return state;
  if (!state.modelSequences.has(event.source)) {
    // Text and model activity arrive on independent Streams. Never guess that an
    // unknown source belongs after the durable snapshot, even during replay.
    const provisionalText = [...state.provisionalText, action];
    let characters = provisionalText.reduce((total, entry) => total + entry.event.value.length, 0);
    while (
      provisionalText.length > MAXIMUM_PROVISIONAL_TEXT_EVENTS ||
      characters > MAXIMUM_PROVISIONAL_TEXT_CHARACTERS
    ) {
      characters -= provisionalText.shift()!.event.value.length;
    }
    return { ...state, provisionalText };
  }
  if (!isUncommittedModelSource(state, event.source)) return state;
  const complete = isModelFinished(state.activities, event.source);
  if (action.stream === EventStream.ASSISTANT)
    return { ...state, assistant: appendText(state.assistant ?? undefined, event, complete) };
  const index = state.reasoning.findIndex((entry) => entry.source === event.source);
  const entry = appendText(state.reasoning[index], event, complete);
  return {
    ...state,
    reasoning:
      index < 0 ? [...state.reasoning, entry] : state.reasoning.map((item, at) => (at === index ? entry : item)),
  };
}

function completeLiveText(state: SessionState, source: string): Pick<SessionState, "assistant" | "reasoning"> {
  return {
    assistant: state.assistant?.source === source ? { ...state.assistant, isComplete: true } : state.assistant,
    reasoning: state.reasoning.map((entry) => (entry.source === source ? { ...entry, isComplete: true } : entry)),
  };
}

/** Drops the queued and steered messages the Agent just consumed. */
function applyInputConsumption(snapshot: AgentSnapshot, event: ActivityStreamEvent): AgentSnapshot {
  const consumption = event.value.inputConsumption;
  if (event.value.kind !== EventKind.INPUT_CONSUMED || consumption === null) return snapshot;
  const consumed = new Set([...consumption.queuedMessageIds, ...consumption.steeredMessageIds]);
  const queued = snapshot.queued.filter((message) => !consumed.has(message.messageId));
  const steered = snapshot.steered.filter((message) => !consumed.has(message.messageId));
  return {
    ...snapshot,
    queued,
    steered,
    description: {
      ...snapshot.description,
      pendingQueuedMessageCount: queued.length,
      pendingSteeredMessageCount: steered.length,
    },
  };
}

/** The draft to keep against the state's Snapshot and messages: none once its card call is over. */
function currentDraft(state: SessionState, draft: RequirementsDraft | null): RequirementsDraft | null {
  const description = state.snapshot?.description;
  if (!draft || !description) return draft;
  const context = {
    revision: description.confirmedRequirements?.revision ?? 0,
    turn: description.turn,
    waiting: description.status === AgentStatus.WAITING_FOR_MESSAGE,
    answers: (callId: string) => state.messages.some(({ message }) => message.toolCallId === callId),
  };
  return isDraftOver(draft, context) ? null : draft;
}

function applySnapshot(state: SessionState, snapshot: AgentSnapshot, hasEarlierMessages: boolean): SessionState {
  const pendingCallId = snapshot.description.pendingUserInput?.callId;
  const messages = mergeSequencedMessages(state.messages, snapshot.history.messages);
  return reconcileModelText({
    ...state,
    snapshot,
    messages,
    requirementsDraft: currentDraft({ ...state, snapshot, messages }, state.requirementsDraft),
    hasEarlierMessages,
    answeredQuestionCallId: pendingCallId === state.answeredQuestionCallId ? state.answeredQuestionCallId : null,
    loading: false,
    snapshotCurrent: true,
    // Current and archived message sequences never decrease within a Session.
    // A committed message cannot become provisional after a later read.
    committedSequence: Math.max(state.committedSequence, snapshot.description.lastSequence),
    // A subagent's live line lasts only while the snapshot lists it as running.
    subagentLines: new Map(
      [...state.subagentLines].filter(([taskId]) =>
        snapshot.description.subagents.some((subagent) => subagent.taskId === taskId && isSubagentRunning(subagent)),
      ),
    ),
    error: null,
  });
}

function applyActivity(state: SessionState, event: ActivityStreamEvent): SessionState {
  const { kind, messageSequence } = event.value;
  if (state.stoppedSources.has(event.source)) return state;
  if (kind === EventKind.AGENT_INTERRUPTED)
    return reconcileModelText({
      ...state,
      stoppedSources: stopModelSources(state),
      activities: appendActivityEvent(state.activities, event, MAXIMUM_ACTIVITY_EVENTS),
      provisionalText: [],
    });
  const modelLifecycle =
    kind === EventKind.MODEL_STARTED || kind === EventKind.MODEL_COMPLETED || kind === EventKind.MODEL_FAILED;
  let modelSequences = state.modelSequences;
  if (modelLifecycle && messageSequence !== null) {
    const sequences = new Map(modelSequences);
    sequences.delete(event.source);
    sequences.set(event.source, messageSequence);
    while (sequences.size > MAXIMUM_MODEL_SOURCES) sequences.delete(sequences.keys().next().value!);
    modelSequences = sequences;
  }
  const modelFinished = kind === EventKind.MODEL_COMPLETED || kind === EventKind.MODEL_FAILED;
  let next: SessionState = {
    ...state,
    ...(modelFinished ? completeLiveText(state, event.source) : {}),
    snapshot: state.snapshot && applyInputConsumption(state.snapshot, event),
    activities:
      kind === EventKind.SNAPSHOT_REQUIRED
        ? state.activities
        : appendActivityEvent(state.activities, event, MAXIMUM_ACTIVITY_EVENTS),
    modelSequences,
    provisionalText: [],
  };
  // Text that waited for its model call may now place itself.
  for (const provisional of state.provisionalText) next = applyTextEvent(next, provisional);
  return reconcileModelText(next);
}

export function sessionReducer(state: SessionState, action: SessionAction): SessionState {
  switch (action.type) {
    case "resource": {
      const stopping = state.stopping && action.snapshot !== null && hasRunningWork(action.snapshot.description);
      const next = { ...state, resource: action.resource, loading: false, unavailable: false, error: null, stopping };
      return action.snapshot
        ? applySnapshot(next, action.snapshot, action.hasEarlierMessages)
        : { ...next, snapshot: null, snapshotCurrent: true };
    }
    case "stopRequested":
      return { ...state, stopping: true };
    case "archived":
      return {
        ...state,
        messages: mergeSequencedMessages(state.messages, action.messages),
        hasEarlierMessages: action.hasEarlierMessages,
        historyLoading: false,
        historyLoadMode: null,
        historyLoadedCount: 0,
      };
    case "historyLoading":
      return {
        ...state,
        historyLoading: action.loading,
        historyLoadMode: action.loading ? (action.mode ?? state.historyLoadMode) : null,
        historyLoadedCount: action.loading ? (action.loadedCount ?? state.historyLoadedCount) : 0,
      };
    case "streamConnecting":
      return {
        ...state,
        provisionalText: [],
        streamState: state.streamState === "reconnecting" ? "reconnecting" : "connecting",
      };
    case "streamConnected":
      return { ...state, streamState: "connected" };
    case "streamFailed":
      return { ...state, streamState: "reconnecting", streamGeneration: state.streamGeneration + 1 };
    case "textEvent":
      return applyTextEvent(state, action);
    case "activityEvent":
      return applyActivity(state, action.event);
    case "requirementsDraft":
      return { ...state, requirementsDraft: currentDraft(state, action.draft) };
    case "subagentActivity": {
      // The tool and what it works on say more than the event's generic message.
      const { toolName, target, message } = action.event;
      const line = toolName && target ? `${toolName} ${target}` : message;
      return line ? { ...state, subagentLines: new Map(state.subagentLines).set(action.taskId, line) } : state;
    }
    case "mutating":
      return {
        ...state,
        mutating: action.mutating,
        mutationKind: action.mutating ? (action.kind ?? null) : null,
        ...(action.mutating ? { error: null, commandError: null } : {}),
      };
    case "questionsAnswered":
      return { ...state, answeredQuestionCallId: action.callId };
    case "answeringQuestions":
      return { ...state, answeringQuestionCallId: action.callId };
    case "commandError":
      // Snapshot/read success is not proof that an uncertain command was admitted.
      // Only an explicit next command or a resource-owner reset clears this error.
      return { ...state, commandError: action.error };
    case "executionStopped":
      // An operation found the Session's engine execution stopped, as the Go snapshot reports it:
      // the Session reads FAILED, with interaction disabled, until a snapshot says otherwise.
      return state.resource
        ? {
            ...state,
            resource: {
              ...state.resource,
              status: "SESSION_STATUS_FAILED",
              failure: {
                code: action.reason,
                phase: "agent",
                message: "The Agent execution stopped. Its conversation and last workspace state remain available.",
                isRecoverable: true,
                remedy: "FAILURE_REMEDY_UNSPECIFIED",
                owner: "FAILURE_OWNER_UNSPECIFIED",
              },
            },
          }
        : state;
    case "sessionStarting":
      return { ...state, loading: true, sessionStartReads: state.sessionStartReads + 1 };
    case "error":
      return { ...state, loading: false, snapshotCurrent: false, unavailable: action.unavailable, error: action.error };
  }
}
