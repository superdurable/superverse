// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomUUID } from "node:crypto";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { chooseMockFirstInput, waitForFirstMockInput } from "@/tests/e2e/support/mock/mock-composer";
import { createProject, deleteProject, readSession } from "@/tests/e2e/support/project";
import { openPublishPopover } from "@/tests/e2e/support/publishing";
import { rpc, rpcAnswered, rpcPosted, rpcResult } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// The fault is an actual authenticated cancellation of this test's owned
// builder job. Studio, the BFF, Dex, Git, storage and Docker stay real.
test("Publish again retries failed source preparation with a new request and resumes polling", async ({
  page,
}, testInfo) => {
  test.setTimeout(30 * 60_000);
  const tokenFile = process.env.SUPERVERSE_LOCAL_BUILD_TOKEN_FILE;
  const builderURL = process.env.SUPERVERSE_LOCAL_BUILD_URL;
  if (!tokenFile || !builderURL) throw new Error("Requires the local lane's builder URL and private token file");
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  const { projectId } = await createProject(page, teamSpaceId, chooseMockFirstInput("setup-live-versions"), testInfo);
  try {
    await waitForFirstMockInput(page, projectId, "setup-live-versions");
    const commit = (await readSession(page, projectId)).headCommit;
    const firstRequest = randomUUID();
    expect(
      (await rpc(page.request, "PrepareRelease", { projectId, sourceCommit: commit, requestId: firstRequest })).ok(),
    ).toBe(true);
    const statePath = join(dirname(tokenFile), "release-builds", "test", projectId, commit, "source", "state.json");
    let providerRequest: Record<string, unknown> | null = null;
    await expect
      .poll(
        async () => {
          try {
            const state = JSON.parse(await readFile(statePath, "utf8"));
            if (state.state === "RUNNING") providerRequest = state.request;
            return state.state;
          } catch {
            return "MISSING";
          }
        },
        { timeout: 60_000, intervals: [100] },
      )
      .toBe("RUNNING");
    const token = (await readFile(tokenFile, "utf8")).trim();
    // Node fetch keeps the private provider token out of browser traces.
    const cancelled = await fetch(`${builderURL}/releases/cancel`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
      body: JSON.stringify(providerRequest),
      signal: AbortSignal.timeout(30_000),
    });
    expect(cancelled.ok).toBe(true);
    await expect
      .poll(
        async () =>
          (await rpcResult(page.request, "GetRelease", { projectId, releaseId: commit })).release?.preparedSource
            ?.status,
        { timeout: 6 * 60_000, intervals: [2000] },
      )
      .toBe("RELEASE_STATUS_FAILED");
    const { release } = await rpcResult(page.request, "GetRelease", { projectId, releaseId: commit });
    expect(release?.preparedSource?.failure?.remedy).toBe("FAILURE_REMEDY_PUBLISH_AGAIN");

    const requests: string[] = [];
    page.on("request", (request) => {
      if (rpcPosted("PrepareRelease")(request)) requests.push(request.postDataJSON().requestId);
    });
    let popover = await openPublishPopover(page, projectId);
    await expect(popover.getByRole("button", { name: "Publish again", exact: true })).toBeEnabled();
    await page.keyboard.press("Escape");
    popover = await openPublishPopover(page, projectId);
    await page.keyboard.press("Escape");
    expect(requests).toEqual([]);

    // The chat remedy opens the bar's popover on this same version.
    await page.goto(`/p/${projectId}/flow`);
    await page.getByTestId("problem-panel").getByRole("button", { name: "Publish again", exact: true }).first().click();
    popover = page.getByTestId("publish-popover");
    await expect(popover).toBeVisible();
    const [response] = await Promise.all([
      page.waitForResponse(rpcAnswered("PrepareRelease")),
      popover.getByRole("button", { name: "Publish again", exact: true }).click(),
    ]);
    expect(response.ok()).toBe(true);
    expect(requests).toHaveLength(1);
    expect(requests[0]).not.toBe(firstRequest);
    await expect(popover.getByRole("button", { name: "Publish again", exact: true })).toHaveCount(0);
    await expect
      .poll(
        async () =>
          (await rpcResult(page.request, "GetRelease", { projectId, releaseId: commit })).release?.preparedSource
            ?.status,
        { timeout: 6 * 60_000, intervals: [2000] },
      )
      .toBe("RELEASE_STATUS_READY");
    const state = JSON.parse(await readFile(statePath, "utf8"));
    expect(state.request.attempt).toBe(2);
    expect(state.state).toBe("READY");
    expect(requests).toHaveLength(1);
  } finally {
    await deleteProject(page, projectId);
  }
});
