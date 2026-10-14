// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock Agent's tasks: background commands and subagents, their scripts and the Stop button.
// Development only; no command or subagent runs.
import { appendMessage, bumpWaitingRound, mockNotification, type MockAgent } from "@/mock/store/agent-state";
import { startTurn } from "@/mock/store/agent-scripts";
import { nowIso, randomHex } from "@/mock/store/clock";
import { SCENARIO_MARKERS } from "@/mock/store/scenarios/catalog";
import { mockMessage, mockToolResult } from "@/mock/store/seed";
import { holdWorkspace } from "@/mock/store/seed";
import type { MockStore } from "@/mock/store/store";

/** Exact phrases (trim + lower-case) that drive the task scripts. */
const MOCK_TASK_PHRASES = {
  RUN_IN_BACKGROUND: "run in background",
  FINISH_TASK: "finish task",
  START_SUBAGENT: "start subagent",
  FINISH_SUBAGENT: "finish subagent",
} as const;

type MockTaskPhrase = (typeof MOCK_TASK_PHRASES)[keyof typeof MOCK_TASK_PHRASES];

/**
 * Runs the task script of a message, which follows the user's message in a new turn; null when the
 * message is no task phrase, which the other scripts then answer.
 */
export function sendTaskScript(store: MockStore, projectId: string, content: string): boolean | null {
  const key = content.replace(SCENARIO_MARKERS, "").trim().toLowerCase();
  const phrase = Object.values(MOCK_TASK_PHRASES).find((candidate): candidate is MockTaskPhrase => candidate === key);
  const agent = store.agents.get(projectId);
  if (phrase === undefined || !agent) return phrase === undefined ? null : false;
  appendMessage(agent, mockMessage("ROLE_USER", content));
  startTurn(agent);
  const session = store.sessions.get(projectId);
  switch (phrase) {
    case MOCK_TASK_PHRASES.RUN_IN_BACKGROUND:
      startBackgroundTask(agent);
      if (session) holdWorkspace(session);
      break;
    case MOCK_TASK_PHRASES.FINISH_TASK:
      finishBackgroundTask(agent);
      break;
    case MOCK_TASK_PHRASES.START_SUBAGENT:
      startSubagent(agent);
      if (session) holdWorkspace(session);
      break;
    case MOCK_TASK_PHRASES.FINISH_SUBAGENT:
      finishSubagent(agent);
      break;
  }
  agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
  bumpWaitingRound(agent);
  return true;
}

/** The Agent starts the test suite as a background task, which runs on after the turn ends. */
function startBackgroundTask(agent: MockAgent) {
  const callId = `call-background-${agent.nextSequence}`;
  const description = "Run the test suite";
  appendMessage(
    agent,
    mockMessage("ROLE_ASSISTANT", "Running the test suite in the background.", {
      toolCalls: [{ id: callId, name: "bash", target: description }],
    }),
  );
  appendMessage(agent, mockToolResult({ toolCallId: callId, toolName: "bash" }));
  agent.description.backgroundCommands.push({
    taskId: `cmd-${randomHex(12)}`,
    description,
    status: "STATUS_RUNNING",
    endReason: "END_REASON_UNSPECIFIED",
    outputBytes: 0,
    startedAt: nowIso(),
  });
  appendMessage(
    agent,
    mockMessage("ROLE_ASSISTANT", "The tests run in the background; I will read them when they finish."),
  );
}

/** The oldest running background task exits; its notification opens an automatic turn. */
function finishBackgroundTask(agent: MockAgent) {
  const command = agent.description.backgroundCommands.find((candidate) => candidate.status === "STATUS_RUNNING");
  if (command) {
    Object.assign(command, {
      status: "STATUS_EXITED",
      endReason: "END_REASON_EXIT",
      exitCode: 0,
      outputBytes: 2048,
      endedAt: nowIso(),
    });
    const summary = `Background command "${command.description}" exited with code 0.`;
    const notification = mockNotification({
      id: `task/${command.taskId}/exited`,
      kind: "KIND_TASK",
      source: "SOURCE_COMMAND",
      taskId: command.taskId,
      taskStatus: "TASK_STATUS_EXITED",
      summary,
    });
    appendMessage(agent, mockMessage("ROLE_NOTIFICATION", summary, { notification }));
    startTurn(agent);
    appendMessage(
      agent,
      mockMessage("ROLE_ASSISTANT", "The test suite finished in the background, and every test passed."),
    );
  }
}

