// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The mock Agent's scripts: scripted replies to exact phrases, ordinary turns and waits.
// Development only; no model or tool is called.
import type { AgentRequirements__Output } from "@/lib/generated/protobuf/superverse/v2/AgentRequirements";
import type { AgentRequirementsDraft } from "@/lib/generated/protobuf/superverse/v2/AgentRequirementsDraft";
import { MOCK_FLOW_DESIGN, MOCK_QUESTIONS, MOCK_REQUIREMENTS } from "@/mock/fixture/agent-requests";
import { appendMessage, bumpWaitingRound, syncDescriptionCounts, type MockAgent } from "./agent-state";
import { nowIso } from "./clock";
import { recordAgentCommit } from "./publishing";
import { SCENARIO_MARKERS } from "./scenarios/catalog";
import { buildToolShowcaseMessages, createAgentState, mockMessage, mockToolResult } from "./seed";
import { holdWorkspace, pauseWorkspace } from "./seed";
import { deployPushedCommit, type MockStore } from "./store";

/** Exact phrases (trim + lower-case) that drive mock agent scripts. */
const MOCK_SCRIPT_PHRASES = {
  SHOW_TOOLS: "show tools",
  ASK_QUESTIONS: "ask questions",
  CONFIRM_REQUIREMENTS: "confirm requirements",
  CONFIRM_PLAIN_REQUIREMENTS: "confirm plain requirements",
  FINISH_BUILD: "finish build",
  NEED_APPROVAL: "need approval",
  TOOL_FAILED: "tool failed",
  SHOW_TODOS: "show todos",
  PAUSE_WORKSPACE: "pause workspace",
} as const;

type MockScriptPhrase = (typeof MOCK_SCRIPT_PHRASES)[keyof typeof MOCK_SCRIPT_PHRASES];

function matchScriptPhrase(content: string): MockScriptPhrase | null {
  const phraseKey = content.replace(SCENARIO_MARKERS, "").trim().toLowerCase();
  return Object.values(MOCK_SCRIPT_PHRASES).find((phrase) => phrase === phraseKey) ?? null;
}

/**
 * The next turn starts on the selected model. A turn that changes the model refreshes the context,
 * which a marker tells; the effort follows the model, and Haiku has none.
 */
export function startTurn(agent: MockAgent) {
  const { description } = agent;
  description.turn += 1;
  if (description.contextModel !== "AGENT_MODEL_UNSPECIFIED" && description.contextModel !== description.model)
    appendMessage(agent, mockMessage("ROLE_SYSTEM", "Context refreshed after a platform or model change"));
  description.contextModel = description.model;
  description.effort = description.model === "AGENT_MODEL_HAIKU_4_5" ? "" : "medium";
}

function applyShowTools(agent: MockAgent, includePending: boolean) {
  // The history shows a marker, and a hidden server entry before the tools leaves a gap in the sequences.
  appendMessage(agent, mockMessage("ROLE_SYSTEM", "Context compacted"));
  agent.nextSequence += 1;
  const base = buildToolShowcaseMessages(agent.nextSequence);
  for (const entry of base.messages) {
    agent.messages.push(entry);
  }
  agent.nextSequence = base.nextSequence;
  if (includePending) {
    appendMessage(
      agent,
      mockMessage("ROLE_ASSISTANT", "One more tool is still running.", {
        toolCalls: [{ id: `call-pending-${agent.nextSequence}`, name: "glob", target: "apps/**" }],
      }),
    );
  }
  agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
  agent.description.pendingApproval = null;
  agent.description.pendingUserInput = null;
  agent.description.todoList = null;
  syncDescriptionCounts(agent);
  bumpWaitingRound(agent);
}

/** A tool call fails for a platform reason; its row shows the code, and the model carries on alone. */
function applyToolFailure(agent: MockAgent) {
  const callId = `call-failed-${agent.nextSequence}`;
  appendMessage(
    agent,
    mockMessage("ROLE_ASSISTANT", "Searching the source for the signup form.", {
      toolCalls: [{ id: callId, name: "grep", target: "signup" }],
    }),
  );
  const toolExecution = { outcome: "OUTCOME_KNOWN_FAILURE", resultCode: "tool_error" } as const;
  appendMessage(agent, mockToolResult({ toolCallId: callId, toolName: "grep", toolExecution }));
  appendMessage(
    agent,
    mockMessage(
      "ROLE_ASSISTANT",
      "The source search is unavailable because of a problem on the platform. I will continue from the files I already know.",
    ),
  );
  agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
  agent.description.pendingApproval = null;
  agent.description.pendingUserInput = null;
  bumpWaitingRound(agent);
}

