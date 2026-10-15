// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { chooseMockFirstInput, choosesMockModel, waitForFirstMockInput } from "@/tests/e2e/support/mock/mock-composer";
import { awaitConfigurationNotice } from "@/tests/e2e/support/preview/preview";
import { createProject, deleteProject, readSession } from "@/tests/e2e/support/project";
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
  const marker = `Timeline ${Date.now().toString(36)}`;
  const { projectId } = await createProject(
    page,
    teamSpaceId,
    chooseMockFirstInput("build-basic", { heading: marker }),
    testInfo,
  );
  await waitForFirstMockInput(page, projectId, "build-basic");
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
