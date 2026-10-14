// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Studio mock-only seed conversation. It never reaches a production client.
import type { AgentModelCallMetrics } from "@/lib/generated/protobuf/superverse/v2/AgentModelCallMetrics";
import type { AgentToolCall__Output } from "@/lib/generated/protobuf/superverse/v2/AgentToolCall";
import {
  appendMessage,
  type MockActivity,
  type MockActivityEvent,
  type MockAgent,
  type MockAgentMessage,
} from "./agent-state";
import { createAgentState, mockMessage, mockToolResult } from "./seed";

const SEED_TOOL_NAMES = [
  "read",
  "edit",
  "write",
  "grep",
  "glob",
  "bash",
  "delegate",
  "commit_and_push",
  "write_todos",
  "start_timer",
  "request_user_input",
];

function at(baseMs: number, seconds: number) {
  return new Date(baseMs + seconds * 1_000).toISOString();
}

function message(
  role: MockAgentMessage["role"],
  content: string,
  createdAt: string,
  extra: Partial<MockAgentMessage> = {},
) {
  return mockMessage(role, content, { createdAt, ...extra });
}

function activity(
  sequence: number,
  kind: MockActivity["kind"],
  createdAt: string,
  { source, ...value }: Partial<Omit<MockActivity, "kind">> & { source?: string } = {},
): MockActivityEvent {
  return {
    createdAt,
    source: source ?? `activity-${sequence}`,
    value: { kind, message: kind.slice("KIND_".length).toLowerCase().replaceAll("_", " "), ...value },
  };
}

function toolCall(id: string, name: string, target: string): AgentToolCall__Output {
  return { id, name, target };
}

function addToolTurn(
  agent: MockAgent,
  baseMs: number,
  { user, intro, calls, offset = 0 }: { user: string; intro: string; calls: AgentToolCall__Output[]; offset?: number },
) {
  appendMessage(agent, message("ROLE_USER", user, at(baseMs, offset)));
  const assistantSequence = appendMessage(
    agent,
    message("ROLE_ASSISTANT", intro, at(baseMs, offset + 1), { toolCalls: calls }),
  );
  return assistantSequence;
}

/** A tool result, which carries no content; the outcome and duration come from the call's activity. */
function addResult(agent: MockAgent, baseMs: number, offset: number, call: AgentToolCall__Output) {
  appendMessage(
    agent,
    mockToolResult({
      createdAt: at(baseMs, offset),
      toolCallId: call.id,
      toolName: call.name,
      ...(call.name === "commit_and_push"
        ? { toolExecution: { outcome: "OUTCOME_SUCCEEDED", resultCode: "pushed" } }
        : {}),
    }),
  );
}

function addModel(
  agent: MockAgent,
  baseMs: number,
  source: string,
  messageSequence: number,
  start: number,
  end: number | null,
  metrics: AgentModelCallMetrics,
  outcome: MockActivity["kind"] = "KIND_MODEL_COMPLETED",
) {
  const durableMessage = agent.messages.find((entry) => entry.sequence === messageSequence)?.message;
  if (durableMessage?.role === "ROLE_ASSISTANT") {
    durableMessage.startedAt = at(baseMs, start);
    durableMessage.modelCall = metrics;
  }
  agent.activity.push(
    activity(agent.activity.length + 1, "KIND_MODEL_STARTED", at(baseMs, start), {
      source,
      messageSequence,
      message: "Model started",
      attempt: 1,
    }),
  );
  // A call served by another model than the requested one was told to the person.
  if (metrics.servedModel && metrics.servedModel !== metrics.model) {
    agent.activity.push(
      activity(agent.activity.length + 1, "KIND_MODEL_FALLBACK", at(baseMs, start + 0.1), {
        source,
        messageSequence,
        message: `Continuing with ${metrics.servedModel}.`,
      }),
    );
  }
  if (end !== null) {
    agent.activity.push(
      activity(agent.activity.length + 1, outcome, at(baseMs, end), {
        source,
        messageSequence,
        message: outcome === "KIND_MODEL_FAILED" ? "Model request failed" : "Model completed",
        attempt: 1,
      }),
    );
  }
}

interface ToolActivityOptions {
  attempt?: number;
  failed?: boolean;
  message?: string;
  /** Progress carries only the attempt number, never output. */
  progress?: { at: number; attempt: number }[];
}