/** An ordinary turn: the message, one file listing, and a reply. */
function applyGenericTurn(agent: MockAgent, userContent: string) {
  appendMessage(agent, mockMessage("ROLE_USER", userContent));
  startTurn(agent);
  const callId = `call-generic-${agent.nextSequence}`;
  appendMessage(
    agent,
    mockMessage("ROLE_ASSISTANT", "Listing the workspace files.", {
      toolCalls: [{ id: callId, name: "glob", target: "*" }],
    }),
  );
  appendMessage(agent, mockToolResult({ toolCallId: callId, toolName: "glob" }));
  agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
  agent.description.pendingApproval = null;
  agent.description.pendingUserInput = null;
  syncDescriptionCounts(agent);
  bumpWaitingRound(agent);
}

/** The designed Steps of a card in the mock model's order: each Flow's Steps, which it writes first. */
function draftedFlows(spec: AgentRequirements__Output) {
  return spec.design ? MOCK_FLOW_DESIGN.flows.map(({ flowType, steps }) => ({ flowType, steps })) : [];
}

/** How long the agent writes a card: one second per designed Step, then the features, summary and title. */
function draftMs(spec: AgentRequirements__Output): number {
  return (draftedFlows(spec).reduce((sum, flow) => sum + flow.steps.length, 0) + 5) * 1_000;
}

/** The agent starts writing a requirements card, whose draft the Session Watch streams until it is recorded. */
function draftRequirementsCard(agent: MockAgent, spec: AgentRequirements__Output) {
  agent.description.status = "STATUS_CALLING_MODEL";
  agent.requirementsDraft = {
    callId: `confirm-${agent.nextSequence}`,
    turn: agent.description.turn,
    baseRevision: agent.description.confirmedRequirements?.revision ?? 0,
    startedAtMs: Date.parse(nowIso()),
    spec,
  };
}

/**
 * The card the agent is writing as far as it wrote it, which the Session Watch streams; null when it writes
 * none. Like the mock model, it writes the design's Steps first, one a second, then the features and
 * connections, the summary and the title last.
 */
export function mockRequirementsDraft(store: MockStore, projectId: string): AgentRequirementsDraft | null {
  const draft = store.agents.get(projectId)?.requirementsDraft;
  if (!draft) return null;
  const { spec } = draft;
  let left = Math.floor((Date.parse(nowIso()) - draft.startedAtMs) / 1_000);
  const flows = draftedFlows(spec).flatMap(({ flowType, steps }) => {
    const written = steps.slice(0, Math.max(0, left)).map((step) => step.stepType);
    left -= steps.length;
    // The mock's design has no failure routes, so every drafted Step is in its main story.
    return written.length > 0 ? [{ flowType, stepTypes: written, failureHandlingStepTypes: [] }] : [];
  });
  return {
    callId: draft.callId,
    turn: draft.turn,
    baseRevision: draft.baseRevision,
    title: left >= 3 ? spec.title : "",
    summary: left >= 2 ? spec.summary : "",
    features: left >= 1 ? spec.features : [],
    connections: left >= 1 ? spec.connections.map(({ need, connector }) => ({ need, connector })) : [],
    designSummary: flows.length > 0 ? (spec.design?.summary ?? "") : "",
    flows,
    complete: false,
  };
}

/** Records the card the agent wrote once its draft has streamed, as the Session records the finished call. */
export function advanceMockDraft(store: MockStore, projectId: string) {
  const agent = store.agents.get(projectId);
  const draft = agent?.requirementsDraft;
  if (!agent || !draft || Date.parse(nowIso()) - draft.startedAtMs < draftMs(draft.spec)) return;
  agent.requirementsDraft = null;
  recordRequirementsCard(agent, draft.spec, draft.callId);
  syncDescriptionCounts(agent);
}

/**
 * The agent's requirements card is recorded as the next version, and it goes on building from it; the mock
 * builds it only on `finish build`.
 */
