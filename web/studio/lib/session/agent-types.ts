// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Browser Agent types derive from the canonical protobuf through its explicit view projection.
import type { agentActivityView, agentSnapshotView, connectorContributionView } from "./agent-view";
import type { AgentUserInputAnswer__Output } from "@/lib/generated/protobuf/superverse/v2/AgentUserInputAnswer";
import type { Remedy } from "@/lib/shared/problem/problem";

export type AgentSnapshot = ReturnType<typeof agentSnapshotView>;
export type AgentDescription = AgentSnapshot["description"];
type HistoryPage = AgentSnapshot["history"];
export type SequencedMessage = HistoryPage["messages"][number];
export type AgentMessage = SequencedMessage["message"];
export type AgentToolCall = AgentMessage["toolCalls"][number];
/** A file a user message attached; never its content or a URL. */
export type MessageAttachment = AgentMessage["attachments"][number];
/** How the Session page reaches its attachments' files, which the transport-free conversation shows. */
export type { AttachmentFileLinks } from "./attachments/attachment-downloads";
export { formatFileSize } from "./attachments/attachment-files";
export { sentAttachmentWhat } from "./attachments/attachment-view";
export type PendingUserMessage = AgentSnapshot["queued"][number];
export type AgentEvent = ReturnType<typeof agentActivityView>;
/** A model a Session can run on: the Agent UI literal of an AgentModel enum value. */
export type AgentModel = NonNullable<AgentDescription["model"]>;
export type UserInputQuestion = NonNullable<AgentDescription["pendingUserInput"]>["questions"][number];
export type UserInputAnswer = AgentUserInputAnswer__Output;
export type TodoItem = NonNullable<AgentDescription["todoList"]>["items"][number];
export type ConfirmedRequirements = NonNullable<AgentDescription["confirmedRequirements"]>;
export type Requirements = ConfirmedRequirements["spec"];
/** A Session notification: an automated report of something that happened outside the turn. */
export type AgentNotification = NonNullable<AgentMessage["notification"]>;
/**
 * Where a person acts on a notification, which the page that shows it builds; a link that carries
 * out a remedy names it, and the notice's problem panel shows it as the fix.
 */
export interface NotificationLink {
  label: string;
  href: string;
  onClick?: () => void;
  remedy?: Remedy;
}
/** A background command of the Agent, as the tasks panel shows it. */
export type BackgroundCommand = AgentDescription["backgroundCommands"][number];
/** A subagent of the Agent, as the tasks panel shows it. */
export type Subagent = AgentDescription["subagents"][number];
/** A connector the Session contributes to the connectors library, as the tasks panel shows it. */
export type ConnectorContribution = ReturnType<typeof connectorContributionView>;

