// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import {
  chooseMockFirstInput,
  choosesMockModel,
  sendMockInput,
  waitForFirstMockInput,
} from "@/tests/e2e/support/mock/mock-composer";
import { awaitConfigurationNotice } from "@/tests/e2e/support/preview/preview";
import { createProject, deleteProject, readSession } from "@/tests/e2e/support/project";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// A template Project's first push needs a Preview connection its configuration lacks, so the
// Preview refuses to start it. The refusal reaches the person in the one problem format: what
// happened, why, the Open Setup fix the server declared with the code, and the folded details,
// which alone name the code and the commit. The mock model makes the push; everything else is the
// lane's real server, Dex and Git host.
test("a refused Preview start shows what happened, why, Open Setup and the folded details", async ({
  page,
}, testInfo) => {
  test.setTimeout(20 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  if (!(await choosesMockModel(page))) throw new Error("The problem format case runs on the mock model");
  const { projectId } = await createProject(
    page,
    teamSpaceId,
    chooseMockFirstInput("configuration-gap-commit"),
    testInfo,
  );
  await waitForFirstMockInput(page, projectId, "configuration-gap-commit");
  const { headCommit, preview } = await readSession(page, projectId).then(async (first) => {
    await awaitConfigurationNotice(page, projectId, first.headCommit, "summary-model");
    return readSession(page, projectId);
  });
  expect(preview?.failure?.code).toBe("PREVIEW_CONFIGURATION_INCOMPATIBLE");
  expect(preview?.failure?.remedy).toBe("FAILURE_REMEDY_OPEN_SETUP");
  expect(preview?.failure?.owner).toBe("FAILURE_OWNER_CONFIGURATION");

  await page.goto(`/p/${projectId}/interface`);
  const panel = page.getByRole("region", { name: "Project Preview", exact: true }).getByTestId("problem-panel");
  await expect(panel).toBeVisible({ timeout: 60_000 });
  // 1. What happened, in words, with no code.
  const what = panel.getByTestId("problem-what");
  await expect(what).toHaveText("Updating the Preview to the pushed version needs configuration");
  // 2. Why: the server's message for the code, and what the configuration lacks.
  await expect(panel.getByTestId("problem-why")).toHaveText(
    "The Preview configuration does not provide what the source declares.",
  );
  await expect(panel.getByRole("list", { name: "Missing configuration" })).toContainText("summary-model");
  // 3. The one fix the declared remedy names.
  const fix = panel.getByTestId("problem-fix").getByRole("link", { name: "Open Setup", exact: true });
  await expect(fix).toHaveAttribute("href", `/p/${projectId}/tools?tool=setup`);
  // 4. The technical details, folded until opened: the code, the phase and the commit live only here.
  const details = panel.getByTestId("problem-details");
  await expect(details.getByText("PREVIEW_CONFIGURATION_INCOMPATIBLE", { exact: true })).toBeHidden();
  await details.getByText("Technical details", { exact: true }).click();
  await expect(details.getByText("PREVIEW_CONFIGURATION_INCOMPATIBLE", { exact: true })).toBeVisible();
  await expect(details.getByText("deploy-preview", { exact: true })).toBeVisible();
  await expect(details.getByText(headCommit, { exact: true })).toBeVisible();
  for (const part of [what, panel.getByTestId("problem-why")]) {
    await expect(part).not.toContainText("PREVIEW_");
    await expect(part).not.toContainText(headCommit.slice(0, 7));
  }
  await expect(panel).not.toContainText("did not complete");

  await fix.click();
  await expect(page).toHaveURL(new RegExp(`/p/${projectId}/tools\\?tool=setup`));
  await deleteProject(page, projectId);
});

const creditExplanation =
  "The model provider account has run out of credit. After it is topped up, send the message again.";

test("a provider credit rejection explains the fix and stops the turn live and after reload", async ({
  page,
}, testInfo) => {
  test.setTimeout(20 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  if (!(await choosesMockModel(page))) throw new Error("The credit rejection case runs on the mock model");
  const { projectId } = await createProject(
    page,
    teamSpaceId,
    chooseMockFirstInput("model-credit-exhausted"),
    testInfo,
  );
  const status = async () => {
    const session = await readSession(page, projectId);
    const marker = session.agent?.history?.messages?.findLast((item) =>
      item.message?.content?.startsWith("Model call failed:"),
    );
    return marker && session.agent?.description?.status === "STATUS_WAITING_FOR_MESSAGE" ? marker.message?.content : "";
  };
  await expect.poll(status, { timeout: 60_000 }).toBe(`Model call failed: ${creditExplanation}`);
  await page.goto(`/p/${projectId}/flow`);
  // This literal also catches drift from Go's modelFailureMarkerPrefix.
  await expect(page.getByTestId("work-log-failure").last()).toHaveText(`Model call failed: ${creditExplanation}`);
  await expect(page.getByTestId("work-log").getByText("Running", { exact: true })).toHaveCount(0);

  // A second rejection reaches the open Watch, then a fresh snapshot on reload.
  const before = Number((await readSession(page, projectId)).agent?.description?.lastSequence ?? 0);
  await sendMockInput(page, projectId, "model-credit-exhausted", 1);
  await expect
    .poll(
      async () => {
        const session = await readSession(page, projectId);
        return (
          session.agent?.description?.status === "STATUS_WAITING_FOR_MESSAGE" &&
          Number(session.agent.description.lastSequence) > before
        );
      },
      { timeout: 60_000 },
    )
    .toBe(true);
  await expect(page.getByTestId("work-log-failure").last()).toHaveText(`Model call failed: ${creditExplanation}`);
  await expect(page.getByTestId("work-log").getByText("Running", { exact: true })).toHaveCount(0);
  await page.reload();
  await expect(page.getByTestId("work-log-failure").last()).toHaveText(`Model call failed: ${creditExplanation}`);
  await expect(page.getByTestId("work-log").getByText("Running", { exact: true })).toHaveCount(0);
  await deleteProject(page, projectId);
});
