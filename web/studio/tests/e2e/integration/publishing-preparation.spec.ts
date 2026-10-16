// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { type Page } from "@playwright/test";
import { openPublishPopover } from "@/tests/e2e/support/publishing";
import { rpcPosted, rpcResult } from "@/tests/e2e/support/studio-api";

// Use an owned, actually failed Release and the real Studio/BFF/gRPC path.
// This scenario observes reads only; it never clicks the provider retry button.
test.use({ storageState: process.env.SUPERVERSE_E2E_STORAGE_STATE, trace: "off", screenshot: "off", video: "off" });

test("reopening a failed Release preparation in the Publish popover does not admit another preparation", async ({
  page,
}) => {
  const project = process.env.SUPERVERSE_E2E_PREPARATION_PROJECT;
  const commit = process.env.SUPERVERSE_E2E_PREPARATION_COMMIT;
  if (!project || !commit || !process.env.SUPERVERSE_E2E_STORAGE_STATE)
    throw new Error("Requires an owned failed Release, exact commit and private authenticated storage state");
  async function read(page: Page) {
    const { release } = await rpcResult(page.request, "GetRelease", { projectId: project, releaseId: commit });
    expect(release?.releaseId).toBe(commit);
    expect(release?.preparedSource?.status).toBe("RELEASE_STATUS_FAILED");
    return release!.revision;
  }
  /** The popover on the failed version, which shows its failed preparation and offers no retry of it. */
  async function openOnVersion(page: Page) {
    const popover = await openPublishPopover(page, project!);
    const version = popover.getByTestId(`publish-version-${commit}`);
    if (!(await version.isVisible())) await popover.getByRole("button", { name: /^Another version/ }).click();
    await version.click();
    const problem = popover.getByTestId("problem-panel");
    await expect(problem).toBeVisible({ timeout: 60_000 });
    await expect(problem).toContainText("Preparing this version");
    return popover;
  }
  const before = await read(page);
  const mutations: string[] = [];
  page.on("request", (request) => {
    if (rpcPosted("PrepareRelease")(request)) mutations.push(request.method());
  });
  // The popover on the version shows its failed preparation; reopening and reloading only read.
  await openOnVersion(page);
  await page.reload();
  await openOnVersion(page);
  await page.goto(`/p/${project}/tools?tool=publishing&commit=${commit}`);
  await openOnVersion(page);
  expect(await read(page)).toBe(before);
  expect(mutations).toHaveLength(0);
});

test("a failed build returns its Release snapshot without source definitions and is visible in Studio", async ({
  page,
}) => {
  const project = process.env.SUPERVERSE_E2E_FAILED_BUILD_PROJECT;
  const commit = process.env.SUPERVERSE_E2E_FAILED_BUILD_COMMIT;
  if (!project || !commit || !process.env.SUPERVERSE_E2E_STORAGE_STATE)
    throw new Error("Requires an owned Release with a real failed build and private authenticated storage state");
  const { release, flowDefinitions } = await rpcResult(page.request, "GetRelease", {
    projectId: project,
    releaseId: commit,
  });
  expect(release?.status).toBe("RELEASE_STATUS_FAILED");
  // This is the build-failed branch, not an unprepared source with no artifacts.
  expect(release?.preparedSource?.status).toBe("RELEASE_STATUS_READY");
  const failed = release?.buildRevisions.find((build) => build.buildRevisionId === release.activeBuildRevisionId);
  expect(failed?.status).toBe("RELEASE_STATUS_FAILED");
  expect(failed?.failure?.code).toBe("BUILD_FAILED");
  expect(release?.failure?.code).toBe(failed?.failure?.code);
  expect(flowDefinitions).toEqual([]);
  if (!failed?.failure?.message) throw new Error("The actual failed build has no failure message");
  const mutations: string[] = [];
  page.on("request", (request) => {
    if (
      (["PrepareRelease", "StartReleaseBuild", "RecoverReleaseBuild"] as const).some((method) =>
        rpcPosted(method)(request),
      )
    )
      mutations.push(request.method());
  });
  // Tools, Publishing lists the version's failed build in its history.
  await page.goto(`/p/${project}/tools?tool=publishing&commit=${commit}`);
  const history = page.getByRole("region", { name: "History", exact: true });
  await expect(history.getByTestId("build-history").first()).toContainText("failed");
  expect(mutations).toHaveLength(0);
});

test("Live Runs keep the deployed build's catalog after a failed rebuild of the same Release", async ({ page }) => {
  const projectId = process.env.SUPERVERSE_E2E_FAILED_REBUILD_PROJECT;
  const releaseId = process.env.SUPERVERSE_E2E_FAILED_REBUILD_COMMIT;
  if (!projectId || !releaseId || !process.env.SUPERVERSE_E2E_STORAGE_STATE)
    throw new Error("Requires an owned Live deployment whose Release has a real failed newer build");
  const { project } = await rpcResult(page.request, "GetProject", { projectId });
  const { release, flowDefinitions } = await rpcResult(page.request, "GetRelease", { projectId, releaseId });
  expect(project?.live?.releaseId).toBe(releaseId);
  expect(project?.live?.isRemoving).toBe(false);
  expect(release?.status).toBe("RELEASE_STATUS_FAILED");
  expect(flowDefinitions).toEqual([]);
  expect(release?.activeBuildRevisionId).not.toBe(release?.readyBuildRevisionId);
  const deployed = release?.buildRevisions.find((build) => build.buildRevisionId === project?.live?.buildRevisionId);
  expect(deployed?.status).toBe("RELEASE_STATUS_READY");
  const releaseReads: string[] = [];
  page.on("request", (request) => {
    if (rpcPosted("GetRelease")(request)) releaseReads.push(request.url());
  });
  const definitions = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname ===
      `/dex/projects/${projectId}/environments/live/releases/${releaseId}/api/flow-definitions`,
  );
  await page.goto(`/p/${projectId}/runs?v=live&release=${releaseId}`);
  const response = await definitions;
  expect(response.ok()).toBe(true);
  expect(response.request().headers()["x-superverse-deployment-id"]).toBe(project?.live?.deploymentId);
  await expect(page.getByRole("button", { name: "Live run", exact: true })).toBeVisible();
  expect(releaseReads).toHaveLength(0);
});
