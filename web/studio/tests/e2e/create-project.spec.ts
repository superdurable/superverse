// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { type Browser, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { revisionFenceHeaders, type ConnectionsResponse } from "@/lib/api/flow/connector";
import { register } from "@/tests/e2e/support/account";
import { startAttachments } from "@/tests/e2e/support/attachments/start-attachments";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";
import { expectNativeConfigurationRecovery } from "@/tests/e2e/support/configuration/recovery";
import { openPreviewConfiguration, setAutomaticBuilds } from "@/tests/e2e/support/configuration";
import { fillEnvironmentValue } from "@/tests/e2e/support/configuration/live-versions";
import {
  chooseMockFirstInput,
  choosesMockModel,
  runMockScenario,
  sendMockInput,
  sendMockMessage,
  waitForFirstMockInput,
} from "@/tests/e2e/support/mock/mock-composer";
import {
  createProject,
  deleteProject,
  expectPushedHead,
  keepWorkspaceRunning,
  readProject,
  readSession,
  sendChange,
  typePrompt,
  waitForCompletedTurn,
} from "@/tests/e2e/support/project";
import { headingChangePrompt, previewTemplateEdit } from "@/tests/e2e/support/preview/preview";
import { publishTemplateEditLive, removeLive } from "@/tests/e2e/support/publishing";
import { observeSessionTransport, withDeadline } from "@/tests/e2e/support/session-transport";
import { rpc, rpcAnswered, rpcResult, studioOrigin } from "@/tests/e2e/support/studio-api";

// The creation page takes the first message of the Project's one Session, which then takes every
// later turn, and CreateProject carries it, so the Session starts with it. SUPERVERSE_E2E_MODEL_TARGET decides the model
// when the platform developer sets it; without it a lane that offers the mock model, as the local lanes do,
// chooses it. On the mock model every turn is a scenario through the mock composer, the creation page's
// included: a read-only inspection, a long streamed explanation, the heading edit, two answered questions
// and a note built by hand. A real model works from the prompts alone. The first message also attaches a
// PNG and an n8n export, which the start page uploads through storage's presigned PUT.
const INSPECTION_PROMPT =
  "Inspect the existing application template. Run its source checks and report readiness. Preserve TextSummaryFlow, its summary form and its API; use Studio Runs for process management.";
const EXPLANATION_PROMPT = [
  "Using only the repository inspection and checks you have already completed, explain the text summary process and its current readiness in 250–350 words of ordinary chat text.",
  "Describe the user-visible form, the durable Flow steps and the result.",
  "State only what you actually verified and distinguish anything unverified.",
  "This is an explanation request: do not change files, invoke tools or ask for a product decision.",
].join(" ");

// Last on the mock model, so no later check waits for an automatic deployment: the person answers the Agent's
// two questions on its card; then, with automatic builds off, builds the Agent's note push with Build processes.
async function answerQuestionsAndBuildByHand(page: Page, projectId: string) {
  await page.goto(`/p/${projectId}/flow`);
  await runMockScenario(page, projectId, "question-answers");
  await setAutomaticBuilds(page, projectId, false);
  // The composer offers a scenario that changes the source only in a new Project.
  await sendMockMessage(page, projectId, "preview-autodeploy", 1);
  const pushed = await readSession(page, projectId);
  expect(pushed.preview?.preparedSource?.sourceCommit, "automatic builds are off").not.toBe(pushed.headCommit);
  // With automatic builds off, Setup's Build processes builds the push.
  await openPreviewConfiguration(page, projectId);
}

