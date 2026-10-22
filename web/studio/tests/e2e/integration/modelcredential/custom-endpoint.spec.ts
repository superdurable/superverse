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
import { MOCK_MODEL_API_KEY } from "@/lib/api/model-credential";

test.use({ trace: "off", screenshot: "off", video: "off" });
test("Builder discovers a custom Qwen endpoint and runs tools, a child and compaction", async ({ page }) => {
  test.setTimeout(8 * 60_000);
  const endpoint = process.env.SUPERVERSE_E2E_CUSTOM_ENDPOINT;
  if (!endpoint || process.env.SUPERVERSE_E2E_BYOK_FIXTURE !== "true")
    throw new Error("Requires the custom endpoint bootstrap fixture");
  await register(page);
  const teamSpaceId = new URL(page.url()).pathname.split("/")[2];
  const accepted = await rpcResult(page.request, "CreateProject", {
    teamSpaceId,
    requestId: randomUUID(),
    name: "Custom endpoint browser acceptance",
    managedRepository: {},
  });
  const projectId = accepted.projectId;
  try {
    await expect
      .poll(async () => (await readProject(page, projectId)).status, { timeout: 120_000 })
      .toBe("PROJECT_STATUS_READY");
    await page.goto(`/p/${projectId}/flow`);
    await page
      .locator("[data-session-executor]")
      .getByRole("button", { name: /^Agent:/ })
      .click();
    await page.getByRole("button", { name: "Qwen", exact: true }).click();
    const key = page.locator('[data-model-credential] input[type="password"]');
    await expect(page.getByLabel("Model Studio API key for the endpoint region", { exact: true })).toBeVisible();
    await page.getByLabel("Use a custom endpoint", { exact: true }).check();
    await expect(page.getByLabel("Model Studio API key for the endpoint region", { exact: true })).toHaveCount(0);
    await page.getByLabel("Custom endpoint API key", { exact: true }).fill(MOCK_MODEL_API_KEY);
    await page.getByLabel("Endpoint API base URL", { exact: true }).fill(endpoint);
    const model = page.getByLabel("Model ID (optional)", { exact: true });
    const discovered = page.waitForResponse(rpcAnswered("ListModelCredentialModels"));
    await page.getByRole("button", { name: "Discover models", exact: true }).click();
    expect((await discovered).ok()).toBe(true);
    await page.getByLabel("Discovered models", { exact: true }).selectOption("custom-model");
    await expect(model).toHaveValue("custom-model");
    let saved = page.waitForResponse(rpcAnswered("SetModelCredential"));
    await page.getByRole("button", { name: "Save key", exact: true }).click();
    expect((await saved).ok()).toBe(true);
    await expect(key).toHaveCount(0);
    await page.getByRole("button", { name: "Replace key", exact: true }).click();
    await expect(key).toHaveValue("");
    await key.fill(MOCK_MODEL_API_KEY);
    await page.getByLabel("Endpoint API base URL", { exact: true }).fill(endpoint.replace("/many/", "/one/"));
    await model.clear();
    saved = page.waitForResponse(rpcAnswered("SetModelCredential"));
    await page.getByRole("button", { name: "Save key", exact: true }).click();
    expect((await saved).ok()).toBe(true);
    await expect(key).toHaveCount(0);
    const state = await rpcResult(page.request, "GetModelCredentialInfo", {
      projectId,
      provider: "MODEL_PROVIDER_QWEN",
    });
    expect(state.credential?.customModelId).toBe("");
    expect(state.credential?.effectiveModelId).toBe("self-hosted-qwen");
    expect(JSON.stringify(state)).not.toContain(MOCK_MODEL_API_KEY);
    await page.getByRole("button", { name: "Custom Chat endpoint", exact: true }).click();
    await expect(page.getByRole("textbox", { name: "Message the agent", exact: true })).toBeEnabled();
    await rpcResult(page.request, "SendMessage", {
      projectId,
      messageId: randomUUID(),
      text: "Inspect the manifest and delegate one investigation. [[scenario:custom-endpoint#1]]",
    });
    await expect
      .poll(
        async () => {
          const snapshot = await readSession(page, projectId);
          return (
            snapshot.agent?.description?.status === "STATUS_WAITING_FOR_MESSAGE" &&
            snapshot.agent.description.subagents.some((child) => child.status === "STATUS_COMPLETED")
          );
        },
        { timeout: 180_000 },
      )
      .toBe(true);
    const snapshot = await readSession(page, projectId);
    expect(JSON.stringify(snapshot)).toContain("The custom endpoint child completed");
    expect(JSON.stringify(snapshot)).toContain("Context compacted");
    expect(JSON.stringify(snapshot)).not.toContain("Model call failed");
    expect(JSON.stringify(snapshot)).not.toContain("synthetic forbidden reasoning");
    await expect(page.locator("body")).not.toContainText("synthetic forbidden reasoning");
  } finally {
    await deleteProject(page, projectId);
  }
});
