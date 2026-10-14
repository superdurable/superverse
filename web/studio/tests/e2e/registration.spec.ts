// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { identity, register, signIn } from "@/tests/e2e/support/account";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

test("registration persists an account that creates its TeamSpace; logout revokes the session and login recovers it", async ({
  page,
  context,
}) => {
  const account = await register(page, { ...identity(), password: "" });
  const me = await page.request.get("/api/auth/session");
  expect(me.ok()).toBeTruthy();
  const first = await me.json();
  expect(first.email).toBe(account.email);

  const duplicate = await page.request.post("/api/auth/register", {
    headers: { origin: new URL(page.url()).origin },
    data: account,
  });
  expect(duplicate.status()).toBe(409);

  const cookie = (await context.cookies()).find((item) => item.name === "sv2_session");
  expect(cookie?.httpOnly).toBe(true);
  expect(cookie?.sameSite).toBe("Lax");
  if (page.url().startsWith("https:")) expect(cookie?.secure).toBe(true);

  // A new account starts its first TeamSpace, whose Home offers its first Project.
  const teamSpaceId = await createTeamSpace(page);
  await page.reload();
  await expect(page.getByRole("heading", { name: "Create a project." })).toBeVisible();
  await page.locator('summary[aria-label="Account"]').click();
  // The sidebar's account row names the account too; the open menu's own line is the one checked.
  await expect(page.getByTestId("account-menu").getByRole("paragraph").first()).toHaveText(account.email);
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  expect((await page.request.get("/api/auth/session")).status()).toBe(401);
  // The revoked cookie stays revoked when replayed.
  expect(
    (await page.request.get("/api/auth/session", { headers: { cookie: `sv2_session=${cookie!.value}` } })).status(),
  ).toBe(401);

  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
  await page.getByLabel("Email", { exact: true }).fill(account.email);
  await page.getByLabel("Password", { exact: true }).fill("intentionally-wrong-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("alert")).toBeVisible();

  await signIn(page, account);
  expect((await (await page.request.get("/api/auth/session")).json()).accountId).toBe(first.accountId);
  // Signed in again, Studio opens the account's TeamSpace.
  await expect(page).toHaveURL(new RegExp(`/t/${teamSpaceId}$`));
});
