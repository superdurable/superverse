// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock Agent's conversation state, the counters every script keeps current, and what Session reads return.
// Development only; no model or tool is called.
import type { AgentAttachment } from "@/lib/generated/protobuf/superverse/v2/AgentAttachment";
import type { AgentMockProgress__Output } from "@/lib/generated/protobuf/superverse/v2/AgentMockProgress";
import type { AgentBackgroundCommand__Output } from "@/lib/generated/protobuf/superverse/v2/AgentBackgroundCommand";
import type { AgentDescription__Output } from "@/lib/generated/protobuf/superverse/v2/AgentDescription";
import type { AgentEvent__Output } from "@/lib/generated/protobuf/superverse/v2/AgentEvent";
import type { AgentMessage__Output } from "@/lib/generated/protobuf/superverse/v2/AgentMessage";
import type { AgentModel__Output } from "@/lib/generated/protobuf/superverse/v2/AgentModel";
import type { AgentModelCallMetrics } from "@/lib/generated/protobuf/superverse/v2/AgentModelCallMetrics";
import type { AgentNotification__Output } from "@/lib/generated/protobuf/superverse/v2/AgentNotification";
import type { AgentPendingApproval__Output } from "@/lib/generated/protobuf/superverse/v2/AgentPendingApproval";
import type { AgentPendingTimer__Output } from "@/lib/generated/protobuf/superverse/v2/AgentPendingTimer";
import type { AgentPendingUserInput__Output } from "@/lib/generated/protobuf/superverse/v2/AgentPendingUserInput";
import type { AgentPendingUserMessage__Output } from "@/lib/generated/protobuf/superverse/v2/AgentPendingUserMessage";
import type { AgentRequirements__Output } from "@/lib/generated/protobuf/superverse/v2/AgentRequirements";
import type { AgentSubagent__Output } from "@/lib/generated/protobuf/superverse/v2/AgentSubagent";
import type { AgentConfirmedRequirements__Output } from "@/lib/generated/protobuf/superverse/v2/AgentConfirmedRequirements";
import type { AgentTodoList__Output } from "@/lib/generated/protobuf/superverse/v2/AgentTodoList";
import type { AgentToolCall__Output } from "@/lib/generated/protobuf/superverse/v2/AgentToolCall";
import type { AgentToolExecutionMetrics } from "@/lib/generated/protobuf/superverse/v2/AgentToolExecutionMetrics";
import { nowIso } from "./clock";
import type { MockStore } from "./store";

// The store keeps 64-bit counters as numbers; the protobuf codec turns them
// into the snapshot's decimal strings.

/**
 * One durable conversation message, in the shape AgentMessage encodes from. Like the public view,
 * a tool result has no content and a system message is a marker whose content is its text.
 */
export interface MockAgentMessage {
  role: AgentMessage__Output["role"];
  content: string;
  toolCalls: AgentToolCall__Output[];
  toolCallId?: string;
  toolName?: string;
  answeredInputCallId?: string;
  startedAt?: string;
  createdAt: string;
  modelCall?: AgentModelCallMetrics;
  toolExecution?: AgentToolExecutionMetrics;
  /** A ROLE_NOTIFICATION message's notification; its content is the summary. */
  notification?: AgentNotification__Output;
  /** The files a user message attached. */
  attachments?: AgentAttachment[];
}

/** A background command of the mock Agent, in the shape AgentBackgroundCommand encodes from. */
interface MockBackgroundCommand extends Omit<AgentBackgroundCommand__Output, "outputBytes"> {
  outputBytes: number;
}

export interface MockSequencedMessage {
  sequence: number;
  message: MockAgentMessage;
}

interface MockAgentTodoList extends Omit<AgentTodoList__Output, "revision"> {
  revision: number;
}

interface MockConfirmedRequirements extends Omit<AgentConfirmedRequirements__Output, "revision"> {
  revision: number;
}

interface MockAgentDescription {
  status: AgentDescription__Output["status"];
  waitingInputRound: number;
  /** The selected model, which the next turn starts on. */
  model: AgentModel__Output;
  modelRevision: string;
  /** The model and effort of the current context, which a turn refreshes from the selected model. */
  contextModel: AgentModel__Output;
  effort: string;
  firstRetainedSequence: number;
  lastSequence: number;
  pendingApproval: AgentPendingApproval__Output | null;
  pendingTimer: AgentPendingTimer__Output | null;
  pendingUserInput: AgentPendingUserInput__Output | null;
  todoList: MockAgentTodoList | null;
  confirmedRequirements: MockConfirmedRequirements | null;
  pendingQueuedMessageCount: number;
  pendingSteeredMessageCount: number;
  availableTools: string[];
  /** The current turn, which a Stop names; each turn start counts it. */
  turn: number;
  backgroundCommands: MockBackgroundCommand[];
  subagents: AgentSubagent__Output[];
  /** Whether the Preview deploys each commit the Agent pushes; on for a new Session. */
  isPreviewAutoDeployEnabled: boolean;
  /** Where the mock composer's scenario stands, as the server's mock model reports it. */
  mockProgress?: AgentMockProgress__Output | null;
}