function addToolActivity(
  agent: MockAgent,
  baseMs: number,
  call: AgentToolCall__Output,
  messageSequence: number,
  start: number,
  end: number | null,
  options: ToolActivityOptions = {},
) {
  const durableResult = agent.messages.find((entry) => entry.message.toolCallId === call.id)?.message;
  if (durableResult) {
    durableResult.startedAt = at(baseMs, start);
    if (end !== null) {
      durableResult.createdAt = at(baseMs, end);
      durableResult.toolExecution = {
        ...durableResult.toolExecution,
        durationMs: Math.round((end - start) * 1_000),
      };
    }
  }
  agent.activity.push(
    activity(agent.activity.length + 1, "KIND_MODEL_TOOL_CALL", at(baseMs, start), {
      callId: call.id,
      toolName: call.name,
      target: call.target,
      messageSequence,
      message: `Calling ${call.name}`,
      attempt: options.attempt ?? 1,
    }),
  );
  for (const progress of options.progress ?? []) {
    agent.activity.push(
      activity(agent.activity.length + 1, "KIND_TOOL_PROGRESS", at(baseMs, progress.at), {
        callId: call.id,
        toolName: call.name,
        target: call.target,
        messageSequence,
        message: `Attempt ${String(progress.attempt)}`,
        attempt: progress.attempt,
      }),
    );
  }
  if (end !== null) {
    agent.activity.push(
      activity(
        agent.activity.length + 1,
        options.failed ? "KIND_TOOL_FAILED" : "KIND_TOOL_COMPLETED",
        at(baseMs, end),
        {
          callId: call.id,
          toolName: call.name,
          target: call.target,
          messageSequence,
          message: options.message ?? (options.failed ? `${call.name} failed` : `${call.name} completed`),
          attempt: options.attempt ?? 1,
        },
      ),
    );
  }
}

function completed(agent: MockAgent, baseMs: number) {
  const list = toolCall("completed-list", "glob", "**/*.tsx");
  const read = toolCall("completed-read", "read", "web/studio/README.md");
  const search = toolCall("completed-search", "grep", "ConversationTimeline");
  const first = addToolTurn(agent, baseMs, {
    user: "Make the agent conversation easier to follow.",
    intro: "I’ll first map the current timeline and its message paths.",
    calls: [list, read, search],
  });
  addResult(agent, baseMs, 4, list);
  addResult(agent, baseMs, 5, read);
  addResult(agent, baseMs, 6, search);
  appendMessage(
    agent,
    message("ROLE_ASSISTANT", "I found the noise source. I’m grouping execution details by turn now.", at(baseMs, 7)),
  );
  const patch = toolCall("completed-patch", "edit", "web/studio/components/timeline.tsx");
  const note = toolCall("completed-note", "write", "web/studio/notes/timeline.md");
  const shortCommand = toolCall("completed-short", "bash", "Show the changed files");
  const longCommand = toolCall("completed-long", "bash", "Run the tests");
  const commit = toolCall("completed-commit", "commit_and_push", "Prototype conversation-first timeline");
  const second = appendMessage(
    agent,
    message("ROLE_ASSISTANT", "The view model is ready; I’ll verify the edit and preserve it in Git.", at(baseMs, 8), {
      toolCalls: [patch, note, shortCommand, longCommand, commit],
    }),
  );
  addResult(agent, baseMs, 15, patch);
  addResult(agent, baseMs, 18, note);
  addResult(agent, baseMs, 19, shortCommand);
  addResult(agent, baseMs, 40, longCommand);
  addResult(agent, baseMs, 69, commit);
  appendMessage(
    agent,
    message(
      "ROLE_ASSISTANT",
      "Done — the primary conversation is calm, while the full execution record remains one click away.",
      at(baseMs, 70),
    ),
  );
  addModel(agent, baseMs, "model-completed-1", first, 0.1, 1.0, {
    model: "claude-opus-5-5",
    servedModel: "claude-opus-5-5",
    effort: "medium",
    durationMs: 900,
    firstEventMs: 320,
    inputTokens: 5_210,
    cacheReadInputTokens: 0,
    cacheCreationInputTokens: 4_880,
    outputTokens: 310,
    thinkingTokens: 120,
    droppedThinkingBlocks: 0,
  });
  addToolActivity(agent, baseMs, list, first, 1.1, 2.0);
  addToolActivity(agent, baseMs, read, first, 1.1, 3.4);
  addToolActivity(agent, baseMs, search, first, 1.2, 6.0);
  // The second call fell back to Sonnet, and its provider reported no thinking tokens.
  addModel(agent, baseMs, "model-completed-2", second, 7.2, 8.0, {
    model: "claude-opus-5-5",
    servedModel: "claude-sonnet-5-5",
    effort: "medium",
    durationMs: 800,
    firstEventMs: 410,
    inputTokens: 6_040,
    cacheReadInputTokens: 4_880,
    cacheCreationInputTokens: 760,
    outputTokens: 540,
    droppedThinkingBlocks: 1,
  });
  addToolActivity(agent, baseMs, patch, second, 8.1, 15.0);
  addToolActivity(agent, baseMs, note, second, 15.2, 18.0);
  addToolActivity(agent, baseMs, shortCommand, second, 18.1, 18.8);
  addToolActivity(agent, baseMs, longCommand, second, 18.9, 40.0, { progress: [{ at: 25, attempt: 2 }] });
  addToolActivity(agent, baseMs, commit, second, 18.2, 69.8);
}

// createSeedConversation returns the seeded Session's completed conversation:
// milestones, parallel tool calls and final timing ending at baseTime + 70 s.
export function createSeedConversation(baseTime: string): MockAgent {
  const baseMs = Date.parse(baseTime);
  const agent = createAgentState({ availableTools: [...SEED_TOOL_NAMES] });
  completed(agent, baseMs);
  return agent;
}
