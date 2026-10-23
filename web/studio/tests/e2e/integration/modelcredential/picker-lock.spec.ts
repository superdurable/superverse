// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomUUID } from "node:crypto";
import type { Page, Locator } from "@playwright/test";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { rpcResult } from "@/tests/e2e/support/studio-api";
import { readProject, deleteProject } from "@/tests/e2e/support/project";

test.use({ trace: "off", screenshot: "off", video: "off" });

async function checkDraft(page: Page, panel: Locator, mockRole: "button" | "option") {
  const key = panel.locator('input[type="password"]');
  const provider = panel.getByRole("button", { name: "OpenAI", exact: true });
  const mock = panel.getByRole(mockRole, { name: "Mock", exact: true });
  await expect(key).toHaveAttribute("autocomplete", "new-password");
  await expect(key).toHaveAttribute("name", "project-model-token");
  await expect(key).toHaveAttribute("data-1p-ignore", "true");
  await expect(key).toHaveAttribute("data-lpignore", "true");
  await expect(key).toHaveAttribute("data-form-type", "other");
  await expect(provider).toBeEnabled();
  await expect(mock).toBeEnabled();
  // A password manager writes the DOM and dispatches input/change without a user edit.
  await key.evaluate((input: HTMLInputElement) => {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
    setter.call(input, "synthetic-login-password");
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await expect(key).toHaveValue("");
  await expect(provider).toBeEnabled();
  await expect(panel.getByText(/Save or discard your key changes/)).toHaveCount(0);
  // A no-op event on an untouched default never locks the picker.
  await panel.getByLabel("Use a custom endpoint", { exact: true }).dispatchEvent("change");
  await expect(provider).toBeEnabled();
  await key.pressSequentially("synthetic-user-key");
  await expect(provider).toBeDisabled();
  await expect(mock).toBeEnabled();
  await expect(panel.getByText(/Save or discard your key changes/)).toBeVisible();
  await expect(panel.getByRole("button", { name: /Save key|Use this key for the new Project/ })).toBeVisible();
  await panel.getByRole("button", { name: "Discard changes", exact: true }).click();
  await expect(provider).toBeEnabled();
  await expect(key).toHaveValue("");
  // Returning to the initial value also releases the lock without Discard.
  await key.fill("synthetic-user-key");
  await expect(provider).toBeDisabled();
  await key.clear();
  await expect(provider).toBeEnabled();
  await panel.getByLabel("Use a custom endpoint", { exact: true }).check();
  await expect(provider).toBeDisabled();
  await panel.getByLabel("Use a custom endpoint", { exact: true }).uncheck();
  await expect(provider).toBeEnabled();
  await key.fill("synthetic-user-key");
  page.once("dialog", (dialog) => dialog.dismiss());
  await mock.click();
  await expect(key).toHaveValue("synthetic-user-key");
  await expect(provider).toBeDisabled();
  page.once("dialog", (dialog) => dialog.accept());
  await mock.click();
}

test("Start picker rejects autofill, compares values and confirms discard before Mock or local", async ({ page }) => {
  await register(page);
  const teamSpaceId = new URL(page.url()).pathname.split("/")[2];
  await page.goto(`/t/${teamSpaceId}/projects/start`);
  const trigger = page.getByRole("button", { name: "Model", exact: true });
  await trigger.click();
  const panel = page.getByRole("dialog", { name: "Choose model" });
  await checkDraft(page, panel, "option");
  await trigger.click();
  await expect(panel.getByRole("button", { name: "OpenAI", exact: true })).toBeEnabled();
  await panel.getByRole("button", { name: "OpenAI", exact: true }).click();
  await expect(panel.locator('input[type="password"]')).toHaveValue("");
  await panel.locator('input[type="password"]').fill("synthetic-user-key");
  await page.keyboard.press("Escape");
  page.once("dialog", (dialog) => dialog.dismiss());
  await page.getByRole("radio", { name: /My local agent/ }).click();
  await expect(page.getByRole("radio", { name: /Platform agent/ })).toBeChecked();
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("radio", { name: /My local agent/ }).click();
  await expect(trigger).toHaveCount(0);
  await page.getByRole("radio", { name: /Platform agent/ }).click();
  await trigger.click();
  await expect(panel.getByRole("option", { name: "Mock", exact: true })).toBeEnabled();
});

test("Composer picker rejects autofill and releases its draft on discard or agent switching", async ({ page }) => {
  test.setTimeout(4 * 60_000);
  await register(page);
  const teamSpaceId = new URL(page.url()).pathname.split("/")[2];
  const created = await rpcResult(page.request, "CreateProject", {
    teamSpaceId,
    requestId: randomUUID(),
    name: "Picker lock regression",
    managedRepository: {},
  });
  const projectId = created.projectId;
  try {
    await expect
      .poll(async () => (await readProject(page, projectId)).status, { timeout: 120_000 })
      .toBe("PROJECT_STATUS_READY");
    await page.goto(`/p/${projectId}/flow`);
    const trigger = page.getByRole("button", { name: /^Agent:/ });
    await trigger.click();
    const panel = page.getByRole("dialog", { name: "Choose agent" });
    await checkDraft(page, panel, "button");
    await trigger.click();
    await expect(panel.getByRole("button", { name: "OpenAI", exact: true })).toBeEnabled();
    await panel.locator('input[type="password"]').fill("synthetic-user-key");
    page.once("dialog", (dialog) => dialog.dismiss());
    await panel.getByRole("button", { name: "Connect a local agent…", exact: true }).click();
    await expect(panel.locator('input[type="password"]')).toHaveValue("synthetic-user-key");
    page.once("dialog", (dialog) => dialog.accept());
    await panel.getByRole("button", { name: "Connect a local agent…", exact: true }).click();
    await expect(page.getByRole("dialog", { name: "Connect your local agent", exact: true })).toBeVisible();
  } finally {
    await deleteProject(page, projectId);
  }
});