function recordRequirementsCard(agent: MockAgent, spec: AgentRequirements__Output, callId: string) {
  const revision = (agent.description.confirmedRequirements?.revision ?? 0) + 1;
  appendMessage(
    agent,
    mockMessage("ROLE_ASSISTANT", "Here is what I will build.", {
      toolCalls: [{ id: callId, name: "confirm_requirements", target: spec.title }],
    }),
  );
  appendMessage(agent, mockToolResult({ toolCallId: callId, toolName: "confirm_requirements" }));
  agent.description.confirmedRequirements = { revision, spec, confirmedAt: nowIso(), confirmedBy: null };
  agent.description.todoList = {
    revision: (agent.description.todoList?.revision ?? 0) + 1,
    items: [
      { content: "Build the subscription page", status: "STATUS_IN_PROGRESS" },
      { content: "Build the review and send flow", status: "STATUS_PENDING" },
    ],
  };
  appendMessage(
    agent,
    mockMessage(
      "ROLE_ASSISTANT",
      `I am building version ${String(revision)} of these requirements. Send a message to change them.`,
    ),
  );
  agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
  bumpWaitingRound(agent);
}

/** A change to the recorded card, as a message: the agent records a revised version with the change as one more feature. */
function reviseRequirements(agent: MockAgent, spec: AgentRequirements__Output, content: string) {
  appendMessage(agent, mockMessage("ROLE_USER", content));
  startTurn(agent);
  const change = content.replace(SCENARIO_MARKERS, "").trim().slice(0, 200);
  const feature = { id: `R${String(spec.features.length + 1)}`, text: change };
  draftRequirementsCard(agent, { ...spec, features: [...spec.features, feature] });
}

/**
 * The agent finishes building the recorded card: every designed Step is written and the design is built at
 * the Session's head, which it stays until the Preview's processes are built from that commit.
 */
function finishDesignBuild(agent: MockAgent, headCommit: string) {
  const current = agent.description.confirmedRequirements;
  if (current?.spec?.design) {
    const written = MOCK_FLOW_DESIGN.flows.flatMap(({ flowType, steps }) =>
      steps.map((s) => `${flowType}.${s.stepType}`),
    );
    current.spec = { ...current.spec, designBuilt: true, designBuiltCommit: headCommit, designWrittenSteps: written };
    appendMessage(agent, mockMessage("ROLE_ASSISTANT", `I built version ${String(current.revision)} as recorded.`));
  } else appendMessage(agent, mockMessage("ROLE_ASSISTANT", "No recorded design is waiting to be built."));
  agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
  bumpWaitingRound(agent);
}

/**
 * The start of a Session created from an example, as the server's start Step records it: a system notice
 * that Studio shows as the agent's greeting, the example's card as version 1, recorded with no call, and the
 * one Preview deployment of the setup commit, whose processes the Flow tab then draws.
 */
export function startFromMockExample(
  store: MockStore,
  projectId: string,
  title: string,
  spec: AgentRequirements__Output,
) {
  const agent = store.agents.get(projectId);
  if (!agent) return;
  appendMessage(
    agent,
    mockMessage(
      "ROLE_SYSTEM",
      `This project starts from the ${title} example. Its processes are built and drawn on the Flow tab, and Try it gets ready in a few minutes. Tell me what you want to change.`,
    ),
  );
  agent.description.confirmedRequirements = { revision: 1, spec, confirmedAt: nowIso(), confirmedBy: null };
  const session = store.sessions.get(projectId);
  if (session) deployPushedCommit(store, session, agent);
  syncDescriptionCounts(agent);
}

