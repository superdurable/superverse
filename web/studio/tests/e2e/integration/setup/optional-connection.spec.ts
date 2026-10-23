// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { connectionPicker, openPreviewConfiguration } from "@/tests/e2e/support/configuration";
import { chooseMockFirstInput, choosesMockModel, waitForFirstMockInput } from "@/tests/e2e/support/mock/mock-composer";
import { createProject, deleteProject, readSession } from "@/tests/e2e/support/project";
import { rpcResult } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// The real Native/Kind app has no selected summary connection. Only the coding model is mocked.
test("the default example previews without an LLM and Setup labels its connection optional", async ({
  page,
  context,
}, info) => {
  test.setTimeout(30 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  expect(await choosesMockModel(page)).toBe(true);
  const { projectId } = await createProject(
    page,
    teamSpaceId,
    chooseMockFirstInput("build-basic", { heading: "Optional connection preview" }),
    info,
  );
  try {
    await waitForFirstMockInput(page, projectId, "build-basic");
    let previewURL = "";
    await expect
      .poll(
        async () => {
          const { preview, headCommit } = await readSession(page, projectId);
          if (preview?.failure) throw new Error(`Preview failed: ${preview.failure.code}`);
          if (preview?.status !== "PREVIEW_STATUS_READY" || preview.sourceCommit !== headCommit) return false;
          previewURL = preview.url;
          return true;
        },
        { timeout: 10 * 60_000, intervals: [1000, 2000] },
      )
      .toBe(true);
    const { connections } = await rpcResult(page.request, "ListTeamSpaceConnections", {
      teamSpaceId,
      connectorId: "llm",
    });
    expect(connections).toEqual([]);
    await openPreviewConfiguration(page, projectId);
    const picker = await connectionPicker(page.getByTestId("setup-checklist"), "summary-model", "preview");
    await expect(picker).toContainText("Optional · Not configured");
    await expect(picker).toHaveAttribute("data-tone", "neutral");
    const { configuration } = await rpcResult(page.request, "GetProjectConfiguration", {
      target: { projectId, scope: "PROJECT_SCOPE_PREVIEW" },
    });
    expect(configuration?.status).toBe("CONFIGURATION_STATUS_READY");
    expect(configuration?.requirementGaps).toEqual([]);
    expect(configuration?.appManifest?.connectors[0]?.optional).toBe(true);
    const app = await context.newPage();
    await app.goto(previewURL);
    await expect(app.getByRole("heading", { name: "Optional connection preview", exact: true })).toBeVisible();
    await app
      .getByLabel("Text to summarize", { exact: true })
      .fill("The page works before its optional model is connected.");
    await app.getByRole("button", { name: "Summarize", exact: true }).click();
    await expect(app.getByRole("alert")).toContainText("Connect an LLM to summarize", { timeout: 60_000 });
    await expect(app.getByRole("alert")).toContainText("summary-model");
    await expect(app.getByRole("button", { name: "Summarize", exact: true })).toBeEnabled();
    await app.close();
  } finally {
    await deleteProject(page, projectId);
  }
});
