// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { randomBytes } from "node:crypto";
import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { identity, register, signIn } from "@/tests/e2e/support/account";
import { chooseMockFirstInput, choosesMockModel, waitForFirstMockInput } from "@/tests/e2e/support/mock/mock-composer";
import {
  createProject,
  expectPushedHead,
  readProject,
  readSession,
  typePrompt,
  waitForCompletedTurn,
} from "@/tests/e2e/support/project";
import { headingChangePrompt, previewTemplateEdit } from "@/tests/e2e/support/preview/preview";
import { publishTemplateEditLive } from "@/tests/e2e/support/publishing";
import { privateOwnerStateDirectory } from "@/tests/e2e/support/studio-api";
import { openTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// Retained journey: the Project, its Session, its Preview and its Live deployment
// stay in place for later inspection. The owner credential is written only to the
// private owner-state directory and never to a report or attachment.
test("new account signs in, builds a template project, previews it and deploys Live without cleanup", async ({
  page,
  context,
}, info) => {
  test.setTimeout(75 * 60_000);
  const directory = await privateOwnerStateDirectory();
  const record = join(directory, `journey-new-project-${Date.now()}-${randomBytes(4).toString("hex")}.json`);
  const save = (value: Record<string, unknown>) => writeFile(record, JSON.stringify(value, null, 2), { mode: 0o600 });

  const account = await register(page, identity());
  await save({ account });
  await page.locator('summary[aria-label="Account"]').click();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  await signIn(page, account);
  const teamSpaceId = await openTeamSpace(page);

  const marker = `V2 journey ${randomBytes(3).toString("hex")}`;
  // The creation page takes the first message: the heading change as a prompt, or on the mock model
  // input 1 of the build-basic scenario with the marker as its heading, as SUPERVERSE_E2E_MODEL_TARGET
  // or the lane decides. CreateProject carries it, the Session starts with it, and that first turn
  // provisions the Session's Sandbox on demand.
  const mock = await choosesMockModel(page);
  const created = await createProject(
    page,
    teamSpaceId,
    mock ? chooseMockFirstInput("build-basic", { heading: marker }) : typePrompt(headingChangePrompt(marker)),
    info,
  );
  await save({ account, projectId: created.projectId });
  if (mock) await waitForFirstMockInput(page, created.projectId, "build-basic");
  else await waitForCompletedTurn(page, created.projectId);
  const project = await readProject(page, created.projectId);
  const session = await readSession(page, created.projectId);
  expect(session.workspace?.status).toBe("WORKSPACE_STATUS_READY");
  const repository = await expectPushedHead(project, session);

  const { app, previewURL } = await previewTemplateEdit(page, context, created.projectId, marker);
  const live = await publishTemplateEditLive(page, app, created.projectId, marker, session.headCommit);

  const identities = {
    projectId: created.projectId,
    repository: repository.fullName,
    headCommit: session.headCommit,
    previewURL,
    liveURL: live.url,
    releaseId: live.release.releaseId,
    deploymentId: live.deployment.deploymentId,
    imageDigest: live.imageDigest,
  };
  await save({ account, ...identities });
  await info.attach("journey-new-project", { contentType: "application/json", body: JSON.stringify(identities) });
});
