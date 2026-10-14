// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { type Page } from "@playwright/test";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { register, signIn } from "@/tests/e2e/support/account";
import {
  agentModelEnum,
  createProject,
  expectPushedHead,
  MOCK_TARGET,
  modelTargetDecision,
  readProject,
  readSession,
  realModelTarget,
  typePrompt,
  waitForCompletedTurn,
} from "@/tests/e2e/support/project";
import { ownerFixture } from "@/tests/e2e/support/private-files";
import { privateOwnerStateDirectory, rpcPosted, rpcResult } from "@/tests/e2e/support/studio-api";
import type { SessionSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/SessionSnapshot";
import type { AgentSequencedMessage__Output } from "@/lib/generated/protobuf/superverse/v2/AgentSequencedMessage";
import { openTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// The exact prompt of the test-environment Session yamc/79nvnqsa, unchanged.
const businessPrompt =
  "Build a simple English-language release newsletter application. A public page lets visitors subscribe with their email address and stores each subscriber once. An administrator starts a newsletter in Studio Runs. It reads published releases from the last seven days in the superdurable GitHub organization repositories dex, dex-skills, and dex-connectors-library, excluding drafts and prereleases. Use OpenAI to produce a concise email draft with links to the source releases. The run then waits for administrator review in Studio Inbox. The administrator can approve it or request revision with written feedback; a revised draft must wait for approval again. After approval, send the approved draft through Gmail separately to every subscriber present when sending begins, without exposing the recipient list. Show the draft, review status, and delivery outcome in the run. An uncertain email delivery must be clearly reported and must not be automatically sent again. Keep the public UI simple. No Slack, scheduled sending, unsubscribe, double opt-in, or separate administrator UI is needed. Implement the application, verify its source, and commit and push it. I will configure credentials and validate real execution afterward.";

// Complete durable history, including messages that compaction archived.
async function completeHistory(
  page: Page,
  projectId: string,
  session: SessionSnapshot__Output,
): Promise<AgentSequencedMessage__Output[]> {
  const messages = [...(session.agent?.history?.messages ?? [])];
  let before = session.agent?.history?.nextBeforeSequence;
  const seen = new Set<string>();
  while (before) {
    if (seen.has(before) || seen.size >= 100) throw new Error("Cannot establish complete bounded Agent history");
    seen.add(before);
    const input = { projectId, beforeSequence: before, pageSize: 200 };
    const { history } = await rpcResult(page.request, "ListArchivedMessages", input);
    if (!history) throw new Error("ListArchivedMessages returned no history");
    messages.push(...history.messages);
    before = history.nextBeforeSequence;
  }
  return messages.sort((a, b) => Number(a.sequence) - Number(b.sequence));
}

// Source authoring acceptance for one unchanged business prompt with the
// accepted coding model. Real GitHub, OpenAI and Gmail execution needs private
// provider configuration and is a separate configured acceptance.
test.use({ trace: "off", screenshot: "off", video: "off" });
test("one release-newsletter prompt produces pushed, valid application source", async ({ browser }, info) => {
  test.setTimeout(75 * 60_000);
  // Skipped, never passed: the mock model has no scenario for this journey, which needs the real model.
  test.skip(
    modelTargetDecision() === MOCK_TARGET,
    "SUPERVERSE_E2E_MODEL_TARGET is mock; this journey needs a real model.",
  );
  const directory = await privateOwnerStateDirectory();
  const record = join(directory, "release-newsletter-journey.json");
  const context = await browser.newContext({ baseURL: process.env.SUPERVERSE_E2E_BASE_URL });
  const page = await context.newPage();
  page.setDefaultTimeout(30_000);
  let additionalAgentMessages = 0;
  page.on("request", (request) => {
    if (rpcPosted("SendMessage")(request)) additionalAgentMessages += 1;
  });
  const loginFile = process.env.SUPERVERSE_E2E_LOGIN_FILE;
  if (loginFile) await signIn(page, ownerFixture(loginFile, "Private test-account file"));
  else await register(page);

  const startedAt = new Date();
  const created = await createProject(page, await openTeamSpace(page), typePrompt(businessPrompt), info);
  const initial = await readProject(page, created.projectId);
  expect(initial.templateRevision).toMatch(/^app-template@[0-9a-f]{40}$/);
  // The Flow opens on the project map, whose card names the template's Flow Type.
  await expect(page.getByTestId("project-map").getByRole("link", { name: /TextSummaryFlow/ })).toBeVisible();

  await waitForCompletedTurn(page, created.projectId, -1, 60 * 60_000);
  const completedAt = new Date();
  const project = await readProject(page, created.projectId);
  const session = await readSession(page, created.projectId);
  const messages = await completeHistory(page, created.projectId, session);
  const users = messages.filter((item) => item.message?.role === "ROLE_USER");
  expect(users[0]?.message?.content === businessPrompt, "The original business prompt must remain unchanged").toBe(
    true,
  );
  // The only other user messages are the confirmations of the requirements card, which waitForCompletedTurn clicks.
  for (const answer of users.slice(1))
    expect(answer.message?.content, "A later user message must confirm a requirements card").toMatch(
      /^Requirements confirmed \(version \d+\)$/,
    );
  // Studio sends the prompt as the Session's first message; nothing else is sent.
  expect(additionalAgentMessages).toBe(1);
  expect(session.agent?.description?.model).toBe(agentModelEnum(realModelTarget()));

  const commits = messages.filter(
    (item) => item.message?.role === "ROLE_TOOL" && item.message.toolName === "commit_and_push",
  );
  if (!commits.length) throw new Error("The actual Agent has not recorded a commit_and_push completion");
  // The tool result's content is never projected; its result code says whether the commit was pushed
  // as completed source or recorded as a blocked partial checkpoint.
  expect(
    commits.at(-1)!.message!.toolExecution?.resultCode,
    "A blocked or failed commit_and_push is not completed source",
  ).toBe("pushed");
  const repository = await expectPushedHead(project, session);

  // Durable per-call metrics; never export message bodies or tool arguments.
  const modelCalls = messages.filter((item) => item.message?.modelCall?.model).length;
  const toolResults = messages.filter((item) => item.message?.role === "ROLE_TOOL" && item.message.toolExecution);
  const failedTools = toolResults.filter((item) => item.message?.toolExecution?.outcome !== "OUTCOME_SUCCEEDED").length;
  const calls = messages.flatMap((item) => item.message?.toolCalls ?? []);
  // A read of an installed reference names it as its target, "skills:..." or "module:...".
  const referenceReads = calls.filter((call) => /^(skills|module):/.test(call.target)).length;
  const questions = calls.filter((call) => call.name === "request_user_input").length;
  const requirementCards = calls.filter((call) => call.name === "confirm_requirements").length;
  const metrics = {
    projectId: created.projectId,
    repository: repository.fullName,
    headCommit: session.headCommit,
    model: session.agent?.description?.model,
    authoringSeconds: (completedAt.getTime() - startedAt.getTime()) / 1000,
    modelCalls,
    toolCalls: toolResults.length,
    failedTools,
    referenceReads,
    questions,
    requirementCards,
    confirmedRequirementsVersion: session.agent?.description?.confirmedRequirements?.revision ?? null,
    additionalAgentMessages,
  };
  await writeFile(record, JSON.stringify(metrics, null, 2), { mode: 0o600 });
  await info.attach("release-newsletter-authoring", { contentType: "application/json", body: JSON.stringify(metrics) });
  expect(questions, "The prompt specifies every business decision; the Agent must not ask").toBe(0);

  // Prepared Preview definitions are the strict FDG output of the pushed source.
  await page.goto(`/p/${created.projectId}/flow`);
  await expect
    .poll(
      async () => {
        const source = (await readSession(page, created.projectId)).preview?.preparedSource;
        if (source?.status === "PREVIEW_SOURCE_STATUS_FAILED")
          throw new Error(`Preview preparation failed: ${source.failure?.code}`);
        return source?.status === "PREVIEW_SOURCE_STATUS_READY" && source.sourceCommit === session.headCommit;
      },
      { timeout: 5 * 60_000, intervals: [1000, 2000] },
    )
    .toBe(true);
  const preview = { projectId: created.projectId };
  const prepared = await rpcResult(page.request, "GetPreviewDefinitions", preview);
  expect(prepared.flowDefinitions.length).toBeGreaterThan(0);
  for (const definition of prepared.flowDefinitions) {
    expect(definition.valid).toBe(true);
    expect(definition.schemaVersion).toBe("2.0");
    expect(definition.flowType, "The unrelated template reference Flow must be removed").not.toBe("TextSummaryFlow");
  }
  const { configuration } = await rpcResult(page.request, "GetProjectConfiguration", {
    target: { ...preview, scope: "PROJECT_SCOPE_PREVIEW" },
  });
  const connectors = configuration?.appManifest?.connectors ?? [];
  const declares = (ids: string[], operation: string) =>
    connectors.some((connector) => ids.includes(connector.connectorId) && connector.operations.includes(operation));
  expect(declares(["github"], "listReleases"), "GitHub release reads use the released connector").toBe(true);
  expect(declares(["openai", "llm"], "generateText"), "Draft generation uses the released connector").toBe(true);
  expect(declares(["gmail"], "sendMessage"), "Delivery uses the released Gmail connector").toBe(true);
});
