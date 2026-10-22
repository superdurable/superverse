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
    await expect(composer).toBeDisabled();
    await expect(page.getByText("Set your key or connect your local agent", { exact: true })).toBeVisible();
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
    await expect(composer).toBeDisabled();
    await expect(page.getByText("Set your key or connect your local agent", { exact: true })).toBeVisible();
  } finally {
    await deleteProject(page, projectId);
  }
});
