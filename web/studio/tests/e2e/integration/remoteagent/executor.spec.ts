// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomUUID } from "node:crypto";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { object } from "@/tests/e2e/support/json-assert";
import { readSession } from "@/tests/e2e/support/project";
import { rpcAnswered } from "@/tests/e2e/support/studio-api";
import { openExecutorMenu, runRemoteCase } from "./remote-fixture";

// The external client supplies user behavior through the actual remote listener.
// Studio, BFF, gRPC, SQL and Dex remain the application under test.
test.use({ trace: "off", screenshot: "off", video: "off" });
test("Studio pairs a remote executor, renders its reply and takes over with the observed epoch", async ({ page }) => {
  test.setTimeout(180_000);
  await runRemoteCase(page, async (scope) => {
    const projectId = scope.fixture.projectId;
    const executor = page.locator("[data-session-executor]");
    await expect(executor).toHaveAttribute("data-session-executor", "platform");
    const client = await scope.connect();
    const current = await client.activate("Studio integration host");
    const reply = `A remote reply from the actual HTTP listener (${randomUUID()}).`;
    await expect(executor).toHaveAttribute("data-session-executor", "remote");
    await expect(executor.getByRole("button", { name: /^Agent:/ })).toContainText("My Codex · online");
    await client.post("report", {
      ...current,
      sequence: 1,
      events: [{ kind: "assistant_message", text: reply }],
    });
    const replyArticle = page.locator("article").filter({ hasText: reply });
    await expect(replyArticle.getByText(reply, { exact: true })).toBeVisible();
    await expect(replyArticle.getByText("Codex · Remote reply", { exact: true })).toBeVisible();
    const authority = (await readSession(page, projectId)).agent?.description?.executor;
    if (authority?.remote?.connectionId !== client.connectionId) throw new Error("Reported remote lost authority");
    const menu = await openExecutorMenu(page);
    const switching = page.waitForResponse(rpcAnswered("SetSessionExecutor"));
    await menu.getByRole("button", { name: "Use the Superverse agent", exact: true }).click();
    const switched = await switching;
    if (!switched.ok()) throw new Error(`Executor takeover returned HTTP ${switched.status()}`);
    const command = object(switched.request().postDataJSON(), "Executor takeover request");
    expect(command.basisEpoch).toBe(authority.epoch);
    expect(typeof command.basisEpoch).toBe("string");
    await expect(executor).toHaveAttribute("data-session-executor", "platform");
    await executor.getByRole("button", { name: /^Agent:/ }).click();
    await page
      .getByRole("dialog", { name: "Choose agent" })
      .getByRole("button", { name: /^Superverse agent/ })
      .click();
    await expect(page.getByRole("button", { name: "Claude Sonnet 5.5" })).toBeVisible();
    await page.keyboard.press("Escape");
    // UsePlatform revokes exactly its replaced credential after committing.
    const refused = await client.post("report", { ...current, sequence: 2, turnCompleted: true }, 401);
    expect(refused.code).toBe("unauthorized");
  });
});
