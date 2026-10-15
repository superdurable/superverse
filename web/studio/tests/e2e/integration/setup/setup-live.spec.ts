// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { configure, connectionPicker, fillTemplateSummaryConnection } from "@/tests/e2e/support/configuration";
import {
  applyPreviewValue,
  fillEnvironmentValue,
  fillLiveSummaryAnd,
  publishVersion,
  variableControl,
} from "@/tests/e2e/support/configuration/live-versions";
import {
  chooseMockFirstInput,
  choosesMockModel,
  runMockScenario,
  waitForFirstMockInput,
} from "@/tests/e2e/support/mock/mock-composer";
import { createProject, deleteProject, keepWorkspaceRunning, readSession } from "@/tests/e2e/support/project";
import { removeLive } from "@/tests/e2e/support/publishing";
import { chooseOption } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// Setup on the Live tab, on the real Studio, BFF, server, Dex and local Git host, with the mock model pushing
// two versions: Live runs the first, published with its own summary connection and SUMMARY_GREETING; the
// second declares SUMMARY_TONE instead, and Preview applied a value for it. The Live tab shows Live's next
// publish, checked against the version Live runs, under a banner that counts what waits for a publish: a
// value of Live's own that differs from what Live runs waits, and "Same as Preview" makes an item follow
// Preview again.

/** A layer write of one item route, as Setup sends it. */
const layerWrite = (route: string) => (response: { url(): string; request(): { method(): string } }) =>
  new URL(response.url()).pathname.includes("/configuration/live/next") &&
  new URL(response.url()).pathname.endsWith(route) &&
  response.request().method() === "PUT";

test("Setup on Live edits Live's next publish under its banner, which counts what waits for a publish", async ({
  page,
}, testInfo) => {
  test.setTimeout(90 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  expect(await choosesMockModel(page), "Setup on Live runs its two versions on the mock model").toBe(true);
  const created = await createProject(page, teamSpaceId, chooseMockFirstInput("setup-live-versions"), testInfo);
  const projectId = created.projectId;
  let liveRelease = "";
  try {
    await waitForFirstMockInput(page, projectId, "setup-live-versions");
    await keepWorkspaceRunning(page, projectId);
    const older = (await readSession(page, projectId)).headCommit;
    await configure(page, "Preview", fillTemplateSummaryConnection);
    const published = await publishVersion(page, projectId, older, fillLiveSummaryAnd("SUMMARY_GREETING", "Hello"));
    liveRelease = published.release.releaseId;

    await page.goto(`/p/${projectId}/flow`);
    await runMockScenario(page, projectId, "setup-live-versions", { inputs: [2] });
    const newer = (await readSession(page, projectId)).headCommit;
    expect(newer).not.toBe(older);
    await applyPreviewValue(page, projectId, "SUMMARY_TONE", "friendly");

    // The Live tab: a dark banner names the version Live runs, and nothing waits right after its publish.
    await page.goto(`/p/${projectId}/tools?tool=setup&v=live`);
    const banner = page.getByTestId("live-banner");
    await expect(banner).toBeVisible({ timeout: 10 * 60_000 });
    await expect(banner).toContainText("You're changing Live");
    await expect(banner.getByTestId("live-banner-version")).toHaveText(/^V\d+$/);
    const checklist = page.getByTestId("setup-checklist");
    await expect(checklist).toBeVisible({ timeout: 10 * 60_000 });
    const waiting = banner.getByTestId("live-waiting-count");
    await expect(waiting).toHaveText("Nothing waiting for a publish", { timeout: 60_000 });
    await expect(banner.getByTestId("change-live-configuration")).toHaveText(/^Publish V\d+ again$/);

    // What Live published is its own: the connection it chose and the greeting it typed.
    const connection = await connectionPicker(checklist, "summary-model", "next");
    await expect(connection).not.toHaveText("Same as Preview");
    await expect(connection).toHaveAttribute("data-tone", "ok");
    const greeting = await variableControl(page, "SUMMARY_GREETING", "next");
    await expect(greeting).toHaveValue("Hello");

    // A greeting of Live's own that differs from what Live runs waits for a publish, with the blue dot.
    await fillEnvironmentValue(page, "SUMMARY_GREETING", "Hello again");
    await expect(waiting).toContainText("1 change waiting", { timeout: 60_000 });
    await expect(banner.getByTestId("change-live-configuration")).toHaveText("Publish this change");
    await expect(page.getByRole("img", { name: /^Live V\d+ runs Hello now\./ })).toBeVisible();
    // Back to the same as Preview: the version Preview applied no longer declares it, so it takes what Live
    // runs, and nothing waits.
    const followed = page.waitForResponse(layerWrite("/api/v2/application-environment"));
    await page.getByRole("button", { name: "SUMMARY_GREETING: back to the same as Preview", exact: true }).click();
    expect((await followed).request().postDataJSON()).toMatchObject({ followPreview: ["SUMMARY_GREETING"] });
    await expect(waiting).toHaveText("Nothing waiting for a publish", { timeout: 60_000 });

    // "Same as Preview" makes the connection follow Preview's again.
    const [selection] = await Promise.all([
      page.waitForResponse(layerWrite("/api/v2/connector-selections/llm/summary-model")),
      chooseOption(connection, /^Same as Preview/),
    ]);
    expect(selection.ok()).toBe(true);
    expect(selection.request().postDataJSON()).toMatchObject({ followPreview: true });
    await expect(connection).toHaveText("Same as Preview");
  } finally {
    if (liveRelease) await removeLive(page, projectId, liveRelease);
    await deleteProject(page, projectId);
  }
});
