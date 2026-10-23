// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomUUID } from "node:crypto";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { rpcResult, rpcAnswered } from "@/tests/e2e/support/studio-api";
import { readProject, readSession, deleteProject } from "@/tests/e2e/support/project";

// The bootstrap fixture composes all production services on Native dependencies.
// It directs only the model adapter at the existing mock model credential endpoint.
test.use({ trace: "off", screenshot: "off", video: "off" });
test("Project Anthropic key entry, replacement and deletion gate the platform composer", async ({ page }) => {
  test.setTimeout(8 * 60_000);
  if (process.env.SUPERVERSE_E2E_BYOK_FIXTURE !== "true")
    throw new Error("This spec requires TestProjectAnthropicCredentialDexIntegration's real Native server fixture");
  let credentialReads = 0;
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/api/rpc/GetModelCredentialInfo") credentialReads++;
  });
  await register(page);
  const teamSpaceId = new URL(page.url()).pathname.split("/")[2];
  const accepted = await rpcResult(page.request, "CreateProject", {
    teamSpaceId,
    requestId: randomUUID(),
    name: "Project key browser acceptance",
    managedRepository: {},
  });
  const projectId = accepted.projectId;
  try {
    await expect
      .poll(async () => (await readProject(page, projectId)).status, { timeout: 120_000 })
      .toBe("PROJECT_STATUS_READY");
    await expect
      .poll(
        async () => {
          try {
            return (await readSession(page, projectId)).status;
          } catch (reason) {
            if (reason && typeof reason === "object" && "status" in reason && reason.status === 404) return null;
            throw reason;
          }
        },
        { timeout: 30_000 },
      )
      .toBe("SESSION_STATUS_ACTIVE");
    await page.goto(`/p/${projectId}/flow`);
    const composer = page.getByRole("textbox", { name: "Message the agent", exact: true });
    await expect(composer).toBeEditable();
    await expect(page.getByRole("button", { name: "Send", exact: true })).toBeDisabled();
    await expect(
      page.getByText("Set this provider's API key in the Agent picker to send. Your draft stays editable.", {
        exact: true,
      }),
    ).toBeVisible();
    const picker = page.locator("[data-session-executor]").getByRole("button", { name: /^Agent:/ });
    await picker.click();
    const key = page.getByLabel("Anthropic API key", { exact: true });
    await expect(key).toHaveAttribute("type", "password");
    await key.fill("synthetic-byok-studio-1234");
    const saved = page.waitForResponse(rpcAnswered("SetModelCredential"));
    await page.getByRole("button", { name: "Save key", exact: true }).click();
    expect((await saved).ok()).toBe(true);
    await expect(page.getByText("Anthropic API key · ••••1234", { exact: true })).toBeVisible();
    await expect(key).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(composer).toBeEnabled();
    const beforeTurnReads = credentialReads;
    await rpcResult(page.request, "SendMessage", {
      projectId,
      messageId: randomUUID(),
      text: "Hello from a keyed browser Session.",
    });
    await expect
      .poll(
        async () => {
          const snapshot = await readSession(page, projectId);
          return (
            Number(snapshot.agent?.description?.turn) > 0 &&
            snapshot.agent?.description?.status === "STATUS_WAITING_FOR_MESSAGE"
          );
        },
        { timeout: 120_000 },
      )
      .toBe(true);
    // One final save invalidation may arrive after the composer enables.
    expect(credentialReads - beforeTurnReads).toBeLessThanOrEqual(1);
    await picker.click();
    await page.getByRole("button", { name: "Replace key", exact: true }).click();
    await expect(key).toHaveValue("");
    await key.fill("synthetic-byok-studio-1234");
    const replaced = page.waitForResponse(rpcAnswered("SetModelCredential"));
    await page.getByRole("button", { name: "Save key", exact: true }).click();
    expect((await replaced).ok()).toBe(true);
    const state = await rpcResult(page.request, "GetModelCredentialInfo", {
      projectId,
      provider: "MODEL_PROVIDER_ANTHROPIC",
    });
    const info = state.credential!;
    expect(info.revision).toBe("2");
    expect(info.last4).toBe("1234");
    expect(JSON.stringify(state.credential)).not.toContain("synthetic-byok-studio-1234");
    const deleted = page.waitForResponse(rpcAnswered("DeleteModelCredential"));
    await page.getByRole("button", { name: "Delete key", exact: true }).click();
    expect((await deleted).ok()).toBe(true);
    await expect(page.getByText("Anthropic API key · Not set", { exact: true })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(composer).toBeEditable();
    await expect(page.getByRole("button", { name: "Send", exact: true })).toBeDisabled();
    await expect(
      page.getByText("Set this provider's API key in the Agent picker to send. Your draft stays editable.", {
        exact: true,
      }),
    ).toBeVisible();
  } finally {
    await deleteProject(page, projectId);
  }
});

