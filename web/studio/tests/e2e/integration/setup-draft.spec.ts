// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Page } from "@playwright/test";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { fillTemplateSummaryConnection } from "@/tests/e2e/support/configuration";
import { variableControl } from "@/tests/e2e/support/configuration/live-versions";
import { chooseMockFirstInput, choosesMockModel, waitForFirstMockInput } from "@/tests/e2e/support/mock/mock-composer";
import { awaitConfigurationNotice } from "@/tests/e2e/support/preview/preview";
import { createProject, deleteProject, readSession } from "@/tests/e2e/support/project";
import { chooseOption, rpcAnswered, rpcResult } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// Tools, Setup on the Preview tab, on the lane's real server, Dex and Git host, the mock model making the
// push: Apply waits for READY and lists the missing connection as a link, a selection autosaves, Apply
// restarts the Preview with the revision the tab saved, a later edit survives a reload as one unapplied
// change, and a write on a revision another tab changed shows "Changed elsewhere". The Next publish column
// beside it writes Live's next-publish layer: a setting follows Preview, becomes Live's own, and follows again.

/** Edits the environment value the scenario declares in Preview's column. */
async function typeGreeting(page: Page, value: string) {
  const greeting = await variableControl(page, "SUMMARY_GREETING", "preview");
  await expect(greeting).toBeEnabled({ timeout: 60_000 });
  await greeting.fill(value);
}

/** The scenario's greeting as Live's next publish resolves it, checked against Preview's source. */
async function nextPublishGreeting(page: Page, projectId: string) {
  const target = { projectId, scope: "PROJECT_SCOPE_LIVE" as const, releaseId: "", nextPublish: true as const };
  const { configuration } = await rpcResult(page.request, "GetProjectConfiguration", { target });
  return configuration?.items.find((item) => item.key?.environmentName === "SUMMARY_GREETING");
}

/** A write of Live's next-publish layer to the application environment. */
const layerEnvironmentWrite = (response: { url(): string; request(): { method(): string } }) =>
  new URL(response.url()).pathname.endsWith("/configuration/live/next/api/v2/application-environment") &&
  response.request().method() === "PUT";

/** The Tools tab and Tools' Setup entry, as they read while Setup's Preview column needs a person. */
function setupMarks(page: Page) {
  return {
    tab: page
      .getByRole("navigation", { name: "Surface", exact: true })
      .getByRole("link", { name: "Tools, setup needs you", exact: true }),
    entry: page
      .getByRole("navigation", { name: "Tools", exact: true })
      .getByRole("link", { name: "Setup, needs you", exact: true }),
  };
}

async function openConnectors(page: Page, projectId: string) {
  await page.goto(`/p/${projectId}/tools?tool=connectors`);
  await expect(page.getByTestId("preview-status")).toBeVisible({ timeout: 5 * 60_000 });
  await expect(page.getByTestId("next-publish-status")).toBeVisible();
}

