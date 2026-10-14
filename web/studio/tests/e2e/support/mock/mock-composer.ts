// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Page } from "@playwright/test";
import type { AgentMessage__Output } from "@/lib/generated/protobuf/superverse/v2/AgentMessage";
import { delay } from "@/lib/shared/async";
import {
  ENDED_SESSION_STATUSES,
  MOCK_MODEL_OPTION,
  MOCK_TARGET,
  modelTargetDecision,
  modelTargetRequired,
  readSession,
  type FirstTurnChoice,
} from "@/tests/e2e/support/project";
import { chooseOption, rpcResult } from "@/tests/e2e/support/studio-api";

/*
 * A Session on the mock model takes its scenarios' inputs from the mock composer, which replaces
 * the text box: the specs choose a scenario's category, the scenario and its arguments there, wait
 * until the composer offers the input they expect next and send it, as a person does. The creation page shows the same composer once the person chose the mock
 * model there, and its first input becomes the Session's first message. Whether a lane offers the
 * mock model is what the server tells the caller; whether a Session runs it is what its Snapshot says.
 */

/**
 * Whether the lane lets the signed-in account select the mock model, as the server tells it
 * (`ListMockScenarios`): Local Kind and Native with the mock model configured offer it to every
 * account, a hosted environment only to its administrator, and a server that offers none to nobody.
 */
async function offersMockModel(page: Page): Promise<boolean> {
  return (await rpcResult(page.request, "ListMockScenarios", {})).selectable;
}

/**
 * Whether the spec chooses the mock model on the creation page, decided before it touches the page.
 * SUPERVERSE_E2E_MODEL_TARGET, the platform developer's decision, decides when it is set: `mock`
 * chooses the mock model, which the lane must offer, and a Claude model is chosen with the prompt
 * (`typePrompt`). When it is unset, a lane that offers the mock model chooses it, and a lane that
 * does not fails, because a real model is never automatic.
 */
export async function choosesMockModel(page: Page): Promise<boolean> {
  const target = modelTargetDecision();
  if (target && target !== MOCK_TARGET) return false;
  if (await offersMockModel(page)) return true;
  throw target
    ? new Error(`SUPERVERSE_E2E_MODEL_TARGET is ${MOCK_TARGET}, but this lane does not offer the mock model`)
    : modelTargetRequired();
}

/** Whether the Project's Session runs on the mock model, as its Snapshot's selected model says. */
export async function runsMockModel(page: Page, projectId: string): Promise<boolean> {
  return (await readSession(page, projectId)).agent?.description?.model === "AGENT_MODEL_MOCK";
}

/**
 * Chooses a scenario under its category, as the catalog lists it, sets its arguments and waits
 * until the composer offers input `number` next.
 */
async function selectMockInput(page: Page, scenario: string, number: number, args: Readonly<Record<string, string>>) {
  const { scenarios } = await rpcResult(page.request, "ListMockScenarios", {});
  const category = scenarios.find(({ name }) => name === scenario)?.category;
  if (!category) throw new Error(`The mock catalog lists no scenario ${scenario}`);
  await page.getByLabel("Category", { exact: true }).selectOption(category);
  await page.getByLabel("Scenario", { exact: true }).selectOption(scenario);
  for (const [name, value] of Object.entries(args)) await page.getByLabel(name, { exact: true }).fill(value);
  await expect(page.locator("[data-mock-next-input]")).toHaveAttribute("data-mock-next-input", String(number), {
    timeout: 60_000,
  });
}

/**
 * On the creation page, chooses the mock model and the first input of a scenario with its
 * arguments as the Session's first message, in place of a prompt; Build then creates the Project.
 */
export function chooseMockFirstInput(scenario: string, args: Readonly<Record<string, string>> = {}): FirstTurnChoice {
  return async (page) => {
    await chooseOption(page.getByRole("button", { name: "Model", exact: true }), MOCK_MODEL_OPTION);
    await selectMockInput(page, scenario, 1, args);
  };
}

/** A durable reply that ends a turn: an assistant message with text and no tool calls. */
function isFinalReply(message: AgentMessage__Output | null | undefined): message is AgentMessage__Output {
  return message?.role === "ROLE_ASSISTANT" && message.content.trim() !== "" && message.toolCalls.length === 0;
}

