// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { createHash } from "node:crypto";
import { createInterface } from "node:readline/promises";
import { expect, test, type BrowserContext, type Locator, type Page } from "@playwright/test";
import { administratorCredentials, credentials, type Credentials, type OAuth } from "./newsletter-credentials";
import { register, signIn } from "@/tests/e2e/support/account";
import { openPreviewConfiguration } from "@/tests/e2e/support/configuration";
import { observedTarget } from "@/tests/e2e/support/flow-run";
import { chooseMockFirstInput, runMockScenario, waitForFirstMockInput } from "@/tests/e2e/support/mock/mock-composer";
import { createProject, readSession, waitForRequirementsCard } from "@/tests/e2e/support/project";
import { publishLive } from "@/tests/e2e/support/publishing";
import { rpcAnswered, rpcResult, safeFill } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// Only this manual provider test accepts the private self-signed localhost callback certificate.
test.use({ ignoreHTTPSErrors: true });

async function reuseConnections(page: Page, teamSpaceId: string) {
  const required = ["slack", "github", "openai", "gmail"] as const;
  const nameFor = (provider: (typeof required)[number]) =>
    `Newsletter ${provider[0].toUpperCase()}${provider.slice(1)}`;
  const namesFor = (provider: (typeof required)[number]) => [nameFor(provider), `Newsletter newsletter-${provider}`];
  const found = new Map<string, { credentialStatus: string; connectionId: string }>();
  let pageToken = "";
  do {
    const response = await rpcResult(page.request, "ListTeamSpaceConnections", {
      teamSpaceId,
      pageSize: 100,
      pageToken,
      statuses: ["TEAM_SPACE_CONNECTION_STATUS_ACTIVE"],
    });
    for (const connection of response.connections) {
      if (required.some((provider) => namesFor(provider).includes(connection.name)))
        found.set(connection.name, connection);
    }
    if (!response.nextPageToken) break;
    if (response.nextPageToken === pageToken) throw new Error("Teamspace connection cursor did not advance");
    pageToken = response.nextPageToken;
  } while (true);

  for (const provider of required) {
    const name = namesFor(provider).find((candidate) => found.has(candidate)) ?? nameFor(provider);
    const connection = found.get(name);
    if (connection?.credentialStatus !== "CREDENTIAL_STATUS_READY")
      throw new Error(`${name} is not Connected; reauthorize it on the Teamspace Connections page`);
    const row = page.getByTestId("setup-connection").filter({ hasText: `newsletter-${provider}` });
    const choice = row.getByRole("button", { name: new RegExp(`connection newsletter-${provider}$`) });
    await expect(choice).toBeEnabled();
    await choice.click();
    await page
      .getByRole("listbox", { name: new RegExp(`connection newsletter-${provider}$`) })
      .getByRole("option", { name: new RegExp(`^${name}(?:\\s|$)`) })
      .click();
    await expect(choice).toContainText(name);
  }
}

async function newConnection(page: Page, name: string): Promise<Locator> {
  const row = page.getByTestId("setup-connection").filter({ hasText: name });
  const choice = row.getByRole("button", { name: new RegExp(`connection ${name}$`) });
  await expect(choice).toBeEnabled();
  await choice.click();
  await page
    .getByRole("listbox", { name: new RegExp(`connection ${name}$`) })
    .getByRole("option", {
      name: "New connection…",
    })
    .click();
  const editor = page.getByTestId("team-space-connection-editor");
  await expect(editor).toBeVisible();
  await editor.getByLabel("Connection name", { exact: true }).fill(`Newsletter ${name}`);
  return editor;
}