/** An activity event's value, in the shape AgentEvent encodes from. */
export interface MockActivity {
  kind: AgentEvent__Output["kind"];
  message: string;
  callId?: string;
  toolName?: string;
  target?: string;
  messageSequence?: number;
  attempt?: number;
}

/**
 * One recent event of the Session's activity Stream, which the Session Watch replays by
 * position. The scripted Agent streams no reasoning or assistant text.
 */
export interface MockActivityEvent {
  createdAt: string;
  source: string;
  /** The subagent whose live status line the event is; absent for the Session's own activity. */
  taskId?: string;
  value: MockActivity;
}

/** A requirements card the mock Agent is writing, whose draft the Session Watch streams until it is recorded. */
interface MockRequirementsDraft {
  callId: string;
  turn: number;
  baseRevision: number;
  /** When the agent started writing it, on the mock clock. */
  startedAtMs: number;
  spec: AgentRequirements__Output;
}

export interface MockAgent {
  messages: MockSequencedMessage[];
  nextSequence: number;
  queued: AgentPendingUserMessage__Output[];
  steered: AgentPendingUserMessage__Output[];
  description: MockAgentDescription;
  activity: MockActivityEvent[];
  requirementsDraft?: MockRequirementsDraft | null;
}

/** What the Session snapshot reads of the Agent. */
export interface MockAgentSnapshot {
  history: { messages: MockSequencedMessage[]; nextBeforeSequence?: number };
  description: MockAgentDescription;
  queued: AgentPendingUserMessage__Output[];
  steered: AgentPendingUserMessage__Output[];
}

/** A notification whose fields its kind does not use stay empty, as the protocol reads them. */
export function mockNotification(
  value: Pick<AgentNotification__Output, "id" | "kind" | "source" | "summary"> & Partial<AgentNotification__Output>,
): AgentNotification__Output {
  return {
    occurredAt: nowIso(),
    taskId: "",
    taskStatus: "TASK_STATUS_UNSPECIFIED",
    outcome: "OUTCOME_UNSPECIFIED",
    owner: "OWNER_UNSPECIFIED",
    code: "",
    phase: "",
    commit: "",
    resourceId: "",
    gaps: [],
    gapCount: 0,
    message: "",
    slug: "",
    connectorEvent: "CONNECTOR_EVENT_UNSPECIFIED",
    version: null,
    remedy: "FAILURE_REMEDY_UNSPECIFIED",
    resolvedNoticeId: "",
    url: "",
    ...value,
  };
}

export function bumpWaitingRound(agent: MockAgent) {
  agent.description.waitingInputRound += 1;
}

export function syncDescriptionCounts(agent: MockAgent) {
  agent.description.pendingQueuedMessageCount = agent.queued.length;
  agent.description.pendingSteeredMessageCount = agent.steered.length;
  agent.description.lastSequence = agent.messages.at(-1)?.sequence ?? 0;
}

export function appendMessage(agent: MockAgent, message: MockAgentMessage): number {
  const sequence = agent.nextSequence++;
  agent.messages.push({ sequence, message });
  syncDescriptionCounts(agent);
  return sequence;
}

export function snapshotForSession(store: MockStore, projectId: string): MockAgentSnapshot | null {
  const agent = store.agents.get(projectId);
  if (!agent) return null;
  const messages = agent.messages.slice(-40);
  return {
    history: {
      messages,
      nextBeforeSequence: agent.messages.length > 40 ? messages[0].sequence : undefined,
    },
    description: {
      ...agent.description,
      backgroundCommands: [...agent.description.backgroundCommands],
      subagents: [...agent.description.subagents],
    },
    queued: [...agent.queued],
    steered: [...agent.steered],
  };
}

export function archivedMessages(store: MockStore, projectId: string, beforeSequence: number) {
  const agent = store.agents.get(projectId);
  if (!agent) return null;
  const older = agent.messages.filter((entry) => entry.sequence < beforeSequence);
  const page = older.slice(-20);
  return {
    messages: page,
    nextBeforeSequence: page.length > 0 && older[0].sequence < page[0].sequence ? page[0].sequence : undefined,
  };
}

export function recentActivity(store: MockStore, projectId: string): MockActivityEvent[] {
  return store.agents.get(projectId)?.activity ?? [];
}

export function deleteQueuedMessage(store: MockStore, projectId: string, messageId: string): boolean {
  const agent = store.agents.get(projectId);
  if (!agent) return false;
  agent.queued = agent.queued.filter((entry) => entry.messageId !== messageId);
  syncDescriptionCounts(agent);
  return true;
}

export function steerQueuedMessage(store: MockStore, projectId: string, messageId: string): boolean {
  const agent = store.agents.get(projectId);
  if (!agent) return false;
  const index = agent.queued.findIndex((entry) => entry.messageId === messageId);
  if (index < 0) return false;
  const [item] = agent.queued.splice(index, 1);
  agent.steered.push(item);
  syncDescriptionCounts(agent);
  return true;
}