/**
 * Sends input `number` of a scenario through the mock composer: it chooses the scenario, sets the
 * arguments, waits until the composer offers that input next and sends it. It returns the Session's
 * last sequence before the send.
 */
export async function sendMockInput(
  page: Page,
  projectId: string,
  scenario: string,
  number: number,
  args: Readonly<Record<string, string>> = {},
): Promise<number> {
  const before = Number((await readSession(page, projectId)).agent?.description?.lastSequence ?? 0);
  await selectMockInput(page, scenario, number, args);
  // A scenario that received every input offers input 1 again as Start again.
  await page.getByRole("button", { name: /^(Send next input|Start again)$/ }).click();
  return before;
}

/**
 * Waits until the turn an input started ends: the Agent waits again, with a pending input or a
 * reply after `after`. A reply of the mock model itself, such as an input out of order or a
 * stopped scenario, fails with its text, which is never model content.
 */
async function waitForMockTurn(page: Page, projectId: string, after: number, timeoutMs: number) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const session = await readSession(page, projectId);
    if (ENDED_SESSION_STATUSES.has(session.status))
      throw new Error(`The Session of Project ${projectId} ended while a mock input ran (${session.status})`);
    const description = session.agent?.description;
    const replies = (session.agent?.history?.messages ?? [])
      .filter((item) => Number(item.sequence) > after)
      .map((item) => item.message)
      .filter(isFinalReply);
    const last = replies.at(-1);
    if (last?.content.startsWith("Mock scenario "))
      throw new Error(`The mock model refused the input: ${last.content}`);
    if (
      description?.status === "STATUS_WAITING_FOR_MESSAGE" &&
      Number(description.lastSequence) > after &&
      (description.pendingUserInput || last)
    )
      return description;
    if (Date.now() >= deadline)
      throw new Error(
        `The Session of Project ${projectId} did not finish a mock input within ${timeoutMs / 60_000} minutes`,
      );
    await delay(2000);
  }
}

/** Waits until the turn of input `number` ends; the newest mock reply reports the input it answers. */
async function finishMockInput(
  page: Page,
  projectId: string,
  scenario: string,
  number: number,
  after: number,
  timeoutMs: number,
) {
  const { mockProgress } = await waitForMockTurn(page, projectId, after, timeoutMs);
  expect(mockProgress, `${scenario} input ${number} was not received`).toMatchObject({ scenario, input: number });
}

/**
 * Waits until the turn of a scenario's first input ends. The creation page sent that input as the
 * Session's first message once the Session started, so no spec sends it.
 */
export async function waitForFirstMockInput(page: Page, projectId: string, scenario: string, timeoutMs = 15 * 60_000) {
  await finishMockInput(page, projectId, scenario, 1, 0, timeoutMs);
}

/**
 * Runs the inputs of a scenario through the mock composer in order, every input from input 1 or
 * only the numbers `inputs` names, each once the Session waits for it, and returns when the turn
 * of the last one ended. A spec that acts between two inputs runs them in two calls; one that checks
 * the page while an input's turn runs passes `whileRunning`.
 */
export async function runMockScenario(
  page: Page,
  projectId: string,
  scenario: string,
  {
    args = {},
    inputs: only,
    timeoutMs = 15 * 60_000,
    whileRunning,
  }: {
    args?: Readonly<Record<string, string>>;
    inputs?: readonly number[];
    timeoutMs?: number;
    whileRunning?: (number: number) => Promise<void>;
  } = {},
) {
  const { scenarios } = await rpcResult(page.request, "ListMockScenarios", {});
  const catalog = scenarios.find(({ name }) => name === scenario)?.inputs;
  if (!catalog?.length) throw new Error(`The mock catalog lists no scenario ${scenario}`);
  const inputs = only ? catalog.filter(({ number }) => only.includes(number)) : catalog;
  if (only && inputs.length !== only.length)
    throw new Error(`The mock scenario ${scenario} lacks an input of ${only.join(", ")}`);
  for (const { number } of inputs) {
    const after = await sendMockInput(page, projectId, scenario, number, args);
    await whileRunning?.(number);
    await finishMockInput(page, projectId, scenario, number, after, timeoutMs);
  }
}
