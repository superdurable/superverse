// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Request } from "@playwright/test";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { readProject, readSession } from "@/tests/e2e/support/project";
import { rpc, rpcPosted } from "@/tests/e2e/support/studio-api";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { openExecutorMenu, runRemoteCase } from "./remote-fixture";
import { installSwitchGate } from "./switch-gate";

test.use({ trace: "off", screenshot: "off", video: "off" });

test("Project navigation cancels a pending switch before dispatch and leaves both authorities unchanged", async ({
  page,
}) => {
  test.setTimeout(180_000);
  await runRemoteCase(page, async (scope) => {
    const projectId = scope.fixture.projectId;
    const other = await scope.otherProject();
    const otherBefore = (await readSession(page, other.projectId)).agent?.description?.executor;
    if (!otherBefore) throw new Error("Navigation fixture needs an existing Session executor");
    const client = await scope.connect();
    await client.activate("Navigation cancellation connection");
    await expect(page.locator("[data-session-executor]")).toHaveAttribute("data-session-executor", "remote");
    const before = (await readSession(page, projectId)).agent?.description?.executor;
    if (before?.remote?.connectionId !== client.connectionId)
      throw new Error("Navigation case lost its exact executor");
    let switchCalls = 0;
    const count = (request: Request) => {
      if (rpcPosted("SetSessionExecutor")(request)) ++switchCalls;
    };
    page.on("request", count);
    scope.defer(async () => {
      page.off("request", count);
    });
    const gate = await installSwitchGate(page, projectId, before.epoch);
    scope.defer(() => gate.dispose());
    const menu = await openExecutorMenu(page);
    await menu.getByRole("button", { name: "Use the Superverse agent", exact: true }).click();
    await gate.pending();
    await expect(menu.getByRole("button", { name: "Switching…", exact: true })).toBeDisabled();
    // This is the real Next Link navigation; no clock or backend response is replaced.
    await page.getByRole("button", { name: /, switch project$/ }).click();
    await page.getByRole("dialog", { name: "Switch project" }).locator(`a[href="/p/${other.projectId}/flow"]`).click();
    await expect(page).toHaveURL(new RegExp(`/p/${other.projectId}/flow(?:[?#]|$)`));
    await gate.assertCancelled();
    await gate.dispose();
    const after = (await readSession(page, projectId)).agent?.description?.executor;
    expect(after?.epoch).toBe(before.epoch);
    expect(after?.generation).toBe(before.generation);
    expect(after?.remote?.connectionId).toBe(client.connectionId);
    const otherAfter = (await readSession(page, other.projectId)).agent?.description?.executor;
    expect(otherAfter?.epoch).toBe(otherBefore.epoch);
    expect(otherAfter?.generation).toBe(otherBefore.generation);
    expect(otherAfter?.mode).toBe(otherBefore.mode);
    expect(otherAfter?.remote?.connectionId).toBe(otherBefore.remote?.connectionId);
    const nextMenu = page.locator("[data-session-executor]");
    await expect(nextMenu).toBeVisible();
    await expect(nextMenu.getByRole("button", { name: "Switching…", exact: true })).toHaveCount(0);
    await expect(nextMenu.getByRole("alert")).toHaveCount(0);
    expect(switchCalls).toBe(1); // No stale command was sent against the destination Project.
  });
});

test("Actual Builder downgrade aborts the pending browser switch; the real BFF independently refuses it", async ({
  page,
  browser,
}) => {
  test.setTimeout(240_000);
  await runRemoteCase(page, async (scope) => {
    const projectId = scope.fixture.projectId;
    const builder = await scope.addBuilder(browser);
    const client = await scope.connect();
    await client.activate("Permission cancellation connection");
    await builder.page.goto(studioRoutes.projectSurface(projectId, "flow"));
    await expect(builder.page.locator("[data-session-executor]")).toHaveAttribute("data-session-executor", "remote");
    const before = (await readSession(page, projectId)).agent?.description?.executor;
    if (before?.remote?.connectionId !== client.connectionId)
      throw new Error("Permission case lost its exact executor");
    const gate = await installSwitchGate(builder.page, projectId, before.epoch);
    scope.defer(() => gate.dispose());
    const menu = await openExecutorMenu(builder.page);
    await menu.getByRole("button", { name: "Use the Superverse agent", exact: true }).click();
    await gate.pending();
    await expect(menu.getByRole("button", { name: "Switching…", exact: true })).toBeDisabled();
    const changed = await rpc(page.request, "SetProjectMemberRole", {
      projectId,
      accountId: builder.accountId,
      role: "PROJECT_ROLE_OPERATOR",
    });
    if (!changed.ok()) throw new Error(`Owned Builder downgrade returned HTTP ${changed.status()}`);
    await expect
      .poll(async () => (await readProject(builder.page, projectId)).callerRole, { timeout: 60_000 })
      .toBe("PROJECT_ROLE_OPERATOR");
    await gate.assertCancelled();
    await gate.dispose();
    await expect(builder.page.getByRole("button", { name: "Use the Superverse agent", exact: true })).toHaveCount(0);
    await expect(builder.page.getByRole("button", { name: "Connect my agent", exact: true })).toHaveCount(0);
    await expect(builder.page.getByRole("button", { name: "Switching…", exact: true })).toHaveCount(0);
    await expect(builder.page.locator("[data-session-executor]").getByRole("alert")).toHaveCount(0);
    // APIRequestContext reaches the actual BFF/server, outside the browser-only
    // gate. The aborted request never reached server; this separate denial does.
    const denied = await rpc(builder.page.request, "SetSessionExecutor", { projectId, basisEpoch: before.epoch });
    expect(denied.status()).toBe(403);
    const after = (await readSession(page, projectId)).agent?.description?.executor;
    expect(after?.epoch).toBe(before.epoch);
    expect(after?.generation).toBe(before.generation);
    expect(after?.remote?.connectionId).toBe(client.connectionId);
  });
});