async function authorize(page: Page, editor: Locator, provider: OAuth) {
  await editor.getByLabel("OAuth client ID").fill(provider.clientId);
  await safeFill(editor.getByLabel("OAuth client secret"), provider.clientSecret);
  const studio = new URL(page.url()).origin;
  const leaving = page.waitForRequest(
    (request) => request.isNavigationRequest() && new URL(request.url()).origin !== studio,
    { timeout: 60_000 },
  );
  await editor.getByRole("button", { name: "Authorize", exact: true }).click();
  // Headed manual run: the operator completes provider login and consent in this browser.
  // A provider that refuses sign-in in an automated browser (Google does) is completed in an
  // ordinary browser instead: open this URL there, and paste the callback URL it reaches
  // (stopped at the self-signed certificate warning, so the code is still unused) into this one.
  console.log(`Authorization URL:\n${(await leaving).url()}`);
  await page.waitForURL((url) => url.origin === studio && /\/p\/[^/]+\/tools\?tool=setup/.test(url.href), {
    timeout: 15 * 60_000,
  });
  await expect(page.getByTestId("team-space-connection-editor")).toHaveCount(0);
}

async function pickSlackValue(row: Locator, title: string, id: string, kind: "radio" | "checkbox") {
  if (kind === "radio") {
    const group = row.getByRole("group", { name: title });
    await group.getByRole("button", { name: title }).click();
    const list = row.getByRole("listbox", { name: title });
    const option = list
      .locator(`[role="option"][title="${id}"]`)
      .or(list.getByRole("option", { name: id, exact: true }));
    await expect(option, `Slack bot must be in ${id}; type /invite @app in Slack, then refresh the picker`).toHaveCount(
      1,
      { timeout: 60_000 },
    );
    await option.click();
    await expect(group.getByRole("button", { name: title })).not.toContainText("Choose");
    return;
  }
  const unit = row.locator(`section[aria-label="${title}"]`);
  await expect(unit).toBeVisible();
  await unit.getByRole("searchbox", { name: "Search available values" }).fill(id);
  const option = unit.locator("label").filter({ hasText: id }).getByRole(kind);
  await expect(option).toBeVisible({ timeout: 60_000 });
  await option.check();
  await expect(option).toBeChecked();
}

async function promptForSlackPost(channelId: string, environment: "Preview" | "Live"): Promise<number> {
  if (!process.stdin.isTTY) throw new Error("The manual Slack trigger requires an interactive terminal");
  const prompt = createInterface({ input: process.stdin, output: process.stdout });
  try {
    const after = Date.now();
    await prompt.question(
      `Post the exact top-level message "publish newsletter" for the ${environment} run as the selected admin in Slack channel ${channelId}, then press Enter: `,
    );
    return after;
  } finally {
    prompt.close();
  }
}

function timestampFromSlackLink(value: string, channelId: string): string {
  const link = value.match(/\/archives\/([A-Z0-9]+)\/p(\d{16,})/);
  if (link) {
    if (link[1] !== channelId) throw new Error("Slack message link points to the wrong channel");
    return `${link[2].slice(0, -6)}.${link[2].slice(-6)}`;
  }
  if (/^\d{10,}\.\d{6}$/.test(value)) return value;
  throw new Error("Enter the Slack message link or a timestamp such as 1720000000.000001");
}

async function fallbackSlackTimestamp(channelId: string, environment: "Preview" | "Live"): Promise<string> {
  const prompt = createInterface({ input: process.stdin, output: process.stdout });
  try {
    return timestampFromSlackLink(
      (
        await prompt.question(
          `${environment} Runs did not find the campaign. Paste the Slack message link or timestamp: `,
        )
      ).trim(),
      channelId,
    );
  } finally {
    prompt.close();
  }
}

async function campaignFromRuns(
  page: Page,
  projectId: string,
  environment: "preview" | "live",
  releaseId: string,
  after: number,
): Promise<string | null> {
  const { prefix, headers } = await observedTarget(page, projectId, environment, releaseId);
  const catalogResponse = await page.request.get(`${prefix}/api/v2/catalog`, { headers });
  expect(catalogResponse.ok()).toBe(true);
  const catalog = (await catalogResponse.json()) as { definitionRevision: string; flows: { flowType: string }[] };
  if (!catalog.flows.some((flow) => flow.flowType === "NewsletterCampaignFlow"))
    throw new Error(`${environment} Runs does not serve NewsletterCampaignFlow`);
  const csrf = catalogResponse.headers()["x-superverse-csrf-token"];
  if (!csrf) throw new Error(`${environment} Runs catalog did not issue a CSRF token`);
  const response = await page.request.post(`${prefix}/api/v2/search`, {
    headers: { ...headers, "X-Dex-CSRF-Token": csrf, "X-Dex-Flow-Definition-Revision": catalog.definitionRevision },
    data: { flowType: "NewsletterCampaignFlow", pageSize: 10, filters: [], workQueueOnly: false },
  });
  expect(response.ok()).toBe(true);
  const search = (await response.json()) as { flows: { flowId: string; startTime: string | null }[] };
  return search.flows.find((flow) => flow.startTime && Date.parse(flow.startTime) >= after)?.flowId ?? null;
}

