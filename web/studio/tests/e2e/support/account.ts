// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Page } from "@playwright/test";
import { randomBytes } from "node:crypto";
import { safeFill } from "./studio-api";

export function platformAdminEmail(): string {
  return process.env.SUPERVERSE_E2E_ADMIN_EMAIL || "admin@superverse.test";
}

export function identity() {
  const nonce = randomBytes(6).toString("hex");
  return { email: `sv2-e2e-${nonce}@example.com`, password: `Sv2-${randomBytes(20).toString("base64url")}` };
}
export async function register(page: Page, account = identity()) {
  await page.goto("/register");
  await page.getByLabel("Email", { exact: true }).fill(account.email);
  await page.getByLabel("Password", { exact: true }).fill(account.password);
  await page.getByRole("button", { name: "Create account", exact: true }).click();
  await expect(page).toHaveURL(/\/$/);
  // A new account belongs to no TeamSpace yet, so Studio offers to create one.
  await expect(page.getByRole("heading", { name: "Create your TeamSpace." })).toBeVisible();
  return account;
}
/** Signs in through the login form; a failure never shows the credentials. */
export async function signIn(page: Page, { email, password }: { email: string; password: string }) {
  await page.goto("/login");
  await safeFill(page.getByLabel("Email", { exact: true }), email);
  await safeFill(page.getByLabel("Password", { exact: true }), password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  // Studio opens at / and moves on to the account's TeamSpace when it has one.
  await expect(page).toHaveURL(/\/(t\/[^/]+)?$/);
}
