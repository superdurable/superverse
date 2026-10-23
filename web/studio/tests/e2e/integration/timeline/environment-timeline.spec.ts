// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import type { RpcOutput } from "@/lib/shared/rpc-methods";
import { chooseMockFirstInput, choosesMockModel, waitForFirstMockInput } from "@/tests/e2e/support/mock/mock-composer";
import { awaitConfigurationNotice } from "@/tests/e2e/support/preview/preview";
import { createProject, deleteProject, readProject, readSession } from "@/tests/e2e/support/project";
import { rpcAnswered } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// A template Project's first push reaches the Preview, which refuses it for the connection its
// configuration lacks. The History under the Interface's Preview lists that start from the server's
// recorded starts: the Agent's push has no actor, its outcome is refused, the version reads
// "V1 · subject" and the commit stays out of the row.
test("the Preview timeline lists the refused push with its version and no commit hash", async ({ page }, testInfo) => {
  test.setTimeout(20 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  if (!(await choosesMockModel(page))) throw new Error("The timeline case runs on the mock model");
  const { projectId } = await createProject(
    page,
    teamSpaceId,
    chooseMockFirstInput("configuration-gap-commit"),
    testInfo,
  );
  await waitForFirstMockInput(page, projectId, "configuration-gap-commit");
  const headCommit = await readSession(page, projectId).then(async (first) => {
    await awaitConfigurationNotice(page, projectId, first.headCommit, "summary-model");
    return (await readSession(page, projectId)).headCommit;
  });

  await page.goto(`/p/${projectId}/interface`);
  const timeline = page.getByRole("region", { name: "Preview timeline", exact: true });
  // The history is under the Interface's Details. The panel remounts once the Session's read settles, which
  // folds Details again: open it until it stays.
  await expect(async () => {
    if (!(await timeline.isVisible()))
      await page
        .getByRole("region", { name: "Project Preview", exact: true })
        .getByText("Details", { exact: true })
        .click();
    await expect(timeline).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 60_000 });
  const row = timeline.getByRole("listitem").first();
  await expect(row).toBeVisible({ timeout: 60_000 });
  await expect(row).toContainText("The agent started the Preview on");
  await expect(row).toContainText(/V\d+ · /);
  await expect(row).toContainText("refused");
  await expect(row.getByTestId("problem-panel")).toBeVisible();
  await expect(row.getByText(headCommit, { exact: true })).toBeHidden();
  // The folded technical details hold the hash; the sentence and the version do not.
  await expect(row.locator("p").first()).not.toContainText(headCommit.slice(0, 7));
  await deleteProject(page, projectId);
});

// The shipped newsletter has two real Flow definitions, so its bar exercises the Process picker
// without inventing catalog data or connecting its external providers.
test("the Process picker keeps the environment while selecting each real example Flow and all processes", async ({
  page,
}) => {
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  expect(await choosesMockModel(page)).toBe(true);
  await page.goto(`/t/${teamSpaceId}/projects/start`);
  const example = page.getByRole("article", { name: "GitHub releases newsletter", exact: true });
  await expect(example).toBeVisible({ timeout: 60_000 });
  const answered = page.waitForResponse(
    (response) => rpcAnswered("CreateProject")(response) && response.status() !== 503,
  );
  await example.getByRole("button", { name: "Open example", exact: true }).click();
  const response = await answered;
  expect(response.ok()).toBe(true);
  const { projectId }: RpcOutput<"CreateProject"> = await response.json();
  try {
    await expect
      .poll(async () => (await readProject(page, projectId)).status, { timeout: 10 * 60_000 })
      .toBe("PROJECT_STATUS_READY");
    expect((await readProject(page, projectId)).example?.exampleId).toBe("github-releases-newsletter");
    // This example has no published Live. An explicit Preview selection still belongs to the URL
    // and must survive every Process link; the template acceptance covers the actual Live switch.
    await page.goto(`/p/${projectId}/flow?v=preview`);
    const picker = page.getByRole("button", { name: /^Process: / });
    for (const flowType of ["NewsletterCampaignFlow", "SubscriptionRegistryFlow", null]) {
      await picker.click();
      const menu = page.getByRole("dialog", { name: "Processes", exact: true });
      const link = flowType
        ? menu.locator(`a[href*="flowType=${flowType}"]`)
        : menu.getByRole("link", { name: "All processes", exact: true });
      await link.click();
      await expect(page).toHaveURL(
        (url) =>
          url.pathname === `/p/${projectId}/flow` &&
          url.searchParams.get("v") === "preview" &&
          url.searchParams.get("flowType") === flowType,
      );
    }
    await expect(picker).toHaveAccessibleName("Process: All processes");
  } finally {
    await deleteProject(page, projectId);
  }
});