// The agent picker's "Connect a local agent…" opens Connect my agent, which lists the Session's remote agent
// connections: a new Project has none.
async function expectNoAgentConnections(page: Page, projectId: string) {
  await page.goto(`/p/${projectId}/flow`);
  await page
    .locator("[data-session-executor]")
    .getByRole("button", { name: /^Agent:/ })
    .click();
  const listed = page.waitForResponse(rpcAnswered("ListRemoteAgentConnections"));
  await page
    .getByRole("dialog", { name: "Choose agent" })
    .getByRole("button", { name: "Connect a local agent…", exact: true })
    .click();
  expect((await listed).ok()).toBe(true);
  const dialog = page.getByRole("dialog", { name: "Connect my agent" });
  await expect(dialog.getByText("No remote agent is currently running this Session.")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
}

// Another freshly registered account, outside the Teamspace, neither lists nor reads the Teamspace, the Project
// or its Session; its Builder page says why instead of waiting for a Watch that was refused.
async function expectHiddenFromOtherAccount(browser: Browser, teamSpaceId: string, projectId: string) {
  const other = await browser.newContext({ baseURL: process.env.SUPERVERSE_E2E_BASE_URL });
  const otherPage = await other.newPage();
  await register(otherPage);
  const reads = [
    await rpc(otherPage.request, "GetTeamSpace", { teamSpaceId }),
    await rpc(otherPage.request, "ListProjects", { teamSpaceId }),
    await rpc(otherPage.request, "GetProject", { projectId }),
    await rpc(otherPage.request, "GetSession", { projectId }),
  ];
  for (const response of reads) expect([403, 404]).toContain(response.status());
  await otherPage.goto(studioRoutes.projectSurface(projectId, "flow"));
  await expect(otherPage.locator('[data-not-ready="error"]')).toContainText(
    /You are not a member of this project|This project does not exist/,
  );
  await other.close();
}

// The real BFF and gRPC boundary reject malformed browser requests before effects;
// the malformed DeleteProject never reaches the Project it names.
async function expectStrictBusinessRoutes(page: Page, teamSpaceId: string, projectId: string) {
  const post = (method: string, data: object) =>
    page.request.post(`/api/rpc/${method}`, { headers: { origin: studioOrigin() }, data });
  const rejectedName = `Rejected source ${randomUUID()}`;
  const missingSource = { requestId: randomUUID(), name: rejectedName, teamSpaceId };
  expect((await post("CreateProject", missingSource)).status()).toBe(400);
  expect((await post("CreateProject", { ...missingSource, importedRepository: { repositoryId: "1" } })).status()).toBe(
    400,
  );
  // Every Project belongs to a Teamspace.
  const noTeamSpace = { requestId: randomUUID(), name: rejectedName, managedRepository: {} };
  expect((await post("CreateProject", noTeamSpace)).status()).toBe(400);
  // A first message takes its message ID, as SendMessage does; neither comes without the other.
  const firstMessage = { requestId: randomUUID(), name: rejectedName, managedRepository: {}, teamSpaceId };
  expect((await post("CreateProject", { ...firstMessage, initialMessage: "Build it." })).status()).toBe(400);
  expect((await post("CreateProject", { ...firstMessage, messageId: randomUUID() })).status()).toBe(400);
  expect((await post("ListImportableRepositories", {})).status()).toBe(404);
  const listed = await rpcResult(page.request, "ListProjects", { teamSpaceId });
  expect(Array.isArray(listed.projects)).toBe(true);
  expect(listed.nextPageToken).toEqual(expect.any(String));
  expect(listed.projects.some((project) => project.name === rejectedName)).toBe(false);
  expect(listed.projects.find((project) => project.projectId === projectId)?.callerRole).toBe("PROJECT_ROLE_OWNER");
  expect((await post("GetProject", { projectId, unknownField: true })).status()).toBe(400);
  const caller = { accountId: "probe", canonicalEmail: "probe@example.com", sessionToken: "p".repeat(32) };
  expect((await post("GetProject", { projectId, caller })).status()).toBe(400);
  for (const method of ["Login", "GetFlowCatalog", "getProject"]) expect((await post(method, {})).status()).toBe(404);
  const unaryGet = await page.request.get(`/api/rpc/GetProject?projectId=${projectId}`);
  expect([unaryGet.status(), unaryGet.headers().allow]).toEqual([405, "POST"]);
  const watchPost = await post("WatchProject", { projectId });
  expect([watchPost.status(), watchPost.headers().allow]).toEqual([405, "GET"]);
  const malformedFence = { projectId, requestId: randomUUID(), expectedRevision: "12abc" };
  expect((await post("DeleteProject", malformedFence)).status()).toBe(400);
  for (const statuses of [["READY"], ["PROJECT_STATUS_UNSPECIFIED"]])
    expect((await post("ListProjects", { teamSpaceId, statuses })).status()).toBe(400);
  expect((await post("ListProjects", {})).status()).toBe(400);
}

test("template project with attached files edits, pushes, previews, deploys Live twice, denies old grants and cleans up", async ({
  page,
  browser,
  context,
}, testInfo) => {
  test.setTimeout(30 * 60_000);
  const transport = await observeSessionTransport(page);
  const failures: unknown[] = [];
  let pendingReconnect: Promise<{ ok: true } | { ok: false; error: unknown }> | undefined;
  try {
    await register(page);
    const teamSpaceId = await createTeamSpace(page);
    const mock = await choosesMockModel(page);
    // The mock composer starts a scenario that changes the source only in a new project, so on
    // the mock model the heading edit is the first input and the inspection runs later.
    const marker = `V2 approved ${Date.now().toString(36)}`;
    const attachments = startAttachments();
    const created = await createProject(
      page,
      teamSpaceId,
      attachments.choose(
        mock ? chooseMockFirstInput("build-basic", { heading: marker }) : typePrompt(INSPECTION_PROMPT),
      ),
      testInfo,
    );
    const projectId = created.projectId;
    await attachments.expectDelivered(page, projectId);
    await expectStrictBusinessRoutes(page, teamSpaceId, projectId);
    if (mock) await waitForFirstMockInput(page, projectId, "build-basic");
    else await waitForCompletedTurn(page, projectId);
    await transport.assertSingleWatch(projectId);
    // The first turn left the workspace READY; keeping it running holds it for the rest of the case.
    await keepWorkspaceRunning(page, projectId);

    // A real coding turn may contain only tool calls. Ask the actual model for
    // prose after the page Watch exists, then interrupt that new streaming reply.
    // Observe rejection immediately: fill/click can fail before this is awaited.
    const beforeExplanation = await readSession(page, projectId);
    const afterSequence = Number(beforeExplanation.agent?.description?.lastSequence);
    expect(Number.isSafeInteger(afterSequence)).toBe(true);
    pendingReconnect = transport.reconnectDuringModelText(projectId, afterSequence).then(
      () => ({ ok: true as const }),
      (error) => ({ ok: false as const, error }),
    );
    if (mock) {
      await sendMockInput(page, projectId, "explain", 1);
    } else {
      await page.getByRole("textbox", { name: "Message the agent", exact: true }).fill(EXPLANATION_PROMPT);
      await page.getByRole("button", { name: "Send", exact: true }).click();
    }
    const interruption = await pendingReconnect;
    pendingReconnect = undefined;
    if (!interruption.ok) throw interruption.error;
    await waitForCompletedTurn(page, projectId, afterSequence);
    const completedExplanation = await readSession(page, projectId);
    await transport.assertCompletedTextReconciled(projectId, afterSequence, completedExplanation);

    // The heading edit continues in the Project's one Session; on the mock model, which made it
    // first, a read-only inspection does.
    if (mock) await runMockScenario(page, projectId, "inspect-basic");
    else await sendChange(page, projectId, headingChangePrompt(marker));
    await transport.assertSingleWatch(projectId);
    const project = await readProject(page, projectId);
    const session = await readSession(page, projectId);
    const repository = await expectPushedHead(project, session);
    expect(repository.fullName).toMatch(/^[^/]+\/[^/]+$/);

    await openPreviewConfiguration(page, projectId);
    if (mock) await fillEnvironmentValue(page, "SUMMARY_GREETING", "From project acceptance");
    // A UI session's signed nonce is longer than a configuration resource ID.
    // Drive both business RPCs through Studio's BFF and require the configuration
    // host's coded result, before this Project selects its Teamspace connection.
    const configurationBase = `/dex/projects/${projectId}/configuration/preview`;
    const listed = await page.request.get(`${configurationBase}/api/v2/connector-connections`);
    expect(listed.ok()).toBe(true);
    const catalog = (await listed.json()) as ConnectionsResponse;
    const connection = catalog.connections.find(
      (item) => item.connectorId === "llm" && item.connectionName === "summary-model",
    );
    expect(connection).toBeDefined();
    const headers = {
      ...revisionFenceHeaders({
        csrfToken: catalog.csrfToken,
        appManifestRevision: catalog.appManifestRevision ?? 0,
        configurationRevision: catalog.configurationRevision ?? "0",
        credentialRevision: connection?.credentialRevision ?? 0,
      }),
      "X-Dex-Flow-Definition-Revision": catalog.definitionRevision,
      origin: studioOrigin(),
    };
    const opened = await page.request.post(`${configurationBase}/api/v2/connector-native-sessions`, {
      headers,
      data: { connectorId: "llm", connectionName: "summary-model" },
    });
    expect(opened.ok()).toBe(true);
    const { sessionNonce } = (await opened.json()) as { sessionNonce: string };
    expect(sessionNonce.length).toBeGreaterThan(256);
    const command = await page.request.post(
      `${configurationBase}/api/v2/connector-ui-sessions/${encodeURIComponent(sessionNonce)}/commands/notDeclared`,
      { headers, data: { parameters: {} } },
    );
    const commandResult = (await command.json()) as { code?: string; error?: string };
    expect(command.status(), `${commandResult.code}: ${commandResult.error}`).toBe(404);
    expect(commandResult.code).toBe("CONNECTOR_STUDIO_COMMAND_NOT_FOUND");
    await expectNativeConfigurationRecovery(page, projectId, testInfo);
    const { app } = await previewTemplateEdit(page, context, projectId, marker, () =>
      transport.assertSingleWatch(projectId),
    );
    // The first publish goes through the Publish popover on the version Preview ran; the second
    // publishes Live's own version again from the Live tab, the same code rebuilt and deployed.
    const live = await publishTemplateEditLive(page, app, projectId, marker, session.headCommit);
    await expectHiddenFromOtherAccount(browser, teamSpaceId, projectId);

    // A second real publish must update the existing Live route and current
    // pointer, then leave the Project able to remove the replacement.
    const replacement = await publishTemplateEditLive(
      page,
      app,
      projectId,
      marker,
      session.headCommit,
      "configuration",
    );
    expect(replacement.deployment.deploymentId).not.toBe(live.deployment.deploymentId);
    expect(BigInt(replacement.deployment.generation)).toBeGreaterThan(BigInt(live.deployment.generation));
    expect(replacement.url).toBe(live.url);
    const oldGate = await page.request.get(
      `/dex/projects/${projectId}/environments/live/releases/${live.release.releaseId}/api/v2/catalog`,
      { headers: { "x-superverse-deployment-id": live.deployment.deploymentId } },
    );
    expect(oldGate.status(), "The superseded deployment must lose current Live authorization").toBe(409);
    await removeLive(page, projectId, replacement.release.releaseId);
    if (mock) await answerQuestionsAndBuildByHand(page, projectId);
    await expectNoAgentConnections(page, projectId);
    await deleteProject(page, projectId);
    await testInfo.attach("accepted-source", {
      body: JSON.stringify({
        projectId,
        remoteSHA: session.headCommit,
        imageDigest: live.imageDigest,
        releaseId: replacement.release.releaseId,
        firstDeploymentId: live.deployment.deploymentId,
        firstGeneration: live.deployment.generation,
        replacementDeploymentId: replacement.deployment.deploymentId,
        replacementGeneration: replacement.deployment.generation,
      }),
      contentType: "application/json",
    });
  } catch (error) {
    failures.push(error);
  } finally {
    try {
      await transport.close();
    } catch (error) {
      failures.push(error);
    }
    if (pendingReconnect) {
      try {
        const outcome = await withDeadline(
          pendingReconnect,
          5000,
          "Reconnect observation did not settle after observer close; Playwright fixture disposal remains responsible",
        );
        if (!outcome.ok) failures.push(outcome.error);
      } catch (error) {
        failures.push(error);
      }
    }
  }
  if (failures.length === 1) throw failures[0];
  if (failures.length > 1) throw new AggregateError(failures, "Project E2E and Session transport cleanup failed");
});
