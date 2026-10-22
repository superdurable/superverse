// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";

test("Local agent help explains setup, recovery and local permissions", async ({ page, context }) => {
  await page.goto("/help/local-agent");
  await expect(page).toHaveTitle(/Local agent help/);
  await expect(page.getByRole("heading", { name: "Connect your local agent", exact: true })).toBeVisible();
  await expect(page.getByText(/Use macOS with Homebrew/)).toBeVisible();
  await expect(page.getByText(/offers to install it or sign in if needed/)).toBeVisible();
  await expect(page.getByText(/Claude Code requires version 2.1.41 or newer/)).toBeVisible();
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.getByRole("button", { name: "Copy brew install superdurable/tap/dexai", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toBe("brew install superdurable/tap/dexai");
  await page.getByText("Stop and reconnect", { exact: true }).click();
  for (const command of ["dexai status", "dexai disconnect"]) {
    await expect(page.getByRole("button", { name: `Copy ${command}`, exact: true })).toBeVisible();
  }
  await expect(page.getByText(/With several connections, use/)).toBeVisible();
  const daemon = page.getByText("The Codex daemon is not running. What should I do?", { exact: true });
  await daemon.focus();
  await daemon.press("Enter");
  await expect(page.getByText(/dexai starts an absent Codex app-server daemon automatically/)).toBeVisible();
  await page.getByText("What can Claude Code access?", { exact: true }).click();
  await expect(page.getByText(/only the platform MCP tools allowed/)).toBeVisible();
  await page.getByText("Where does dexai keep local state?", { exact: true }).click();
  await expect(page.getByText(/Library\/Application Support\/dexai/)).toBeVisible();
  await expect(page.getByText(/Normal exit revokes the connection and removes its directory/)).toBeVisible();
  await page.getByText("What does ‘message may not have reached the agent’ mean?", { exact: true }).click();
  await expect(page.getByText(/send the message again from the composer/)).toBeVisible();
  await expect(page.getByText(/Sending again may duplicate a message/)).toBeVisible();
  await expect(
    page.getByText("Legacy Orca users can still use dexai-connect until it is retired.", { exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("link", { name: "Back to Studio", exact: true })).toHaveAttribute("href", "/");
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.locator("main").evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true);
});