async function runCampaign(
  page: Page,
  context: BrowserContext,
  projectId: string,
  appURL: string,
  environment: "preview" | "live",
  releaseId: string,
  secret: Credentials,
) {
  const app = await context.newPage();
  try {
    await app.goto(appURL);
    await app.getByLabel("Email address").fill(secret.recipientEmail);
    await app.getByRole("button", { name: "Subscribe", exact: true }).click();
    await expect(app.getByText("You're on the list.", { exact: false })).toBeVisible();
  } finally {
    await app.close();
  }

  let flowID: string;
  if (secret.slack.adminUserToken) {
    const headers = { Authorization: `Bearer ${secret.slack.adminUserToken}` };
    const auth = await page.request.post("https://slack.com/api/auth.test", { headers });
    const actor = (await auth.json()) as { ok: boolean; team_id?: string; user_id?: string };
    expect(
      actor.ok && actor.team_id === secret.slack.teamId && actor.user_id === secret.slack.adminUserId,
    ).toBeTruthy();
    const posted = await page.request.post("https://slack.com/api/chat.postMessage", {
      headers,
      form: { channel: secret.slack.channelId, text: "publish newsletter" },
    });
    const message = (await posted.json()) as { ok: boolean; ts?: string; channel?: string };
    expect(message.ok && message.ts && message.channel === secret.slack.channelId).toBeTruthy();
    flowID = `newsletter-${createHash("sha256").update(`${secret.slack.teamId}:${secret.slack.channelId}:${message.ts}`).digest("hex")}`;
  } else {
    const label = environment === "preview" ? "Preview" : "Live";
    const after = await promptForSlackPost(secret.slack.channelId, label);
    let found: string | null = null;
    try {
      await expect
        .poll(async () => (found = await campaignFromRuns(page, projectId, environment, releaseId, after)), {
          timeout: 2 * 60_000,
          intervals: [2000, 5000],
        })
        .not.toBeNull();
    } catch {
      // A link is requested only if the automatic Runs lookup cannot identify the post.
    }
    const timestamp = found ? "" : await fallbackSlackTimestamp(secret.slack.channelId, label);
    flowID =
      found ??
      `newsletter-${createHash("sha256").update(`${secret.slack.teamId}:${secret.slack.channelId}:${timestamp}`).digest("hex")}`;
  }
  await expect
    .poll(
      async () => {
        const response = await page.request.get(`${appURL}/api/newsletters/${flowID}`);
        if (response.status() === 404) return "pending";
        expect(response.ok()).toBe(true);
        const report = (await response.json()) as {
          status: string;
          recipientCount: number;
          sentCount: number;
          failedCount: number;
          unknownCount: number;
        };
        if (
          report.status === "release_failed" ||
          report.status === "generation_failed" ||
          report.status === "generation_unknown"
        )
          throw new Error(`Newsletter failed: ${report.status}`);
        if (report.status !== "completed") return report.status;
        expect(report.recipientCount).toBe(1);
        expect(report.sentCount).toBe(1);
        expect(report.failedCount).toBe(0);
        expect(report.unknownCount).toBe(0);
        return "completed";
      },
      { timeout: 15 * 60_000, intervals: [2000, 5000] },
    )
    .toBe("completed");
  console.log(`Newsletter ${environment}: ${flowID}, sent 1, failed 0, unknown 0`);
}