export function sendMessage(store: MockStore, projectId: string, content: string): boolean {
  const agent = store.agents.get(projectId);
  if (!agent) return false;
  const phrase = matchScriptPhrase(content);
  // A scripted reply follows the user's message; an ordinary turn appends its own.
  if (phrase !== null) {
    appendMessage(agent, mockMessage("ROLE_USER", content));
    startTurn(agent);
  }

  const session = store.sessions.get(projectId);
  const current = agent.description.confirmedRequirements;
  if (phrase === MOCK_SCRIPT_PHRASES.SHOW_TOOLS) {
    applyShowTools(agent, true);
    if (session) holdWorkspace(session);
  } else if (phrase === MOCK_SCRIPT_PHRASES.PAUSE_WORKSPACE) {
    // Stands in for the Sandbox deadline passing while the Session is idle.
    appendMessage(agent, mockMessage("ROLE_ASSISTANT", "The workspace was idle past its deadline, so it is paused."));
    agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
    if (session) pauseWorkspace(session);
    bumpWaitingRound(agent);
  } else if (phrase === MOCK_SCRIPT_PHRASES.ASK_QUESTIONS) {
    // The history keeps the call with its first question as the target, never its arguments.
    const callId = `ask-${agent.nextSequence}`;
    appendMessage(
      agent,
      mockMessage("ROLE_ASSISTANT", "I have a few questions first.", {
        toolCalls: [{ id: callId, name: "request_user_input", target: MOCK_QUESTIONS[0].question }],
      }),
    );
    agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
    agent.description.pendingUserInput = {
      callId,
      kind: "KIND_QUESTION",
      questions: MOCK_QUESTIONS,
      requirements: null,
    };
    bumpWaitingRound(agent);
  } else if (phrase === MOCK_SCRIPT_PHRASES.CONFIRM_REQUIREMENTS) {
    draftRequirementsCard(agent, MOCK_REQUIREMENTS);
  } else if (phrase === MOCK_SCRIPT_PHRASES.CONFIRM_PLAIN_REQUIREMENTS) {
    // A card without a Flow design, which the Flow tab shows without a drawing.
    draftRequirementsCard(agent, { ...MOCK_REQUIREMENTS, design: null });
  } else if (phrase === MOCK_SCRIPT_PHRASES.FINISH_BUILD) {
    finishDesignBuild(agent, session?.headCommitSha ?? "");
  } else if (phrase === MOCK_SCRIPT_PHRASES.NEED_APPROVAL) {
    agent.description.status = "STATUS_WAITING_FOR_TOOL_APPROVAL";
    agent.description.pendingApproval = {
      callId: `approve-${agent.nextSequence}`,
      toolName: "bash",
      argumentsJson: JSON.stringify({ command: "rm -rf /tmp/mock", description: "Remove the mock directory" }),
    };
    bumpWaitingRound(agent);
  } else if (phrase === MOCK_SCRIPT_PHRASES.TOOL_FAILED) {
    applyToolFailure(agent);
  } else if (phrase === MOCK_SCRIPT_PHRASES.SHOW_TODOS) {
    agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
    agent.description.todoList = {
      revision: (agent.description.todoList?.revision ?? 0) + 1,
      items: [
        { content: "Scaffold the inbox layout", status: "STATUS_COMPLETED" },
        { content: "Add search and tags", status: "STATUS_IN_PROGRESS" },
        { content: "Verify responsive UI", status: "STATUS_PENDING" },
      ],
    };
    bumpWaitingRound(agent);
  } else if (phrase === null && current?.spec && !current.spec.designBuilt) {
    // While the agent builds a recorded card, which the mock never finishes, a message changes it.
    reviseRequirements(agent, current.spec, content);
  } else {
    applyGenericTurn(agent, content);
    if (session) {
      holdWorkspace(session);
      recordAgentCommit(store, session, content);
      deployPushedCommit(store, session, agent);
    }
  }

  syncDescriptionCounts(agent);
  return true;
}

type PendingWait = "pendingUserInput" | "pendingApproval";

/** Ends a pending wait with one assistant reply. */
function resolveWait(agent: MockAgent, field: PendingWait, reply: string) {
  agent.description[field] = null;
  agent.description.status = "STATUS_WAITING_FOR_MESSAGE";
  appendMessage(agent, mockMessage("ROLE_ASSISTANT", reply));
  bumpWaitingRound(agent);
  return true;
}

export function answerQuestions(store: MockStore, projectId: string, callId: string): boolean {
  const agent = store.agents.get(projectId);
  const pending = agent?.description.pendingUserInput;
  if (!agent || pending?.callId !== callId || pending.kind !== "KIND_QUESTION") return false;
  appendMessage(agent, mockMessage("ROLE_USER", "Answered the questions above.", { answeredInputCallId: callId }));
  appendMessage(agent, mockToolResult({ toolCallId: callId, toolName: "request_user_input" }));
  return resolveWait(agent, "pendingUserInput", "Thanks — continuing with your answers.");
}

export function approveTool(store: MockStore, projectId: string, callId: string, approved: boolean): boolean {
  const agent = store.agents.get(projectId);
  if (agent?.description.pendingApproval?.callId !== callId) return false;
  return resolveWait(
    agent,
    "pendingApproval",
    approved ? "Tool approved and executed in mock mode." : "Tool rejected.",
  );
}

/** The Agent of a new Project's Session: an empty conversation that waits for its first message. */
export function createAgent(): MockAgent {
  return createAgentState({ availableTools: ["read", "edit", "glob", "bash"] });
}