/** Whether a subagent may still run: not admitted yet, or running, and not counted as lost by the Session. */
export function isSubagentRunning(subagent: Subagent): boolean {
  return !subagent.lost && (subagent.status === "starting" || subagent.status === "running");
}
export const AgentStatus = {
  INITIALIZING: "initializing",
  WAITING_FOR_MESSAGE: "waiting_for_message",
  CALLING_MODEL: "calling_model",
} as const satisfies Record<string, AgentDescription["status"]>;
const AGENT_MODEL_DETAILS = {
  "anthropic/custom": {
    label: "Anthropic custom endpoint",
    description: "Conservative text and tools profile",
    selectable: false,
  },
  "openai/custom": {
    label: "Openai custom endpoint",
    description: "Conservative text and tools profile",
    selectable: false,
  },
  "xai/custom": { label: "Xai custom endpoint", description: "Conservative text and tools profile", selectable: false },
  "gemini/custom": {
    label: "Gemini custom endpoint",
    description: "Conservative text and tools profile",
    selectable: false,
  },
  "qwen/custom": {
    label: "Qwen custom endpoint",
    description: "Conservative text and tools profile",
    selectable: false,
  },

  "opus-5.5": { label: "Claude Opus 5.5", description: "Strongest", selectable: true },
  "sonnet-5.5": { label: "Claude Sonnet 5.5", description: "Faster and cheaper", selectable: true },
  "haiku-4.5": { label: "Claude Haiku 4.5", description: "Cheapest, suited to testing", selectable: true },
  mock: { label: "Mock", description: "Testing, no model cost", selectable: true },
  "gpt-6-astra": { label: "GPT-6 Astra", description: "Provider unavailable", selectable: false },
  "deepseek-flash": { label: "DeepSeek V4.1 Flash", description: "Provider unavailable", selectable: false },
  "grok-4.7": { label: "Grok 4.7", description: "Provider unavailable", selectable: false },
  "muse-wonder-1.0": { label: "Muse Wonder 1.0", description: "Provider unavailable", selectable: false },
  "gemini-4-argon": { label: "Gemini 4 Argon", description: "Provider unavailable", selectable: false },
  "kimi-k3": { label: "Kimi K3", description: "Provider unavailable", selectable: false },
  "glm-5.3": { label: "GLM 5.3", description: "Provider unavailable", selectable: false },
  "qwen3.8-max": { label: "Qwen 3.8 Max", description: "Provider unavailable", selectable: false },
  "minimax-m3.1-flash": { label: "MiniMax M3.1 Flash", description: "Provider unavailable", selectable: false },
  "kimi-k2.6": { label: "Kimi K2.6", description: "Provider unavailable", selectable: false },
  "glm-5.2": { label: "GLM 5.2", description: "Provider unavailable", selectable: false },
  "minimax-m3": { label: "MiniMax M3", description: "Provider unavailable", selectable: false },
} as const satisfies Record<AgentModel, { label: string; description: string; selectable: boolean }>;

/** The models a Session can switch between, in picker order; Studio offers mock only where the server allows it. */
export const AGENT_MODEL_OPTIONS = (Object.keys(AGENT_MODEL_DETAILS) as AgentModel[])
  .filter((value) => AGENT_MODEL_DETAILS[value].selectable)
  .map((value) => ({
    value,
    ...AGENT_MODEL_DETAILS[value],
  }));

/** Whether a stored name is a model the picker offers. */
export function isAgentModel(name: string): name is AgentModel {
  return Object.hasOwn(AGENT_MODEL_DETAILS, name);
}

export function agentModelLabel(model: AgentModel): string {
  return AGENT_MODEL_DETAILS[model].label;
}

/** The Session Watch's two text Streams, which the page keeps apart. */
export const EventStream = { REASONING: "reasoning", ASSISTANT: "assistant" } as const;
export type EventStream = (typeof EventStream)[keyof typeof EventStream];
export const EventKind = {
  INPUT_CONSUMED: "input_consumed",
  SNAPSHOT_REQUIRED: "snapshot_required",
  MODEL_STARTED: "model_started",
  MODEL_FAILED: "model_failed",
  MODEL_COMPLETED: "model_completed",
  AGENT_INTERRUPTED: "agent_interrupted",
} as const satisfies Record<string, AgentEvent["kind"]>;

/** Whether the Stop button applies: a turn runs, or a background command or a subagent does. */
export function hasRunningWork(description: AgentDescription): boolean {
  const turnRuns =
    description.status !== AgentStatus.WAITING_FOR_MESSAGE && description.status !== AgentStatus.INITIALIZING;
  return turnRuns || hasRunningTasks(description);
}

/** Whether a background command or a subagent runs, whose notification opens a turn when it ends. */
export function hasRunningTasks(description: AgentDescription): boolean {
  return (
    description.backgroundCommands.some((command) => command.status === "running") ||
    description.subagents.some(isSubagentRunning)
  );
}

/** Where a Stream event sits: its Watch resume position, time and producing model call. */
type StreamPosition = { resumeToken: string; createdAt: string; source: string };
export type ActivityStreamEvent = StreamPosition & { kind: "activity"; value: AgentEvent };
export type AssistantStreamEvent = StreamPosition & { kind: "assistant_text"; value: string };
export type ReasoningStreamEvent = StreamPosition & { kind: "reasoning_summary"; value: string };

/** Text a model call is streaming, keyed by the call (its source). */
export interface LiveTextEntry {
  source: string;
  createdAt: string;
  value: string;
  isComplete: boolean;
}
