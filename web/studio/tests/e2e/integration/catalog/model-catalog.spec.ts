// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { MOCK_MODEL_API_KEY } from "@/lib/api/model-credential";
import { randomUUID } from "node:crypto";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";
import { readProject, readSession } from "@/tests/e2e/support/project";
import { rpc, rpcAnswered, rpcResult } from "@/tests/e2e/support/studio-api";
import { runMockScenario } from "@/tests/e2e/support/mock/mock-composer";

// Every command crosses the real Studio BFF, gRPC and Dex. Only the model endpoint is mock.
test.use({ trace: "off", screenshot: "off", video: "off" });
test("configured provider groups admit Project keys and preserve stored model mappings", async ({ page }) => {
  test.setTimeout(20 * 60_000);
  if (process.env.SUPERVERSE_E2E_BYOK_FIXTURE !== "true")
    throw new Error("This spec requires TestProjectAnthropicCredentialDexIntegration's real Native server fixture");
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  const created = await rpcResult(page.request, "CreateProject", {
    teamSpaceId,
    requestId: randomUUID(),
    name: "Catalog compatibility",
    managedRepository: {},
  });
  const projectId = created.projectId;
  let failure: unknown;
  try {
    await expect
      .poll(
        async () => {
          try {
            return (await readSession(page, projectId)).agent?.description?.status;
          } catch (reason) {
            if (reason && typeof reason === "object" && "status" in reason && reason.status === 404) return null;
            throw reason;
          }
        },
        {
          timeout: 10 * 60_000,
          intervals: [1000],
        },
      )
      .toBe("STATUS_WAITING_FOR_MESSAGE");
    await rpcResult(page.request, "SetModelCredential", {
      projectId,
      provider: "MODEL_PROVIDER_ANTHROPIC",
      requestId: randomUUID(),
      expectedRevision: "0",
      apiKey: "synthetic-byok-studio-1234",
    });
    const initial = (await readSession(page, projectId)).agent!.description!;
    const revision = initial.modelRevision;
    // A different provider cannot become selected using the Anthropic Project key.
    const refused = await rpc(page.request, "UpdateSessionModel", {
      projectId,
      model: "AGENT_MODEL_GPT_6_ASTRA",
      expectedRevision: revision,
    });
    expect(refused.status()).toBe(409);
    expect((await readSession(page, projectId)).agent!.description!.modelRevision).toBe(revision);
    await page.goto(`/p/${projectId}/flow`);
    await page.getByRole("button", { name: /^Agent:/ }).click();
    const picker = page.getByRole("dialog", { name: "Choose agent" });
    await expect(picker.getByRole("button", { name: /GPT-6 Astra/ })).toHaveCount(0);
    await expect(picker.getByRole("button", { name: "OpenAI", exact: true })).toBeVisible();
    await expect(picker.getByRole("button", { name: "Qwen", exact: true })).toHaveCount(0);
    for (const option of [
      { provider: "OpenAI", label: "GPT-6 Astra", key: "OpenAI API key", model: "AGENT_MODEL_GPT_6_ASTRA" },
      { provider: "xAI Grok", label: "Grok 4.7", key: "xAI API key", model: "AGENT_MODEL_GROK_4_7" },
      {
        provider: "Google Gemini",
        label: "Gemini 4 Argon",
        key: "Gemini API key",
        model: "AGENT_MODEL_GEMINI_4_ARGON",
      },
    ] as const) {
      await picker.getByRole("button", { name: option.provider, exact: true }).click();
      const key = picker.getByLabel(option.key, { exact: true });
      await expect(key).toHaveAttribute("type", "password");
      await expect(key).toHaveValue("");
      await key.fill(MOCK_MODEL_API_KEY);
      const saved = page.waitForResponse(rpcAnswered("SetModelCredential"));
      await picker.getByRole("button", { name: "Save key", exact: true }).click();
      expect((await saved).ok()).toBe(true);
      await expect(key).toHaveCount(0);
      const selectedModel = page.waitForResponse(rpcAnswered("UpdateSessionModel"));
      await picker.getByRole("button", { name: option.label, exact: true }).click();
      expect((await selectedModel).status(), option.label).toBe(200);
      await runMockScenario(page, projectId, "inspect-basic", { inputs: [1] });
      const completed = (await readSession(page, projectId)).agent!.description!;
      expect(completed.model).toBe(option.model);
      expect(completed.contextModel).toBe(option.model);
      expect(JSON.stringify(await readSession(page, projectId))).not.toContain(MOCK_MODEL_API_KEY);
      await page.getByRole("button", { name: /^Agent:/ }).click();
    }
    await picker.getByRole("button", { name: "Anthropic", exact: true }).click();
    const selected = page.waitForResponse(rpcAnswered("UpdateSessionModel"));
    await picker.getByRole("button", { name: /Claude Sonnet 5.5/ }).click();
    expect((await selected).ok()).toBe(true);
    await expect
      .poll(async () => (await readSession(page, projectId)).agent!.description!.model)
      .toBe("AGENT_MODEL_SONNET_5_5");
    // Reload forces a new BFF/gRPC read of the durable historical Claude value.
    await page.reload();
    await expect(page.getByRole("button", { name: /Agent:.*Claude Sonnet 5.5/ })).toBeVisible();
    await page.getByRole("button", { name: /^Agent:/ }).click();
    const changed = page.waitForResponse(rpcAnswered("UpdateSessionModel"));
    await page.getByRole("dialog", { name: "Choose agent" }).getByRole("button", { name: /^Mock/ }).click();
    expect((await changed).ok()).toBe(true);
    await expect
      .poll(async () => (await readSession(page, projectId)).agent!.description!.model)
      .toBe("AGENT_MODEL_MOCK");
    await runMockScenario(page, projectId, "explain", { inputs: [1] });
    const final = (await readSession(page, projectId)).agent!.description!;
    expect(final.model).toBe("AGENT_MODEL_MOCK");
    expect(final.contextModel).toBe("AGENT_MODEL_MOCK");
  } catch (reason) {
    failure = reason;
    throw reason;
  } finally {
    try {
      const project = await readProject(page, projectId);
      await rpcResult(page.request, "DeleteProject", {
        projectId,
        requestId: randomUUID(),
        expectedRevision: project.revision,
      });
      // Terminal deletion revokes Project access; the real API returns PermissionDenied (403).
      await expect
        .poll(async () => (await rpc(page.request, "GetProject", { projectId })).status(), {
          timeout: 5 * 60_000,
          intervals: [1000],
        })
        .toBe(403);
    } catch (cleanup) {
      if (failure)
        throw new AggregateError([failure, cleanup], "Catalog verification and owned Project cleanup failed");
      throw cleanup;
    }
  }
});
