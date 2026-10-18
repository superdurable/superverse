// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Request } from "@playwright/test";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { rpcAnswered } from "@/tests/e2e/support/studio-api";
import type { RpcOutput } from "@/lib/shared/rpc-methods";
import { openExecutorMenu, runRemoteCase } from "./remote-fixture";

// Actual Studio/BFF/gRPC, PostgreSQL, Dex and lane resources. Ephemeral codes
// and bearer credentials stay only in memory; browser recordings are disabled.
test.use({ trace: "off", screenshot: "off", video: "off" });
test("Connect dialog pairs without activating and removes its exact connection", async ({ page, context }) => {
  test.setTimeout(180_000);
  await runRemoteCase(page, async (scope) => {
    let listCalls = 0;
    const countList = (request: Request) => {
      if (new URL(request.url()).pathname === "/api/rpc/ListRemoteAgentConnections") ++listCalls;
    };
    page.on("request", countList);
    scope.defer(async () => {
      page.off("request", countList);
    });
    const menu = await openExecutorMenu(page);
    await menu.getByRole("button", { name: /^Connect (a|another) local agent/ }).click();
    const dialog = page.getByRole("dialog", { name: "Connect my agent" });
    await expect(dialog).toBeVisible();
    const create = dialog.getByRole("button", { name: "Create pairing code", exact: true });
    await expect(create).toBeDisabled();
    await expect(dialog.getByText("No remote agent is currently running this Session.")).toBeVisible();
    await dialog.getByRole("checkbox", { name: "I understand these local-machine permissions." }).check();
    await expect(create).toBeEnabled();
    scope.attemptPairing(page);
    const created = page.waitForResponse(rpcAnswered("CreateRemoteAgentPairing"));
    await create.click();
    const response = await created;
    if (!response.ok()) throw new Error(`Pairing returned HTTP ${response.status()}`);
    const pairing: RpcOutput<"CreateRemoteAgentPairing"> = await response.json();
    const client = scope.track(pairing);
    expect(/^[0123456789ABCDEFGHJKMNPQRSTVWXYZ]{10}$/.test(pairing.code)).toBe(true);
    expect(pairing.currentConnectionId === "").toBe(true);
    await expect(
      dialog.getByText("Waiting for your dexai-connect… Press Refresh connections after pairing."),
    ).toBeVisible();
    await expect(dialog.getByText(/^Expires in 9:/)).toBeVisible();
    await expect(create).toBeDisabled();
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await dialog.getByRole("button", { name: "Copy code", exact: true }).click();
    await expect(dialog.getByText("Pairing code copied.")).toBeVisible();
    const copied = await page.evaluate(() => navigator.clipboard.readText());
    expect(copied === pairing.code).toBe(true);
    const before = listCalls;
    await page.waitForTimeout(1200); // Only the local countdown; no Flow-backed periodic refresh.
    expect(listCalls).toBe(before);
    await client.pair({ ...pairing, code: pairing.code.toLowerCase() }, "0.1.0");
    await dialog.getByRole("button", { name: "Refresh connections", exact: true }).click();
    await expect(dialog.getByText("Paired. Pairing alone does not activate the agent.")).toBeVisible();
    await expect(page.locator("[data-session-executor]")).toHaveAttribute("data-session-executor", "platform");
    await expect(dialog.getByRole("button", { name: "Copy code", exact: true })).toBeDisabled();
    const revoked = page.waitForResponse(rpcAnswered("RevokeRemoteAgentConnection"));
    await dialog
      .getByRole("button", { name: `Remove connection of ${scope.fixture.email.trim().toLowerCase()}`, exact: true })
      .click();
    if (!(await revoked).ok()) throw new Error("Connection removal was refused");
    await expect(dialog.getByText("Connection removed.")).toBeVisible();
    const refused = await client.post("disconnect", {}, 401);
    expect(refused.code).toBe("unauthorized");
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(page.locator("[data-session-executor]").getByRole("button", { name: /^Agent:/ })).toBeFocused();
    const reopened = await openExecutorMenu(page);
    await reopened.getByRole("button", { name: /^Connect (a|another) local agent/ }).click();
    await expect(dialog.getByRole("checkbox")).not.toBeChecked();
    await expect(dialog.getByRole("button", { name: "Copy code", exact: true })).toHaveCount(0);
    await page.keyboard.press("Escape");
  });
});
