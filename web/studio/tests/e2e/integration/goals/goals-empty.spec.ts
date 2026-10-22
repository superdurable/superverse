// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomUUID } from "node:crypto";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { chooseMockFirstInput, choosesMockModel, waitForFirstMockInput } from "@/tests/e2e/support/mock/mock-composer";
import { createProject, deleteProject } from "@/tests/e2e/support/project";
import { rpc, rpcAnswered } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// A Project that was never published has no goal measurement: EvolveFlow starts with the first Live deployment, so
// GetEvolve and Review now answer NOT_FOUND through the BFF, and the Goals tab shows its empty state with Set goals.
// The mock model answers the first message; the server, Dex and the Git host are the lane's own.
test("a Project never published shows the Goals empty state from GetEvolve's NOT_FOUND", async ({ page }, testInfo) => {
  test.setTimeout(20 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  if (!(await choosesMockModel(page))) throw new Error("The Goals empty state case runs on the mock model");
  const { projectId } = await createProject(page, teamSpaceId, chooseMockFirstInput("explain"), testInfo);
  await waitForFirstMockInput(page, projectId, "explain");

  expect((await rpc(page.request, "GetEvolve", { projectId })).status()).toBe(404);
  expect((await rpc(page.request, "StartEvolveReview", { projectId, requestId: randomUUID() })).status()).toBe(404);

  const read = page.waitForResponse((response) => rpcAnswered("GetEvolve")(response) && response.status() !== 503);
  await page.goto(`/p/${projectId}/goals`);
  expect((await read).status()).toBe(404);
  // The tab a link names shows before the Builder reveals it on its own.
  await expect(
    page.getByRole("navigation", { name: "Surface", exact: true }).getByRole("link", { name: "Goals", exact: true }),
  ).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("heading", { name: "No goals yet", exact: true })).toBeVisible();
  await expect(page.getByText("Tell the agent what this app is for", { exact: false })).toBeVisible();
  await expect(page.getByRole("button", { name: "Set goals", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Review now", exact: true })).toHaveCount(0);
  await expect(page.getByTestId("goals-surface")).toHaveCount(0);
  await deleteProject(page, projectId);
});
