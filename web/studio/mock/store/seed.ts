// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Explicit development seed data; never a production or acceptance backend.
import type { AgentModel__Output } from "@/lib/generated/protobuf/superverse/v2/AgentModel";
import type { MockAgent, MockAgentMessage, MockSequencedMessage } from "./agent-state";
import { mockPhases, nowIso, randomHex } from "./clock";
import type { MockProject, MockSession } from "./store";

/** The model a new mock Session starts on, the strongest one the picker offers. */
const MOCK_AGENT_MODEL: AgentModel__Output = "AGENT_MODEL_OPUS_5_5";

/** One durable conversation message with no tool calls unless extra adds them. */
export function mockMessage(
  role: MockAgentMessage["role"],
  content: string,
  extra: Partial<MockAgentMessage> = {},
): MockAgentMessage {
  return { role, content, toolCalls: [], createdAt: nowIso(), ...extra };
}

/** A tool result: it has no content, only the outcome (a success unless extra says otherwise). */
export function mockToolResult(extra: Partial<MockAgentMessage>): MockAgentMessage {
  return mockMessage("ROLE_TOOL", "", { toolExecution: { outcome: "OUTCOME_SUCCEEDED" }, ...extra });
}

const SEED_PROJECT_ID = "11111111-1111-4111-8111-111111111111";
const FAILED_PROJECT_ID = "33333333-3333-4333-8333-333333333333";
const OTHER_PROJECT_ID = "44444444-4444-4444-8444-444444444444";
/** The seed Session's head, which the seed Project's main branch also lists. */
export const SEED_HEAD_COMMIT = "cccccccccccccccccccccccccccccccccccccccc";

export function buildToolShowcaseMessages(startSequence = 1) {
  const createdAt = nowIso();
  const messages: MockSequencedMessage[] = [];
  let sequence = startSequence;

  messages.push({
    sequence: sequence++,
    message: mockMessage("ROLE_USER", "Show me the seeded tool calls.", { createdAt }),
  });

  const calls = [
    { id: "call-glob", name: "glob", target: "*.md", outcome: "OUTCOME_SUCCEEDED", durationMs: 120 },
    { id: "call-read", name: "read", target: "README.md", outcome: "OUTCOME_SUCCEEDED", durationMs: 80 },
    { id: "call-edit", name: "edit", target: "README.md", outcome: "OUTCOME_SUCCEEDED", durationMs: 340 },
    {
      id: "call-exec-short",
      name: "bash",
      target: "Show the changed files",
      outcome: "OUTCOME_SUCCEEDED",
      durationMs: 410,
    },
    {
      id: "call-exec-long",
      name: "bash",
      target: "Run the tests",
      outcome: "OUTCOME_KNOWN_FAILURE",
      resultCode: "command_failed",
      durationMs: 21_000,
    },
  ] as const;

  messages.push({
    sequence: sequence++,
    message: mockMessage("ROLE_ASSISTANT", "Running file and shell tools.", {
      createdAt,
      toolCalls: calls.map(({ id, name, target }) => ({ id, name, target })),
    }),
  });
  for (const call of calls) {
    const { id, name, outcome, durationMs } = call;
    const resultCode = "resultCode" in call ? call.resultCode : "";
    messages.push({
      sequence: sequence++,
      message: mockToolResult({
        createdAt,
        toolCallId: id,
        toolName: name,
        toolExecution: { outcome, durationMs, resultCode },
      }),
    });
  }

  return { messages, nextSequence: sequence };
}

export function createSeedProject(): MockProject {
  const timestamp = nowIso();
  return {
    id: SEED_PROJECT_ID,
    revision: 1,
    displayName: "Mock inbox app",
    state: "ACTIVE",
    repository: {
      githubRepositoryId: 42,
      owner: "superdurable-mock",
      name: "mock-inbox-app",
      defaultBranch: "main",
      state: "ACTIVE",
    },
    createdAt: timestamp,
    updatedAt: timestamp,
  };
}

export function createFailedSeedProject(): MockProject {
  const project = createSeedProject();
  return {
    ...project,
    id: FAILED_PROJECT_ID,
    displayName: "Failed template app",
    state: "SETUP_FAILED",
    repository: {
      ...project.repository,
      githubRepositoryId: 43,
      name: "failed-template-app",
      state: "FAILED",
    },
  };
}

