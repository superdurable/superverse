// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, test, type Page } from "@playwright/test";
import { register } from "@/tests/e2e/support/account";
import { object, string } from "@/tests/e2e/support/json-assert";
import { readPrivateJSON } from "@/tests/e2e/support/private-files";
import { safeFill } from "@/tests/e2e/support/studio-api";

/*
 * A Teamspace connection authorized with OAuth through the real Studio, BFF, Go server and the lane's Keycloak,
 * with the published oauth2-http v0.1.0 connector: a new Teamspace's Connections page adds the app from its catalog
 * with its endpoints, client and scopes, Authorize leaves for Keycloak's login, the callback completes the caller's own
 * attempt, which creates the connection, and returns to the page, which shows it connected. A denied consent
 * returns with OAUTH_DENIED and leaves no connection.
 *
 * Inputs: SUPERVERSE_E2E_KEYCLOAK_ORIGIN, the realm's origin as the browser reaches it, which is its issuer;
 * SUPERVERSE_E2E_KEYCLOAK_BACKCHANNEL, the same realm's origin as the server reaches it, the browser's origin on
 * Native and the in-cluster Service on Kind; and SUPERVERSE_E2E_KEYCLOAK_FILE, the lane's private
 * keycloak-credentials.json with the client secret and the test user's password. Nothing they hold is recorded.
 */

const REALM = "/realms/superverse-e2e";
const CLIENT_ID = "superverse-e2e";
const USER = "superverse-e2e";

function keycloak() {
  const browser = new URL(string(process.env.SUPERVERSE_E2E_KEYCLOAK_ORIGIN, "Keycloak origin")).origin;
  const backchannel = new URL(string(process.env.SUPERVERSE_E2E_KEYCLOAK_BACKCHANNEL, "Keycloak backchannel")).origin;
  const credentials = object(
    readPrivateJSON(
      string(process.env.SUPERVERSE_E2E_KEYCLOAK_FILE, "Keycloak credentials file"),
      "Keycloak credentials",
      4096,
    ),
    "Keycloak credentials",
  );
  return {
    authorizationEndpoint: `${browser}${REALM}/protocol/openid-connect/auth`,
    tokenEndpoint: `${backchannel}${REALM}/protocol/openid-connect/token`,
    apiBaseUrl: `${backchannel}${REALM}/protocol/openid-connect`,
    clientSecret: string(credentials.clientSecret, "Keycloak client secret"),
    userPassword: string(credentials.userPassword, "Keycloak user password"),
  };
}

/** Opens the Connections page of the Teamspace Studio created for the account registered a moment ago. */
async function openConnections(page: Page) {
  await expect(page).toHaveURL(/\/t\/[a-z0-9]{8}$/);
  // The read model holds the new Teamspace a moment after its creation; Home counts its member then.
  await expect(page.getByTestId("team-space-counts")).toContainText("1 member", { timeout: 60_000 });
  await page.getByRole("link", { name: "Connections", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Connections", exact: true, level: 2 })).toBeVisible();
}

/** Opens Add an app from Connections and fills a new oauth2-http connection's form up to Authorize. */
async function fillOAuthConnection(page: Page, name: string, provider: ReturnType<typeof keycloak>) {
  await page.getByRole("link", { name: "+ Add connection", exact: true }).click();
  await page.getByLabel("Search apps", { exact: true }).fill("OAuth 2.0 HTTP");
  const app = page.getByTestId("team-space-catalog-app").filter({ hasText: "OAuth 2.0 HTTP" });
  await app.getByRole("button", { name: /^(Connect|Add another)$/ }).click();
  const editor = app.getByTestId("team-space-connection-editor");
  await editor.getByLabel("Connection name", { exact: true }).fill(name);
  await editor.getByLabel(/^Authorization endpoint/).fill(provider.authorizationEndpoint);
  await editor.getByLabel(/^Token endpoint/).fill(provider.tokenEndpoint);
  await editor.getByLabel(/^Scopes/).fill('["openid"]');
  await editor.getByLabel(/^API base URL/).fill(provider.apiBaseUrl);
  await editor.getByLabel(/^OAuth client ID/).fill(CLIENT_ID);
  await safeFill(editor.getByLabel(/^OAuth client secret/), provider.clientSecret);
  return editor;
}

test("an OAuth Teamspace connection authorizes through Keycloak and returns connected", async ({ page }) => {
  test.setTimeout(5 * 60_000);
  const provider = keycloak();
  await register(page);
  await openConnections(page);

  const editor = await fillOAuthConnection(page, "Keycloak", provider);
  await editor.getByRole("button", { name: "Authorize", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`${REALM}/protocol/openid-connect/auth`));
  await page.locator("#username").fill(USER);
  await safeFill(page.locator("#password"), provider.userPassword);
  await page.locator("#kc-login").click();

  await expect(page).toHaveURL(/\/t\/[a-z0-9]{8}\/connections$/);
  await expect(page.getByRole("status").filter({ hasText: "Signed in" })).toBeVisible();
  const row = page.getByTestId("team-space-connection").filter({ hasText: "Keycloak" });
  await expect(row).toContainText("Connected");
  await expect(page.getByTestId("team-space-app").filter({ hasText: "OAuth 2.0 HTTP" })).toContainText("1 connection");

  // A second connection whose consent the person refuses returns refused, and stays unauthorized. Keycloak's
  // session would consent at once, so the provider page is held and its refusal sent as Keycloak would.
  const second = await fillOAuthConnection(page, "Keycloak refused", provider);
  let held: URL | null = null;
  await page.route(`**${REALM}/protocol/openid-connect/auth*`, async (route) => {
    held = new URL(route.request().url());
    await route.fulfill({ status: 200, contentType: "text/html", body: "<title>Held</title>" });
  });
  await second.getByRole("button", { name: "Authorize", exact: true }).click();
  await expect.poll(() => held !== null).toBe(true);
  await page.unroute(`**${REALM}/protocol/openid-connect/auth*`);
  const denied = held as unknown as URL;
  const callback = new URL(string(denied.searchParams.get("redirect_uri"), "redirect URI"));
  callback.searchParams.set("state", string(denied.searchParams.get("state"), "state"));
  callback.searchParams.set("error", "access_denied");
  await page.goto(callback.toString());
  await expect(page).toHaveURL(/\/t\/[a-z0-9]{8}\/connections$/);
  await expect(page.getByRole("alert").filter({ hasText: "denied the authorization" })).toBeVisible();
  // The connection exists only once its authorization completes, so the refused one left none.
  await expect(page.getByTestId("team-space-connection").filter({ hasText: "Keycloak refused" })).toHaveCount(0);
});
