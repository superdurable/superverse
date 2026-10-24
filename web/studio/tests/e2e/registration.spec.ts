// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { identity, register, signIn } from "@/tests/e2e/support/account";
import { createTeamSpace, START_HEADING } from "@/tests/e2e/support/teamspace/teamspace";

test("registration persists an account that creates its Teamspace; logout revokes the session and login recovers it", async ({
  page,
  context,
}) => {
  const account = await register(page, { ...identity(), password: "" });
  const sharedLogin = (await context.cookies()).some((item) => item.name.endsWith("sv2_login"));
  const me = await page.request.get("/api/auth/session");
  expect(me.ok()).toBeTruthy();
  const first = await me.json();
  expect(first.email).toBe(account.email);

  const duplicate = await page.request.post("/api/auth/register", {
    headers: { origin: new URL(page.url()).origin },
    data: account,
  });
  expect(duplicate.status()).toBe(sharedLogin ? 403 : 409);

  const cookie = (await context.cookies()).find((item) => item.name.endsWith("sv2_session"));
  expect(cookie?.httpOnly).toBe(true);
  expect(cookie?.sameSite).toBe("Lax");
  if (page.url().startsWith("https:")) expect(cookie?.secure).toBe(true);
  if (sharedLogin) {
    const productOrigin = new URL(page.url()).origin;
    const loginCookie = (await context.cookies()).find((item) => item.name.endsWith("sv2_login"));
    expect(loginCookie?.httpOnly).toBe(true);
    expect(loginCookie?.domain).not.toBe(new URL(page.url()).hostname);
    const loginURL = new URL(productOrigin);
    loginURL.hostname = loginCookie!.domain;
    const loginOrigin = loginURL.origin;
    expect((await context.cookies(productOrigin)).some((item) => item.name.endsWith("sv2_login"))).toBe(false);
    expect((await context.cookies(loginOrigin)).some((item) => item.name.endsWith("sv2_session"))).toBe(false);

    expect((await page.request.get(`${loginOrigin}/api/rpc/ListTeamSpaces`)).status()).toBe(404);
    expect((await page.request.get(`${loginOrigin}/dex/api/health`)).status()).toBe(404);
    const framed = await page.request.get(`${loginOrigin}/login`);
    expect(framed.headers()["x-frame-options"]).toBe("DENY");
    expect(framed.headers()["content-security-policy"]).toContain("frame-ancestors 'none'");
    expect((await page.request.get("/api/auth/start?next=//example.invalid")).status()).toBe(400);
    const forgedAuthorize = await page.request.post(`${loginOrigin}/api/auth/authorize`, {
      headers: { origin: productOrigin, "content-type": "application/x-www-form-urlencoded" },
      data: "csrf_token=invalid",
    });
    expect(forgedAuthorize.status()).toBe(403);
    const browserExchange = await page.request.post(`${loginOrigin}/api/auth/exchange`, {
      headers: { origin: loginOrigin, "content-type": "application/json" },
      data: {},
    });
    expect(browserExchange.status()).toBe(403);
    const staleCallback = await page.request.get(
      `/api/auth/callback?code=lh1_${"A".repeat(43)}&state=${"A".repeat(43)}`,
      {
        maxRedirects: 0,
      },
    );
    expect(staleCallback.status()).toBe(303);
    expect(staleCallback.headers().location).toBe("/login/restart");
  }

  // A new account gets its first Teamspace without a question, named from its email's company domain
  // (example.com), and its Home offers the first Project.
  const teamSpaceId = await createTeamSpace(page);
  await expect(page.getByRole("button", { name: /^Example · admin/ })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("heading", { name: START_HEADING })).toBeVisible();
  await page.locator('summary[aria-label="Account"]').click();
  // The sidebar's account row names the account too; the open menu's own line is the one checked.
  await expect(page.getByTestId("account-menu").getByRole("paragraph").first()).toHaveText(account.email);
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  expect((await page.request.get("/api/auth/session")).status()).toBe(401);
  // The revoked cookie stays revoked when replayed.
  expect(
    (await page.request.get("/api/auth/session", { headers: { cookie: `${cookie!.name}=${cookie!.value}` } })).status(),
  ).toBe(401);

  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
  if (sharedLogin) {
    await page.getByRole("button", { name: "Use another account" }).click();
    await expect(page.getByLabel("Email", { exact: true })).toBeVisible();
  }
  await page.getByLabel("Email", { exact: true }).fill(account.email);
  await page.getByLabel("Password", { exact: true }).fill("intentionally-wrong-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("alert")).toBeVisible();

  await signIn(page, account);
  expect((await (await page.request.get("/api/auth/session")).json()).accountId).toBe(first.accountId);
  // Signed in again, Studio opens the account's Teamspace.
  await expect(page).toHaveURL(new RegExp(`/t/${teamSpaceId}$`));
});
