// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Page } from "@playwright/test";
import { randomBytes } from "node:crypto";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";
import { rpcAnswered, safeFill } from "./studio-api";

export function platformAdminEmail(): string {
  return process.env.SUPERVERSE_E2E_ADMIN_EMAIL || "admin@superverse.test";
}

export function identity() {
  const nonce = randomBytes(6).toString("hex");
  return { email: `sv2-e2e-${nonce}@example.com`, password: `Sv2-${randomBytes(20).toString("base64url")}` };
}
/** The creation of an account's first Teamspace, as Studio sent it and the server answered it. */
export interface FirstTeamSpace {
  status: number;
  request: RpcInput<"CreateTeamSpace">;
  accepted: RpcOutput<"CreateTeamSpace">;
}

const firstTeamSpaces = new WeakMap<Page, FirstTeamSpace>();

/** The first Teamspace Studio created for the account `register` signed up on this page, taken once. */
export function takeFirstTeamSpace(page: Page): FirstTeamSpace | undefined {
  const first = firstTeamSpaces.get(page);
  firstTeamSpaces.delete(page);
  return first;
}

export async function register(page: Page, account = identity()) {
  await page.goto("/register");
  const sharedLogin = new URL(page.url()).hostname.startsWith("login.");
  if (sharedLogin) {
    await page.getByRole("link", { name: "Create an account" }).click();
    await expect(page.getByRole("heading", { name: "Create your account" })).toBeVisible();
    await page.waitForLoadState("networkidle");
  }
  await page.getByLabel("Email", { exact: true }).fill(account.email);
  await page.getByLabel("Password", { exact: true }).fill(account.password);
  // A new account belongs to no Teamspace yet, so Studio creates its first one, named from the email, and opens it.
  const created = page.waitForResponse(rpcAnswered("CreateTeamSpace"), { timeout: 60_000 });
  await page.getByRole("button", { name: "Create account", exact: true }).click();
  if (sharedLogin) {
    await page.getByRole("button", { name: "Continue to Dex AI Platform" }).click();
  }
  const response = await created;
  firstTeamSpaces.set(page, {
    status: response.status(),
    request: response.request().postDataJSON(),
    accepted: response.ok() ? await response.json() : { teamSpaceId: "" },
  });
  await expect(page).toHaveURL(/\/t\/[^/]+$/, { timeout: 60_000 });
  return account;
}
/** Signs in through the login form; a failure never shows the credentials. */
export async function signIn(page: Page, { email, password }: { email: string; password: string }) {
  await page.goto("/login");
  const sharedLogin = new URL(page.url()).hostname.startsWith("login.");
  const continueButton = page.getByRole("button", { name: "Continue to Dex AI Platform" });
  if (!(await continueButton.isVisible())) {
    await safeFill(page.getByLabel("Email", { exact: true }), email);
    await safeFill(page.getByLabel("Password", { exact: true }), password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    if (sharedLogin) await expect(continueButton).toBeVisible();
  }
  if (await continueButton.isVisible()) await continueButton.click();
  // Studio opens at / and moves on to the account's Teamspace when it has one.
  await expect(page).toHaveURL(/\/(t\/[^/]+)?$/);
}