test("manual real-provider newsletter from a mock-built application", async ({ page, context }, info) => {
  test.setTimeout(120 * 60_000);
  const teamSpaceIdToReuse = process.env.SUPERVERSE_E2E_NEWSLETTER_TEAMSPACE?.trim();
  const secret = credentials(Boolean(teamSpaceIdToReuse));
  let teamSpaceId: string;
  if (teamSpaceIdToReuse) {
    await signIn(page, administratorCredentials());
    teamSpaceId = teamSpaceIdToReuse;
  } else {
    await register(page);
    teamSpaceId = await createTeamSpace(page);
  }
  const created = await createProject(page, teamSpaceId, chooseMockFirstInput("newsletter-app"), info);
  await waitForFirstMockInput(page, created.projectId, "newsletter-app");
  await waitForRequirementsCard(page, created.projectId, 10 * 60_000);
  await runMockScenario(page, created.projectId, "newsletter-app", { inputs: [2], timeoutMs: 30 * 60_000 });
  const source = await readSession(page, created.projectId);
  expect(source.headCommit).toMatch(/^[0-9a-f]{40}$/);
  await openPreviewConfiguration(page, created.projectId);

  if (teamSpaceIdToReuse) await reuseConnections(page, teamSpaceId);
  else {
    let editor = await newConnection(page, "newsletter-slack");
    await editor.getByRole("button", { name: /^Optional settings/ }).click();
    await safeFill(editor.getByLabel(/^App token/), secret.slack.appToken);
    await authorize(page, editor, secret.slack);

    editor = await newConnection(page, "newsletter-github");
    await authorize(page, editor, secret.github);

    editor = await newConnection(page, "newsletter-openai");
    await editor.getByRole("button", { name: /^Provider/ }).click();
    await page
      .getByRole("listbox", { name: /^Provider/ })
      .getByRole("option", { name: "openai" })
      .click();
    await safeFill(editor.getByLabel(/^API key/), secret.openai.apiKey);
    await editor.getByRole("button", { name: /^Optional settings/ }).click();
    await editor.getByLabel(/^Model/).fill(secret.openai.model);
    await editor.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByTestId("team-space-connection-editor")).toHaveCount(0);

    editor = await newConnection(page, "newsletter-gmail");
    await authorize(page, editor, secret.gmail);
  }

  const trigger = page.getByTestId("setup-trigger").filter({ hasText: "newsletter-slack" });
  await pickSlackValue(trigger, "Newsletter channel", secret.slack.channelId, "radio");
  await pickSlackValue(trigger, "Administrators allowed to publish", secret.slack.adminUserId, "checkbox");
  await expect(page.getByTestId("setup-checklist")).toContainText("Ready", { timeout: 60_000 });

  await page.goto(`/p/${created.projectId}/interface`);
  const start = page.getByRole("button", { name: "Start Preview", exact: true });
  await expect(start).toBeEnabled({ timeout: 3 * 60_000 });
  await start.click();
  let previewURL = "";
  await expect
    .poll(
      async () => {
        const preview = (await readSession(page, created.projectId)).preview;
        if (preview?.failure) throw new Error(`Preview failed: ${preview.failure.code}`);
        if (preview?.status !== "PREVIEW_STATUS_READY") return false;
        previewURL = preview.url;
        return true;
      },
      { timeout: 10 * 60_000, intervals: [2000] },
    )
    .toBe(true);

  await runCampaign(page, context, created.projectId, previewURL, "preview", "", secret);

  await page.goto(`/p/${created.projectId}/interface`);
  // The chat has its own Stop button, so look only in the Preview panel.
  const stop = page.getByRole("region", { name: "Project Preview" }).getByRole("button", { name: "Stop", exact: true });
  await expect(stop).toBeEnabled();
  const [stopped] = await Promise.all([page.waitForResponse(rpcAnswered("StopPreview")), stop.click()]);
  expect(stopped.ok()).toBe(true);
  await expect
    .poll(async () => (await readSession(page, created.projectId)).preview?.status, {
      timeout: 5 * 60_000,
      intervals: [1000, 2000],
    })
    .toBe("PREVIEW_STATUS_STOPPED");

  const live = await publishLive(page, created.projectId);
  await runCampaign(page, context, created.projectId, live.url, "live", live.release.releaseId, secret);
});
