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
  waitForFirstMockInput,
  runMockScenario,
} from "@/tests/e2e/support/mock/mock-composer";
import { setAutomaticBuilds } from "@/tests/e2e/support/configuration";
import { createProject, deleteProject, readSession } from "@/tests/e2e/support/project";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// Real Studio/BFF/server/Dex/Git/storage; only the coding model is substituted.
test("pushes build processes automatically, independently of Preview settings, until switched off", async ({
  page,
}, testInfo) => {
  test.setTimeout(30 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  if (!(await choosesMockModel(page))) throw new Error("Auto refresh runs on the mock model");
  const { projectId } = await createProject(
    page,
    teamSpaceId,
    chooseMockFirstInput("process-auto-refresh", { note: `Processes ${Date.now().toString(36)}` }),
    testInfo,
  );
  try {
    await waitForFirstMockInput(page, projectId, "process-auto-refresh");
    // The switch lives in Project details and is on for a new Session.
    await page.goto(`/p/${projectId}`);
    const automatic = page
      .getByRole("region", { name: "Automatic builds", exact: true })
      .getByRole("switch", { name: "Build each push automatically", exact: true });
    await expect(automatic).toHaveAttribute("aria-checked", "true", { timeout: 60_000 });
    await page.goto(`/p/${projectId}/tools?tool=setup`);
    const control = page.getByRole("region", { name: "Build processes", exact: true });
    await expect(control).toBeVisible({ timeout: 2 * 60_000 });
    await expect(control.getByRole("link", { name: "Automatic builds are on", exact: true })).toBeVisible();
    await expect(control).toContainText(/Processes of V[0-9]+ ready/, { timeout: 10 * 60_000 });
    await expect(control.getByTestId("preview-current-commit")).toHaveText(/^V[0-9]+ · .+/);
    await expect(control.getByRole("button", { name: /^Build (processes|V[0-9]+ processes again)$/ })).toHaveCount(0);
    const first = await readSession(page, projectId);
    expect(first.preview?.preparedSource?.sourceCommit).toBe(first.headCommit);
    expect(first.preview?.preparedSource?.status).toBe("PREVIEW_SOURCE_STATUS_READY");
    await expect
      .poll(async () => (await readSession(page, projectId)).preview?.automaticDeployment?.status, {
        timeout: 5 * 60_000,
      })
      .toBe("PREVIEW_DEPLOYMENT_STATUS_INCOMPATIBLE");
    await expect(page.getByTestId("setup-checklist")).toBeVisible();

    // Off suppresses the next push's build; a manual build still works.
    await setAutomaticBuilds(page, projectId, false);
    await page.goto(`/p/${projectId}/tools?tool=setup`);
    await expect(control.getByRole("button", { name: "Build processes", exact: true })).toBeVisible();
    await page.goto(`/p/${projectId}/flow`);
    await runMockScenario(page, projectId, "process-auto-refresh", { inputs: [2] });
    const pushed = await readSession(page, projectId);
    expect(pushed.headCommit).not.toBe(first.headCommit);
    expect(pushed.preview?.preparedSource?.sourceCommit).toBe(first.headCommit);
    await page.goto(`/p/${projectId}/tools?tool=setup`);
    await control.getByRole("button", { name: "Build processes", exact: true }).click();
    await expect(control).toContainText(/Processes of V[0-9]+ ready/, { timeout: 10 * 60_000 });
    expect((await readSession(page, projectId)).preview?.preparedSource?.sourceCommit).toBe(pushed.headCommit);
    // A real dirty-workspace refusal records FAILED and offers an explicit retry.
    await page.goto(`/p/${projectId}/flow`);
    await runMockScenario(page, projectId, "process-auto-refresh", { inputs: [3] });
    await page.goto(`/p/${projectId}/tools?tool=setup`);
    await control.getByRole("button", { name: "Build processes", exact: true }).click();
    await expect
      .poll(async () => (await readSession(page, projectId)).preview?.preparedSource?.status, { timeout: 5 * 60_000 })
      .toBe("PREVIEW_SOURCE_STATUS_FAILED");
    const failed = (await readSession(page, projectId)).preview?.preparedSource;
    expect(failed?.failure?.code).toBe("PREVIEW_DIRTY_HEAD");
    const retry = control.getByRole("button", { name: "Build processes again", exact: true });
    await expect(retry).toBeVisible();
    await retry.click();
    await expect
      .poll(async () => (await readSession(page, projectId)).preview?.preparedSource?.requestId)
      .not.toBe(failed?.requestId);
    await expect
      .poll(async () => (await readSession(page, projectId)).preview?.preparedSource?.status, { timeout: 5 * 60_000 })
      .toBe("PREVIEW_SOURCE_STATUS_FAILED");

    // Commit the real uncommitted change, then the manual build can succeed again.
    await page.goto(`/p/${projectId}/flow`);
    await runMockScenario(page, projectId, "process-auto-refresh", { inputs: [4] });
    await page.goto(`/p/${projectId}/tools?tool=setup`);
    await control.getByRole("button", { name: "Build processes", exact: true }).click();
    await expect(control).toContainText(/Processes of V[0-9]+ ready/, { timeout: 10 * 60_000 });
    await setAutomaticBuilds(page, projectId, true);
    await page.goto(`/p/${projectId}/tools?tool=setup`);
    await expect(control.getByRole("button", { name: "Build processes", exact: true })).toHaveCount(0);
  } finally {
    await deleteProject(page, projectId);
  }
});
