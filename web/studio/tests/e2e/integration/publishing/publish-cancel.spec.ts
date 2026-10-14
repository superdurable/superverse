// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { configure, fillTemplateSummaryConnection } from "@/tests/e2e/support/configuration";
import {
  FIXTURE_SKIP_REASON,
  fillLiveSummaryAnd,
  publishVersion,
} from "@/tests/e2e/support/configuration/live-versions";
import {
  chooseMockFirstInput,
  choosesMockModel,
  runMockScenario,
  waitForFirstMockInput,
} from "@/tests/e2e/support/mock/mock-composer";
import {
  createProject,
  deleteProject,
  keepWorkspaceRunning,
  readProject,
  readSession,
} from "@/tests/e2e/support/project";
import { chooseVersion, openPublishPopover, publishFromPopover, removeLive } from "@/tests/e2e/support/publishing";
import { rpcAnswered, rpcResult, UUID } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// Cancelling a publish on the real Studio, BFF, server, Dex, builder and local Git host, with the mock
// model pushing two versions: Live runs the first; the second's publish is cancelled during its build.
// The running Live is untouched, the build ends CANCELLED with who cancelled it, and Live's history lists
// the cancelled publish.

test("Cancel stops a running build; Live keeps running and its history lists the cancelled publish", async ({
  page,
}, testInfo) => {
  test.setTimeout(90 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  expect(await choosesMockModel(page), "The cancellation case runs its two versions on the mock model").toBe(true);
  const created = await createProject(page, teamSpaceId, chooseMockFirstInput("setup-live-versions"), testInfo);
  const projectId = created.projectId;
  let liveRelease = "";
  try {
    await waitForFirstMockInput(page, projectId, "setup-live-versions");
    await keepWorkspaceRunning(page, projectId);
    const older = (await readSession(page, projectId)).headCommit;
    await configure(page, "Preview", fillTemplateSummaryConnection);
    const first = await publishVersion(page, projectId, older, fillLiveSummaryAnd("SUMMARY_GREETING", "Hello"));
    liveRelease = first.release.releaseId;
    const live = (await readProject(page, projectId)).live;
    if (!live) throw new Error("The first publish left no Live");

    await page.goto(`/p/${projectId}/flow`);
    await runMockScenario(page, projectId, "setup-live-versions", { inputs: [2] });
    const newer = (await readSession(page, projectId)).headCommit;
    expect(newer).not.toBe(older);
    const popover = await openPublishPopover(page, projectId);
    await chooseVersion(popover, newer, FIXTURE_SKIP_REASON);
    const { response } = await publishFromPopover(page, popover);
    expect(response.ok()).toBe(true);
    const { buildRevisionId } = await response.json();

    // The build runs: Check and Lock settings are done, Build runs, and Cancel stops it.
    const progress = page.getByTestId("publish-progress");
    const stages = progress.getByTestId("publish-stages");
    await expect(stages.locator('[data-stage="build"]')).toHaveAttribute("data-state", "running", {
      timeout: 5 * 60_000,
    });
    await progress.getByTestId("cancel-publish").click();
    const confirm = page.getByTestId("cancel-publish-confirm");
    await expect(confirm).toContainText(`Live keeps running V${live.version?.number}`);
    const [cancelled] = await Promise.all([
      page.waitForResponse(rpcAnswered("CancelReleaseBuild")),
      confirm.getByRole("button", { name: "Stop publishing" }).click(),
    ]);
    expect(cancelled.ok(), "the build is cancelled while it builds").toBe(true);
    expect(cancelled.request().postDataJSON()).toEqual({
      projectId,
      releaseId: newer,
      buildRevisionId,
      requestId: expect.stringMatching(UUID),
    });
    await expect(progress).toHaveAttribute("data-outcome", "cancelled", { timeout: 10 * 60_000 });
    await expect(progress).toContainText("Publishing cancelled");
    await expect(progress).toContainText(`Live still runs V${live.version?.number}`);
    await expect(stages.locator('[data-stage="build"]')).toHaveAttribute("data-state", "stopped");

    // Live is untouched, and the build records the person's cancellation.
    const after = (await readProject(page, projectId)).live;
    expect(after?.deploymentId).toBe(live.deploymentId);
    expect(after?.revision).toBe(live.revision);
    const release = (await rpcResult(page.request, "GetRelease", { projectId, releaseId: newer })).release;
    const build = release?.buildRevisions.find((item) => item.buildRevisionId === buildRevisionId);
    expect(build?.status).toBe("RELEASE_STATUS_CANCELLED");
    expect(build?.cancellation?.actorAccountId).toBeTruthy();
    expect(build?.autoDeploymentId ?? "").toBe("");

    // Live's history lists the cancelled publish beside the first one.
    const history = page.getByTestId("live-history");
    await page.getByRole("button", { name: "Refresh", exact: true }).first().click();
    const cancelledRow = history.locator('[data-kind="KIND_PUBLISH_CANCELLED"]').first();
    await expect(cancelledRow).toContainText("cancelled publishing", { timeout: 60_000 });
    await expect(cancelledRow).toContainText("Live kept running what it ran");
    await expect(history.locator('[data-kind="KIND_PUBLISHED"]').first()).toBeVisible();
  } finally {
    if (liveRelease) await removeLive(page, projectId, liveRelease);
    await deleteProject(page, projectId);
  }
});