test("Setup autosaves Preview and applies it on its own once READY, refuses a write another tab overtook and follows Preview on the next publish", async ({
  page,
  context,
}, testInfo) => {
  test.setTimeout(30 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  if (!(await choosesMockModel(page))) throw new Error("The Setup draft case runs on the mock model");
  const { projectId } = await createProject(page, teamSpaceId, chooseMockFirstInput("setup-live-versions"), testInfo);
  await waitForFirstMockInput(page, projectId, "setup-live-versions");
  const { headCommit } = await readSession(page, projectId);
  await awaitConfigurationNotice(page, projectId, headCommit, "summary-model");

  // The connection is missing: Preview's state under its column names the gap, a link that focuses its row.
  // Nothing applies by hand; Setup applies on its own.
  await openConnectors(page, projectId);
  const header = page.getByTestId("preview-status");
  await expect(page.getByRole("button", { name: "Apply to Preview", exact: true })).toHaveCount(0);
  const gap = header.getByRole("list", { name: "Missing configuration" }).getByRole("link").first();
  await expect(gap).toContainText("summary-model");
  await gap.click();
  await expect(page).toHaveURL(/#setup-item-/);
  await expect(page.locator(":focus")).toBeVisible();
  // The automatic push was refused: FAILED outranks missing configuration in the environment bar,
  // whose word names it, holds the problem in its hint and leads to the Interface that explains it.
  const previewStatus = page.getByTestId("environment-preview-status");
  await expect(previewStatus).toHaveText("· failed");
  await expect(previewStatus).toHaveAttribute("data-tone", "fail");
  await expect(previewStatus).toHaveAttribute(
    "title",
    /failed: Updating the Preview to the pushed version needs configuration/,
  );
  await expect(previewStatus).toHaveAttribute("href", new RegExp(`/p/${projectId}/interface`));
  // The missing item marks Tools and its Setup entry.
  const marks = setupMarks(page);
  await expect(marks.tab).toBeVisible();
  await expect(marks.entry).toBeVisible();

  // A selection autosaves, the configuration turns READY and Setup applies it on its own, without Apply to
  // Preview: the refused Preview restarts with the revision the last write returned.
  const applied = page.waitForResponse(rpcAnswered("StartPreview"), { timeout: 3 * 60_000 });
  await fillTemplateSummaryConnection(page);
  const request = (await applied).request().postDataJSON() as Record<string, unknown>;
  expect(request.applyConfiguration).toBe(true);
  expect(String(request.configurationRevision)).toMatch(/^[1-9][0-9]*$/);
  // Running is not a word: the tab's dot alone says it, and its hint names the version.
  const previewTab = page.getByTestId("environment-preview");
  await expect(previewTab).toHaveAttribute("data-status", "RUNNING", { timeout: 10 * 60_000 });
  await expect(previewTab).toHaveAttribute("title", /Preview: running · V\d+/);
  await expect(page.getByTestId("environment-preview-status")).toHaveCount(0);
  await expect(page.getByTestId("unapplied-count")).toHaveText(/^Running (V\d+ )?with these settings$/, {
    timeout: 60_000,
  });
  await expect(marks.tab).toHaveCount(0);
  await expect(marks.entry).toHaveCount(0);

  // An edit autosaves, and once the edits settle Setup updates the running Preview with it on its own.
  const updated = page.waitForResponse(rpcAnswered("StartPreview"), { timeout: 2 * 60_000 });
  await typeGreeting(page, "Hello from Preview");
  await expect(page.getByTestId("draft-status")).toHaveText("Saved", { timeout: 30_000 });
  const update = (await updated).request().postDataJSON() as Record<string, unknown>;
  expect(update.applyConfiguration).toBe(true);
  await expect(page.getByTestId("unapplied-count")).toHaveText(/^Running (V\d+ )?with these settings$/, {
    timeout: 10 * 60_000,
  });
  await expect(page.getByTestId("environment-preview-status")).toHaveCount(0);

  // A second tab writes the same value; this tab's next write is refused for the changed revision
  // and, the item having changed, shows "Changed elsewhere" instead of overwriting it.
  const other = await context.newPage();
  await openConnectors(other, projectId);
  await typeGreeting(other, "Hello from the other tab");
  await expect(other.getByTestId("draft-status")).toHaveText("Saved", { timeout: 30_000 });
  await typeGreeting(page, "Hello from this tab");
  const conflict = page.getByTestId("draft-conflict");
  await expect(conflict).toBeVisible({ timeout: 30_000 });
  await expect(page.getByTestId("draft-status")).toHaveText("Not saved: changed elsewhere");
  await conflict.getByRole("button", { name: "Take theirs", exact: true }).click();
  await expect(conflict).toHaveCount(0);
  await other.close();

  // Whichever tab applied last, the Preview ends up running the revision this tab now knows.
  await expect(page.getByTestId("unapplied-count")).toHaveText(/^Running (V\d+ )?with these settings$/, {
    timeout: 10 * 60_000,
  });

  // The Next publish column: before the first publish the greeting follows what Preview applied, so its
  // control reads "Same as Preview" and the state under the column offers the version to publish.
  await expect(page.getByTestId("next-publish-status")).toContainText(/Ready to publish V\d+ with these settings/, {
    timeout: 60_000,
  });
  // The bar's Publish opens the Publish popover on that version; Escape closes it.
  await page.getByTestId("publish-to-live").click();
  const popover = page.getByRole("dialog", { name: "Publish to Live" });
  const head = (await readSession(page, projectId)).headCommit;
  await expect(popover.getByTestId(`publish-version-${head}`)).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("Escape");
  await expect(popover).toHaveCount(0);
  const nextGreeting = await variableControl(page, "SUMMARY_GREETING", "next");
  await expect(nextGreeting).toHaveText("Same as Preview");
  await expect.poll(async () => (await nextPublishGreeting(page, projectId))?.state).toBe("STATE_FOLLOWS_PREVIEW");
  // "Different on Live…" makes it Live's own, written to the next-publish layer, and "Saving…" covers it.
  const owned = page.waitForResponse(layerEnvironmentWrite);
  await chooseOption(nextGreeting, "Different on Live…");
  expect((await owned).ok()).toBe(true);
  const ownGreeting = await variableControl(page, "SUMMARY_GREETING", "next");
  const typed = page.waitForResponse(
    (response) => layerEnvironmentWrite(response) && (response.request().postData() ?? "").includes("Hello from Live"),
  );
  await ownGreeting.fill("Hello from Live");
  expect((await typed).ok()).toBe(true);
  await expect(page.getByTestId("draft-status")).toHaveText("Saved", { timeout: 30_000 });
  await expect.poll(async () => (await nextPublishGreeting(page, projectId))?.state).toBe("STATE_OWN");
  expect((await nextPublishGreeting(page, projectId))?.differsFromPreview).toBe(true);
  // Preview's column keeps its own value: the two columns are two writers.
  await expect(await variableControl(page, "SUMMARY_GREETING", "preview")).not.toHaveValue("Hello from Live");
  // Back to the same as Preview: the layer drops Live's own value with followPreview.
  const followed = page.waitForResponse(layerEnvironmentWrite);
  await page.getByRole("button", { name: "SUMMARY_GREETING: back to the same as Preview", exact: true }).click();
  const follow = await followed;
  expect(follow.ok()).toBe(true);
  expect(follow.request().postDataJSON()).toMatchObject({ followPreview: ["SUMMARY_GREETING"] });
  await expect(await variableControl(page, "SUMMARY_GREETING", "next")).toHaveText("Same as Preview");
  const back = await nextPublishGreeting(page, projectId);
  expect(back?.state).toBe("STATE_FOLLOWS_PREVIEW");
  expect(back?.differsFromPreview).toBe(false);
  await deleteProject(page, projectId);
});