for (const { invalid, loseResponse } of [
  { invalid: false, loseResponse: false },
  { invalid: true, loseResponse: false },
  { invalid: false, loseResponse: true },
]) {
  test(`Start form admits the Project key before the first message${invalid ? " and keeps a failed key draft after reload" : loseResponse ? " and fences a lost response after reload" : ""}`, async ({
    page,
  }) => {
    test.setTimeout(8 * 60_000);
    if (process.env.SUPERVERSE_E2E_BYOK_FIXTURE !== "true")
      throw new Error("Requires the real Native credential fixture");
    await register(page);
    const teamSpaceId = new URL(page.url()).pathname.split("/")[2];
    await page.goto(`/t/${teamSpaceId}/projects/start`);
    await page.getByRole("button", { name: "Model", exact: true }).click();
    const panel = page.getByRole("dialog", { name: "Choose model" });
    await panel
      .getByLabel("Anthropic API key", { exact: true })
      .fill(invalid ? "synthetic-invalid-start-key" : "synthetic-byok-studio-1234");
    await panel.getByRole("button", { name: "Use this key for the new Project", exact: true }).click();
    await panel.getByRole("option", { name: "Claude Haiku 4.5", exact: true }).click();
    const prompt = "Inspect this process and report readiness.";
    await page.getByRole("textbox", { name: "Describe a change", exact: true }).fill(prompt);
    const order: string[] = [];
    page.on("request", (request) => {
      const method = new URL(request.url()).pathname.split("/").pop();
      if (method === "SetModelCredential" || method === "SendMessage") order.push(method);
    });
    if (loseResponse)
      await page.route("**/api/rpc/SendMessage", async (route) => {
        const admitted = await route.fetch();
        expect(admitted.ok()).toBe(true);
        await route.abort("failed");
      });
    const created = page.waitForResponse(rpcAnswered("CreateProject"));
    await page.getByRole("button", { name: "Build", exact: true }).click();
    const response = await created;
    expect(response.ok()).toBe(true);
    const creation = response.request().postDataJSON();
    expect(creation.initialMessage).toBeUndefined();
    expect(JSON.stringify(creation)).not.toContain("synthetic-");
    const projectId = (await response.json()).projectId as string;
    try {
      await expect(page).toHaveURL(new RegExp(`/p/${projectId}/flow`));
      const composer = page.getByRole("textbox", { name: "Message the agent", exact: true });
      if (invalid) {
        await expect(page.getByRole("alert").filter({ hasText: /key|credential/i })).toBeVisible({ timeout: 120000 });
        await expect(composer).toHaveValue(prompt);
        await expect(composer).toBeEditable();
        await composer.fill(prompt + " Keep this edit.");
        await page.reload();
        await expect(composer).toHaveValue(prompt + " Keep this edit.");
        await expect(composer).toBeEditable();
        await expect(page.getByRole("button", { name: "Send", exact: true })).toBeDisabled();
        expect(order).toEqual(["SetModelCredential"]);
      } else if (loseResponse) {
        await expect(
          page.getByText(
            "Your first message may already have arrived. Check the conversation before sending it again.",
            { exact: true },
          ),
        ).toBeVisible({ timeout: 120000 });
        await page.unroute("**/api/rpc/SendMessage");
        await page.reload();
        await expect(composer).toHaveValue(prompt);
        await expect(composer).toBeEditable();
        await expect(page.getByRole("button", { name: "Send", exact: true })).toBeDisabled();
        await expect(
          page.getByRole("button", { name: "I checked the conversation; enable manual sending", exact: true }),
        ).toBeVisible();
        expect(order).toEqual(["SetModelCredential", "SendMessage"]);
      } else {
        await expect.poll(() => order, { timeout: 120000 }).toEqual(["SetModelCredential", "SendMessage"]);
        await expect(composer).toHaveValue("");
        const info = await rpcResult(page.request, "GetModelCredentialInfo", {
          projectId,
          provider: "MODEL_PROVIDER_ANTHROPIC",
        });
        expect(info.credential?.status).toBe("MODEL_CREDENTIAL_STATUS_ACTIVE");
      }
      expect(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).not.toContain(
        invalid ? "synthetic-invalid-start-key" : "synthetic-byok-studio-1234",
      );
    } catch (reason) {
      await test.info().attach("first-message-admission", {
        body: JSON.stringify({
          order,
          draft: await page
            .getByRole("textbox", { name: "Message the agent", exact: true })
            .inputValue()
            .catch(() => null),
          sendDisabled: await page
            .getByRole("button", { name: "Send", exact: true })
            .isDisabled()
            .catch(() => null),
          credential: (
            await rpcResult(page.request, "GetModelCredentialInfo", { projectId, provider: "MODEL_PROVIDER_ANTHROPIC" })
          ).credential?.status,
        }),
        contentType: "application/json",
      });
      throw reason;
    } finally {
      await deleteProject(page, projectId);
    }
  });
}