/**
 * The Agent delegates an investigation to a subagent, which runs on after the turn ends; its live
 * status line arrives on the Watch with its task ID.
 */
function startSubagent(agent: MockAgent) {
  const callId = `call-agent-${agent.nextSequence}`;
  const description = "Investigate how the inbox loads messages";
  const taskId = `agent-${randomHex(8)}`;
  appendMessage(
    agent,
    mockMessage("ROLE_ASSISTANT", "Delegating the investigation to a subagent.", {
      toolCalls: [{ id: callId, name: "start_agent", target: description }],
    }),
  );
  appendMessage(agent, mockToolResult({ toolCallId: callId, toolName: "start_agent" }));
  agent.description.subagents.push({
    taskId,
    kind: "KIND_INVESTIGATE",
    description,
    status: "STATUS_RUNNING",
    startedAt: nowIso(),
    summary: "",
    changedFiles: 0,
    insertions: 0,
    deletions: 0,
    lost: false,
  });
  agent.activity.push({
    createdAt: nowIso(),
    source: taskId,
    taskId,
    value: { kind: "KIND_TOOL_PROGRESS", message: "Running grep." },
  });
  appendMessage(agent, mockMessage("ROLE_ASSISTANT", "The subagent works in the background; its report will arrive."));
}

/** The oldest running subagent completes with saved changes; its notification opens an automatic turn. */
function finishSubagent(agent: MockAgent) {
  const subagent = agent.description.subagents.find((candidate) => candidate.status === "STATUS_RUNNING");
  if (!subagent) return;
  const summary = `Subagent "${subagent.description}" finished. It changed 2 file(s), which wait to be merged.`;
  Object.assign(subagent, {
    status: "STATUS_COMPLETED",
    finishedAt: nowIso(),
    summary,
    changedFiles: 2,
    insertions: 14,
    deletions: 3,
  });
  const notification = mockNotification({
    id: `task/${subagent.taskId}/completed`,
    kind: "KIND_TASK",
    source: "SOURCE_SUBAGENT",
    taskId: subagent.taskId,
    taskStatus: "TASK_STATUS_COMPLETED",
    summary,
  });
  appendMessage(agent, mockMessage("ROLE_NOTIFICATION", summary, { notification }));
  startTurn(agent);
  appendMessage(agent, mockMessage("ROLE_ASSISTANT", "The subagent found where the inbox loads its messages."));
}

/**
 * The Stop button: a Stop of an earlier turn, or with nothing running, is refused; otherwise the
 * turn ends, its waits are dropped, every running background task stops with no notification, and
 * every running subagent stops; the mock leaves out the stopped report a real one sends.
 */
export function interruptAgent(store: MockStore, projectId: string, turn: number): boolean {
  const agent = store.agents.get(projectId);
  if (!agent) return false;
  const { description } = agent;
  const running = description.backgroundCommands.filter((command) => command.status === "STATUS_RUNNING");
  const subagents = description.subagents.filter((subagent) => subagent.status === "STATUS_RUNNING");
  const idle = description.status === "STATUS_WAITING_FOR_MESSAGE" && running.length + subagents.length === 0;
  if (turn !== description.turn || idle) return false;
  for (const command of running)
    Object.assign(command, { status: "STATUS_STOPPED", endReason: "END_REASON_STOPPED", endedAt: nowIso() });
  for (const subagent of subagents)
    Object.assign(subagent, {
      status: "STATUS_STOPPED",
      finishedAt: nowIso(),
      summary: `Subagent "${subagent.description}" was stopped.`,
    });
  description.pendingApproval = null;
  description.pendingTimer = null;
  description.status = "STATUS_WAITING_FOR_MESSAGE";
  appendMessage(agent, mockMessage("ROLE_SYSTEM", "Stopped by user"));
  appendMessage(agent, mockMessage("ROLE_SYSTEM", "Automatic continuation paused; reply to continue"));
  bumpWaitingRound(agent);
  return true;
}
