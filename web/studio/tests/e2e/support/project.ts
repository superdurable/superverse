// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { randomBytes } from "node:crypto";
import type { AgentMessage__Output } from "@/lib/generated/protobuf/superverse/v2/AgentMessage";
import type { AgentModel__Output } from "@/lib/generated/protobuf/superverse/v2/AgentModel";
import type { PhaseTiming__Output } from "@/lib/generated/protobuf/superverse/v2/PhaseTiming";
import type { ProjectSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectSnapshot";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import type { SessionStatus__Output } from "@/lib/generated/protobuf/superverse/v2/SessionStatus";
import { delay } from "@/lib/shared/async";
import { timestampToISO } from "@/lib/shared/protobuf";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";
import { activateAcceptanceCredential } from "./credentials/project-model-credential";
import { branchHead } from "./repository-host";
import { chooseOption, rpc, rpcAnswered, rpcResult } from "./studio-api";

/** SUPERVERSE_E2E_MODEL_TARGET when it names the mock model, which a lane offers or not. */
export const MOCK_TARGET = "mock";
/** The mock model's option in the model picker. */
export const MOCK_MODEL_OPTION = "Mock";
/**
 * The models that SUPERVERSE_E2E_MODEL_TARGET may name, with their enum value and the option name of the
 * model picker: the same names as `MODEL_TARGETS` of infra-as-code/scripts/kind/server_config.py, which
 * `kind e2e` validates on the acceptance profile.
 */
const MODEL_TARGETS: Record<string, { enumValue: AgentModel__Output; picker: string }> = {
  [MOCK_TARGET]: { enumValue: "AGENT_MODEL_MOCK", picker: MOCK_MODEL_OPTION },
  "anthropic/claude-opus-5-5": { enumValue: "AGENT_MODEL_OPUS_5_5", picker: "Claude Opus 5.5" },
  "anthropic/claude-sonnet-5-5": { enumValue: "AGENT_MODEL_SONNET_5_5", picker: "Claude Sonnet 5.5" },
  "anthropic/claude-haiku-4-5": { enumValue: "AGENT_MODEL_HAIKU_4_5", picker: "Claude Haiku 4.5" },
};
const REAL_MODEL_TARGETS = Object.keys(MODEL_TARGETS).filter((model) => model !== MOCK_TARGET);
function modelTarget(model: string) {
  const target = MODEL_TARGETS[model];
  if (!target) throw new Error(`SUPERVERSE_E2E_MODEL_TARGET must be one of ${Object.keys(MODEL_TARGETS).join(", ")}`);
  return target;
}
/**
 * The AgentModel enum value of a server model such as the `anthropic/claude-sonnet-5-5` that
 * SUPERVERSE_E2E_MODEL_TARGET names, for comparing with the Session's selected model.
 */
export function agentModelEnum(model: string): AgentModel__Output {
  return modelTarget(model).enumValue;
}
/** The option of the creation page's model picker for such a server model. */
function pickerModel(model: string): string {
  return modelTarget(model).picker;
}
export async function readProject(page: Page, projectId: string): Promise<ProjectSnapshot__Output> {
  const { project } = await rpcResult(page.request, "GetProject", { projectId });
  if (!project) throw new Error("GetProject returned no Project");
  expect(project.projectId).toBe(projectId);
  return project;
}
/** The Project's one Session, which its ProjectFlow started once the Project became READY. */
export async function readSession(page: Page, projectId: string): Promise<SessionSnapshot__Output> {
  const { session } = await rpcResult(page.request, "GetSession", { projectId });
  if (!session) throw new Error("GetSession returned no Session");
  expect(session.projectId).toBe(projectId);
  return session;
}
/**
 * The phase timings of a settled resource: each phase appears once with its start and an ended duration, and the
 * phases the Flow always records are present.
 */
export function expectPhaseTimings(timings: readonly PhaseTiming__Output[], required: readonly string[]) {
  const phases = timings.map((timing) => timing.phase);
  expect(new Set(phases).size, "each phase is measured once").toBe(phases.length);
  for (const timing of timings) {
    expect(timing.phase).not.toBe("");
    expect(Date.parse(timestampToISO(timing.startedAt) ?? ""), `phase ${timing.phase} has a start`).toBeGreaterThan(0);
    expect(timing.durationMs, `phase ${timing.phase} of a settled resource has ended`).toMatch(/^[1-9][0-9]*$/);
  }
  expect(phases).toEqual(expect.arrayContaining([...required]));
}

/**
 * Keeps the Project's READY workspace running for seven days with "Keep running for 7 days" in Project
 * details' Workspace section, requires its Sandbox deadline to be about seven days ahead, and returns to the
 * page it started on.
 */
export async function keepWorkspaceRunning(page: Page, projectId: string) {
  const back = page.url();
  await page.goto(`/p/${projectId}`);
  const keep = page
    .getByRole("region", { name: "Workspace", exact: true })
    .getByRole("button", { name: "Keep running for 7 days", exact: true });
  await expect(keep).toBeEnabled({ timeout: 60_000 });
  const clicked = Date.now();
  const [response] = await Promise.all([page.waitForResponse(rpcAnswered("RenewWorkspace")), keep.click()]);
  expect(response.ok()).toBe(true);
  const answer: RpcOutput<"RenewWorkspace"> = await response.json();
  expect(answer.renewed, "The READY workspace must be renewed").toBe(true);
  await page.goto(back);
  const deadline = Number((await readSession(page, projectId)).workspace?.expiresAt?.seconds) * 1000;
  const week = 7 * 24 * 60 * 60 * 1000;
  expect(deadline).toBeGreaterThanOrEqual(clicked + week - 60_000);
  expect(deadline).toBeLessThanOrEqual(Date.now() + week + 60_000);
}
/** The Session's head is the commit on its Project's default branch on the repository host; returns that repository. */
export async function expectPushedHead(project: ProjectSnapshot__Output, session: SessionSnapshot__Output) {
  expect(session.headCommit).toMatch(/^[0-9a-f]{40}$/);
  if (!project.repository) throw new Error("The Project has no accepted repository");
  expect(await branchHead(project.repository.fullName, project.repository.defaultBranch)).toBe(session.headCommit);
  return project.repository;
}
/**
 * What a spec does on the creation page to choose its Session's first turn, before it presses
 * Build: it types a prompt, or on the mock model chooses the model and a scenario's first input.
 */
export type FirstTurnChoice = (page: Page) => Promise<void>;
/** SUPERVERSE_E2E_MODEL_TARGET, trimmed: the platform developer's decision, or undefined when unset or empty. */
export function modelTargetDecision(): string | undefined {
  return process.env.SUPERVERSE_E2E_MODEL_TARGET?.trim() || undefined;
}
/** The failure of a spec that needs the developer's decision on a lane that offers no mock model. */
export function modelTargetRequired(): Error {
  return new Error(
    `SUPERVERSE_E2E_MODEL_TARGET is required on a lane without the mock model; the platform developer chooses the model (one of ${Object.keys(MODEL_TARGETS).join(", ")})`,
  );
}
/**
 * The server model that the real-model specs run. A real-model run is never automatic: the platform
 * developer sets SUPERVERSE_E2E_MODEL_TARGET, and there is no default. Unset or empty fails the spec
 * here, and an unknown model fails it in `pickerModel`. A spec that has no mock scenario skips on
 * `mock` before it asks, so `mock` reaching here fails.
 */
export function realModelTarget(): string {
  const target = modelTargetDecision();
  if (!target) throw modelTargetRequired();
  if (target === MOCK_TARGET)
    throw new Error(
      `SUPERVERSE_E2E_MODEL_TARGET is ${MOCK_TARGET}, but this spec needs a real model (one of ${REAL_MODEL_TARGETS.join(", ")})`,
    );
  return target;
}
/** The start page's composer, which takes the description of the process as the first message. */
const START_COMPOSER_PLACEHOLDER =
  "For example: when a customer asks for a refund by email, a support lead approves it in Slack and Stripe pays it back…";

/**
 * Chooses `realModelTarget()` in the picker and types the prompt as the first message, which Build
 * sends with the model in CreateProject. The model is checked when this is called, before the page is touched.
 */
export function typePrompt(prompt: string): FirstTurnChoice {
  const model = pickerModel(realModelTarget());
  return async (page) => {
    await chooseOption(page.getByRole("button", { name: "Model", exact: true }), model);
    await page.getByPlaceholder(START_COMPOSER_PLACEHOLDER).fill(prompt);
  };
}
/**
 * How many copies of the creation's first message the Session holds: queued under its message ID, or in the
 * history as a user message with its text. The Session admits it once, at its start, whatever retries came.
 */
function firstMessageCopies(session: SessionSnapshot__Output, request: RpcInput<"CreateProject">): number {
  const queued = (session.agent?.queued ?? []).filter((item) => item.messageId === request.messageId).length;
  const sent = (session.agent?.history?.messages ?? []).filter(
    (item) => item.message?.role === "ROLE_USER" && item.message.content === request.initialMessage,
  ).length;
  return queued + sent;
}
/**
 * Creates a Project in the Teamspace through its start form and returns once it is READY and its Session, which
 * ProjectFlow starts then, exists. The name and the model are under the form's Advanced, which the step opens.
 * CreateProject carries the first message and the model chosen on the start form, so the Session starts on
 * that model with the message admitted once, without the page; Studio opens the Project's first conversation at
 * once, and the spec then waits for that first turn.
 */
export async function createProject(
  page: Page,
  teamSpaceId: string,
  chooseFirstTurn: FirstTurnChoice,
  testInfo: TestInfo,
  onAccepted?: (projectId: string) => Promise<void>,
) {
  const name = `V2 E2E ${randomBytes(4).toString("hex")}`;
  const start = `/t/${teamSpaceId}/projects/start`;
  if (new URL(page.url()).pathname !== start) await page.goto(start);
  await expect(page.getByRole("heading", { name: "What process should Dex run?" })).toBeVisible();
  await expect(page.getByText("Existing repository", { exact: true })).toHaveCount(0);
  // Advanced stays open in this browser once opened.
  const advanced = page.getByRole("button", { name: "Advanced", exact: true });
  if ((await advanced.getAttribute("aria-expanded")) !== "true") await advanced.click();
  await page.getByLabel("Project name", { exact: true }).fill(name);
  await chooseFirstTurn(page);
  const responsePromise = page.waitForResponse(
    (response) => rpcAnswered("CreateProject")(response) && response.status() !== 503,
  );
  await page.getByRole("button", { name: "Build", exact: true }).click();
  const response = await responsePromise;
  expect(response.ok()).toBe(true);
  const accepted: RpcOutput<"CreateProject"> = await response.json();
  expect(accepted.projectId).toMatch(/^[a-z0-9]+$/);
  expect(Object.keys(accepted)).toEqual(["projectId"]);
  const request: RpcInput<"CreateProject"> = response.request().postDataJSON();
  expect(request.managedRepository).toEqual({});
  expect(request.teamSpaceId).toBe(teamSpaceId);
  expect(request.initialMessage?.trim(), "CreateProject carries the first message").toBeTruthy();
  expect(request.messageId, "CreateProject carries the first message's ID").toBeTruthy();
  expect(request.model, "CreateProject carries the chosen model").not.toBe("AGENT_MODEL_UNSPECIFIED");
  // Real concurrent admission recovers the original identity while Setup is
  // still running; a later retry also returns this Project after it is READY.
  const duplicates = await Promise.all(Array.from({ length: 4 }, () => rpc(page.request, "CreateProject", request)));
  for (const duplicate of duplicates) {
    expect(duplicate.ok()).toBe(true);
    expect(await duplicate.json()).toEqual(accepted);
  }
  await onAccepted?.(accepted.projectId);
  await expect(page).toHaveURL(/\/p\/[a-z0-9]+\/flow(\?|$)/, { timeout: 60_000 });
  const projectId = new URL(page.url()).pathname.split("/")[2];
  expect(projectId).toBe(accepted.projectId);
  await expect
    .poll(async () => (await readProject(page, projectId)).status, { timeout: 10 * 60_000, intervals: [2000] })
    .toBe("PROJECT_STATUS_READY");
  const project = await readProject(page, projectId);
  expect(project.name).toBe(name);
  await expect
    .poll(async () => (await rpc(page.request, "GetSession", { projectId })).status(), {
      timeout: 60_000,
      intervals: [1000],
    })
    .toBe(200);
  // The Session started on the chosen model with the first message, which the five creates admitted once.
  await expect
    .poll(async () => firstMessageCopies(await readSession(page, projectId), request), {
      timeout: 60_000,
      intervals: [1000],
    })
    .toBeGreaterThan(0);
  const started = await readSession(page, projectId);
  expect(firstMessageCopies(started, request), "the first message is admitted exactly once").toBe(1);
  expect(started.agent?.description?.model).toBe(request.model);
  expect(await rpcResult(page.request, "CreateProject", request)).toEqual(accepted);
  expect((await rpc(page.request, "CreateProject", { ...request, name: `${name} conflict` })).status()).toBe(409);
  const changedMessage = { ...request, initialMessage: `${request.initialMessage ?? ""} changed` };
  expect((await rpc(page.request, "CreateProject", changedMessage)).status()).toBe(409);
  if (request.model !== "AGENT_MODEL_MOCK")
    await activateAcceptanceCredential(
      page,
      projectId,
      () => readSession(page, projectId),
      () =>
        rpcResult(page.request, "SendMessage", {
          projectId,
          messageId: randomBytes(16).toString("hex"),
          text: "Continue with the original request now that this Project’s model key is set.",
        }),
    );
  await testInfo.attach("resource-identities", {
    body: JSON.stringify({ projectId, name, studio: process.env.SUPERVERSE_E2E_BASE_URL }),
    contentType: "application/json",
  });
  return { projectId, name };
}
/** A durable final reply of an Agent turn: an assistant message with content and no tool calls. */
function isFinalReply(message: AgentMessage__Output | null | undefined): boolean {
  return message?.role === "ROLE_ASSISTANT" && message.content.trim() !== "" && message.toolCalls.length === 0;
}
/**
 * Asserts that a completed Session snapshot holds the Agent's authoritative history and description, with the Agent
 * waiting for a message, and returns the sequence of its newest final reply after `afterSequence`. Errors never
 * include model content.
 */
export function expectNewestFinalReplySequence(
  completed: SessionSnapshot__Output,
  projectId: string,
  afterSequence: number,
): number {
  expect(Number.isSafeInteger(afterSequence) && afterSequence >= 0).toBe(true);
  expect(completed.projectId).toBe(projectId);
  const agent = completed.agent;
  if (!agent?.history || !agent.description)
    throw new Error("Completed Session must include authoritative Agent history and description");
  expect(agent.description.status).toBe("STATUS_WAITING_FOR_MESSAGE");
  const lastSequence = Number(agent.description.lastSequence);
  expect(Number.isSafeInteger(lastSequence) && lastSequence > afterSequence).toBe(true);
  // Select this turn's latest durable final reply, not an older assistant
  // article or a tool-call message.
  let sequence = 0;
  for (const entry of agent.history.messages) {
    if (!isFinalReply(entry.message)) continue;
    const candidate = Number(entry.sequence);
    expect(Number.isSafeInteger(candidate) && candidate > 0 && candidate <= lastSequence).toBe(true);
    if (candidate > afterSequence) sequence = Math.max(sequence, candidate);
  }
  expect(
    sequence,
    "A newly completed final assistant message must follow the pre-explanation sequence",
  ).toBeGreaterThan(afterSequence);
  return sequence;
}
/** The Session statuses at which waiting for an Agent turn stops: the turn can no longer complete. */
export const ENDED_SESSION_STATUSES: ReadonlySet<SessionStatus__Output> = new Set([
  "SESSION_STATUS_FAILED",
  "SESSION_STATUS_ARCHIVED",
]);
/** Waits until a turn after `afterSequence` of the Project's Session ends with a final reply. */
export async function waitForCompletedTurn(page: Page, projectId: string, afterSequence = -1, timeoutMs = 15 * 60_000) {
  let selectedModel = "";
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const session = await readSession(page, projectId);
    const safeCode = /^[A-Z0-9_]{1,80}$/.test(session.failure?.code ?? "") ? session.failure!.code : "unavailable";
    if (ENDED_SESSION_STATUSES.has(session.status))
      throw new Error(
        `The Session of Project ${projectId} ended before the required completed turn (${session.status}; ${safeCode})`,
      );
    const snapshot = session.agent;
    const description = snapshot?.description;
    // A requirements card never waits: the agent records it and goes on building in the same turn.
    if (description?.pendingUserInput || description?.pendingApproval) {
      throw new Error(
        `The Session of Project ${projectId} requires user input or approval; its Agent turn is not complete`,
      );
    }
    if (
      description?.status === "STATUS_WAITING_FOR_MESSAGE" &&
      snapshot?.history?.messages.some((item) => Number(item.sequence) > afterSequence && isFinalReply(item.message))
    ) {
      selectedModel = description.model;
      expect(selectedModel, "Completed Agent turn must identify its selected model").not.toBe(
        "AGENT_MODEL_UNSPECIFIED",
      );
      break;
    }
    if (Date.now() >= deadline)
      throw new Error(
        `The Session of Project ${projectId} did not complete its real Agent turn within ${timeoutMs / 60_000} minutes`,
      );
    await delay(2000);
  }
  await test.info().attach(`agent-model-${projectId}-${afterSequence}`, {
    body: JSON.stringify({ projectId, afterSequence, model: selectedModel }),
    contentType: "application/json",
  });
}
/**
 * Waits until the Agent recorded its first requirements card, version 1 or later, which it shows before it writes
 * code and then goes on building from. A turn that ends without a card fails.
 */