/** A Project of the seed Teamspace in which a signed-in account has no role. */
export function createOtherSeedProject(): MockProject {
  return { ...createSeedProject(), id: OTHER_PROJECT_ID, displayName: "Supplier onboarding" };
}

/** The Session of a Project, with a ready workspace. */
export function createSeedSession(projectId: string): MockSession {
  const timestamp = nowIso();
  const workspaceRequestId = `mock-workspace-${projectId}`;
  return {
    revision: 1,
    projectId,
    headCommitSha: SEED_HEAD_COMMIT,
    workspace: {
      workspaceRequestId,
      state: "READY",
      generation: 1,
      expiresAt: mockSandboxDeadline(),
      phaseTimings: workspaceStartPhases(),
    },
    state: "ACTIVE",
    preview: {
      state: "STOPPED",
      workspaceRequestId,
      generation: 1,
      revision: 0,
      preparedSource: null,
      sourceCommit: "",
      definitionRevision: "",
      servingRevision: 0,
      url: null,
      automaticDeployment: null,
      configurationGaps: [],
      phaseTimings: [],
    },
    createdAt: timestamp,
    updatedAt: timestamp,
  };
}

/** The phases of a workspace start, which ended now: the Sandbox provisioned, then the repository checked out. */
const workspaceStartPhases = () => mockPhases(["provision", 0, 11_800], ["checkout", 11_800, 2_300]);

/** The phases of a Preview start, which ended now: the five a Preview start records, in order. */
export const previewStartPhases = () =>
  mockPhases(
    ["configuration", 0, 600],
    ["environment", 600, 3_400],
    ["grant", 4_000, 900],
    ["application", 4_900, 21_500],
    ["route", 26_400, 1_100],
  );

/** How long the mock Sandbox stays after the Agent last used it, as the server's default hold. */
const MOCK_SANDBOX_HOLD_MS = 60 * 60 * 1_000;

/** The mock Sandbox deadline of a workspace the Agent uses now. */
function mockSandboxDeadline(): string {
  return nowIso(MOCK_SANDBOX_HOLD_MS);
}

/**
 * The Agent used the workspace: a paused one starts a new instance on its kept volume, and the
 * Sandbox deadline moves to one hold from now, as the Session's dispatch of a workspace tool does.
 */
export function holdWorkspace(session: MockSession) {
  if (session.workspace.state === "CLOSED") {
    session.workspace.workspaceRequestId = `mock-workspace-${session.projectId}-${randomHex(6)}`;
    session.workspace.state = "READY";
    session.workspace.phaseTimings = workspaceStartPhases();
  }
  session.workspace.expiresAt = mockSandboxDeadline();
}

/** The Session reclaims its idle Sandbox at the deadline: the instance and its Preview stop, the files stay. */
export function pauseWorkspace(session: MockSession) {
  session.workspace.state = "CLOSED";
  session.workspace.expiresAt = null;
  session.preview.state = "STOPPED";
  session.preview.url = null;
}

/** A mock Agent waiting for a message after these messages, as the Session snapshot reads it. */
export function createAgentState({
  availableTools,
  messages = [],
}: {
  availableTools: string[];
  messages?: MockSequencedMessage[];
}): MockAgent {
  const lastSequence = messages.at(-1)?.sequence ?? 0;
  return {
    messages,
    nextSequence: lastSequence + 1,
    queued: [],
    steered: [],
    description: {
      status: "STATUS_WAITING_FOR_MESSAGE",
      waitingInputRound: 1,
      model: MOCK_AGENT_MODEL,
      modelRevision: "0",
      contextModel: MOCK_AGENT_MODEL,
      effort: "medium",
      firstRetainedSequence: 1,
      lastSequence,
      pendingApproval: null,
      pendingTimer: null,
      pendingUserInput: null,
      todoList: null,
      confirmedRequirements: null,
      pendingQueuedMessageCount: 0,
      pendingSteeredMessageCount: 0,
      availableTools,
      turn: 0,
      backgroundCommands: [],
      subagents: [],
      isPreviewAutoDeployEnabled: true,
    },
    activity: [],
  };
}
