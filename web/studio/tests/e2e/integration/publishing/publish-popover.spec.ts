// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { configure, fillTemplateSummaryConnection } from "@/tests/e2e/support/configuration";
import {
  applyPreviewValue,
  fillEnvironmentValue,
  fillLiveSummaryAnd,
  FIXTURE_SKIP_REASON,
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
import { openPublishPopover, publishButton, publishLive, removeLive } from "@/tests/e2e/support/publishing";
import { rpc, rpcResult } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// The Publish popover on the real Studio, BFF, server, Dex and local Git host, with the mock model pushing
// two versions: the first declares SUMMARY_GREETING and never runs on Preview; the second declares
// SUMMARY_TONE instead, and Preview runs it with a value for it.

/** Waits until the Release of `commit` has its source prepared. */
async function preparedRelease(page: Page, projectId: string, commit: string) {
  const prepared = await rpc(page.request, "PrepareRelease", {
    projectId,
    requestId: randomUUID(),
    sourceCommit: commit,
  });
  expect(prepared.ok()).toBe(true);
  await expect
    .poll(
      async () =>
        (await rpcResult(page.request, "GetRelease", { projectId, releaseId: commit })).release?.preparedSource?.status,
      { timeout: 10 * 60_000, intervals: [2000] },
    )
    .toBe("RELEASE_STATUS_READY");
  const { release } = await rpcResult(page.request, "GetRelease", { projectId, releaseId: commit });
  if (!release) throw new Error("The prepared Release is unavailable");
  return release;
}

test("Publish needs a Preview run or a reason, states its consequences and publishes Live's waiting settings", async ({
  page,
}, testInfo) => {
  test.setTimeout(120 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  expect(await choosesMockModel(page), "The Publish popover runs its two versions on the mock model").toBe(true);
  const created = await createProject(page, teamSpaceId, chooseMockFirstInput("setup-live-versions"), testInfo);
  const projectId = created.projectId;
  let liveRelease = "";
  try {
    await waitForFirstMockInput(page, projectId, "setup-live-versions");
    await keepWorkspaceRunning(page, projectId);
    const older = (await readSession(page, projectId)).headCommit;
    await configure(page, "Preview", fillTemplateSummaryConnection);

    // A version that never ran on Preview: the popover asks for a reason and the server refuses a build
    // without one.
    const popover = await openPublishPopover(page, projectId);
    const row = popover.getByTestId(`publish-version-${older}`);
    await row.click();
    await expect(row).toContainText("not in the Preview record");
    const publish = publishButton(popover);
    await expect(publish).toBeDisabled();
    const reason = popover.getByLabel(/Why publish it without a Preview run\?/);
    await reason.fill("x".repeat(501));
    await expect(popover).toContainText("Shorten the reason to 500 characters (now 501).");
    await expect(publish).toBeDisabled();
    await reason.fill("   ");
    await expect(publish).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(popover).toHaveCount(0);

    const release = await preparedRelease(page, projectId, older);
    const refused = await rpc(page.request, "StartReleaseBuild", {
      projectId,
      requestId: randomUUID(),
      releaseId: older,
      buildRevisionId: `build-${randomUUID()}`,
      configurationRevision: "1",
      expectedRevision: release.revision,
      autoDeploy: true,
      expectedLiveRevision: "0",
    });
    expect(refused.status()).toBe(409);
    expect((await refused.json()).errorInfo?.reason).toBe("PREVIEW_VALIDATION_REQUIRED");

    const first = await publishVersion(page, projectId, older, fillLiveSummaryAnd("SUMMARY_GREETING", "Hello"));
    liveRelease = first.release.releaseId;
    await page.goto(`/p/${projectId}/tools?tool=publishing&commit=${older}`);
    await expect(page.getByRole("region", { name: "History", exact: true })).toContainText(
      `Published without running on Preview: ${FIXTURE_SKIP_REASON}`,
    );

    // The newer version ran on Preview; the popover states what publishing it changes.
    await page.goto(`/p/${projectId}/flow`);
    await runMockScenario(page, projectId, "setup-live-versions", { inputs: [2] });
    const newer = (await readSession(page, projectId)).headCommit;
    expect(newer).not.toBe(older);
    await applyPreviewValue(page, projectId, "SUMMARY_TONE", "friendly");
    const olderVersion = (await readProject(page, projectId)).live?.version;
    expect(olderVersion).toBeTruthy();
    const second = await publishVersion(page, projectId, newer, fillTemplateSummaryConnection, {
      skipReason: undefined,
      checkConsequences: async (shown) => {
        await expect(shown.getByTestId("publish-headline")).toContainText(`Replaces V${olderVersion?.number}`);
        await expect(shown.getByTestId("publish-replaces")).toContainText(`Replaces V${olderVersion?.number}.`);
        // The details are folded under the one-line summary.
        const consequences = shown.getByTestId("publish-consequences");
        await consequences.getByText("Details", { exact: true }).click();
        await expect(consequences).toContainText("New on Live: environment SUMMARY_TONE.");
        await expect(consequences).toContainText("No longer used: environment SUMMARY_GREETING.");
        await consequences.getByRole("button", { name: "The connections Live uses" }).click();
        const identities = shown.getByTestId("live-identities");
        await expect(identities).toContainText("summary-model");
        await expect(identities).toContainText("Teamspace connection");
        await expect(shown.getByTestId(`publish-version-${newer}`)).toContainText("ran on Preview at");
      },
    });
    liveRelease = second.release.releaseId;
    expect(liveRelease).toBe(newer);

    // Live's own version again with a waiting setting: the same code, only its configuration changes.
    const third = await publishLive(
      page,
      projectId,
      (current, side) => fillEnvironmentValue(current, "SUMMARY_TONE", "formal", side),
      {
        mode: "configuration",
        checkConsequences: async (shown) => {
          await expect(shown.getByTestId("publish-headline")).toHaveText(
            /^V\d+ again · \d+ setting changes?( · about .+)?$/,
          );
          const consequences = shown.getByTestId("publish-consequences");
          await expect(consequences).toContainText("environment SUMMARY_TONE changes (Live now: friendly).");
          await expect(shown.getByRole("radiogroup")).toHaveCount(0);
        },
      },
    );
    expect(third.release.releaseId).toBe(newer);
    expect(third.deployment.deploymentId).not.toBe(second.deployment.deploymentId);
    expect(third.deployment.configurationRevision).not.toBe(second.deployment.configurationRevision);
  } finally {
    if (liveRelease) await removeLive(page, projectId, liveRelease);
    await deleteProject(page, projectId);
  }
});