export async function waitForRequirementsCard(page: Page, projectId: string, timeoutMs = 15 * 60_000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const session = await readSession(page, projectId);
    if (ENDED_SESSION_STATUSES.has(session.status))
      throw new Error(`The Session of Project ${projectId} ended before its requirements card (${session.status})`);
    const description = session.agent?.description;
    if (Number(description?.confirmedRequirements?.revision ?? 0) >= 1) return;
    if (description?.pendingUserInput?.questions.length || description?.pendingApproval)
      throw new Error(
        `The Session of Project ${projectId} requires user input or approval instead of a requirements card`,
      );
    if (
      description?.status === "STATUS_WAITING_FOR_MESSAGE" &&
      session.agent?.history?.messages.some((item) => isFinalReply(item.message))
    )
      throw new Error(`The Session of Project ${projectId} ended its turn without a requirements card`);
    if (Date.now() >= deadline)
      throw new Error(
        `The Session of Project ${projectId} showed no requirements card within ${timeoutMs / 60_000} minutes`,
      );
    await delay(2000);
  }
}
/**
 * Sends a change to the Project's Session through the chat's composer, as a person does, and waits
 * until its turn completes. A Session on the mock model takes its inputs from the mock composer
 * instead, which replaces the text box.
 */
export async function sendChange(page: Page, projectId: string, prompt: string) {
  const before = Number((await readSession(page, projectId)).agent?.description?.lastSequence ?? 0);
  await page.goto(`/p/${projectId}/flow`);
  await page.getByRole("textbox", { name: "Message the agent", exact: true }).fill(prompt);
  await page.getByRole("button", { name: "Send", exact: true }).click();
  await waitForCompletedTurn(page, projectId, before);
}
export async function deleteProject(page: Page, projectId: string) {
  await page.goto(`/p/${projectId}`);
  await page.getByRole("button", { name: "Delete project", exact: true }).click();
  await page.getByRole("button", { name: "Confirm delete project", exact: true }).click();
  await expect(page.getByText("deleted", { exact: true })).toBeVisible({ timeout: 5 * 60_000 });
}
