// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { chmod, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { signIn } from "@/tests/e2e/support/account";
import { runsMockModel } from "@/tests/e2e/support/mock/mock-composer";
import {
  createProject,
  expectPushedHead,
  MOCK_TARGET,
  modelTargetDecision,
  readProject,
  readSession,
  typePrompt,
  waitForCompletedTurn,
} from "@/tests/e2e/support/project";
import { publishLive } from "@/tests/e2e/support/publishing";
import { githubJSON, repositoryFile } from "@/tests/e2e/support/repository-host";
import { array, object } from "@/tests/e2e/support/json-assert";
import { ownerFixture } from "@/tests/e2e/support/private-files";
import { privateOwnerStateDirectory, rpcResult } from "@/tests/e2e/support/studio-api";
import { openTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

// This retained journey uses the real Studio, model, GitHub, Sandbox, builder,
// Project Dex and application APIs. Owner credentials remain in private state.
test("sign in, implement a GreetingFlow and execute its durable result on Live", async ({ page, context }, info) => {
  test.setTimeout(75 * 60_000);
  // Skipped, never passed: the mock model has no scenario for this journey, which needs the real model.
  test.skip(
    modelTargetDecision() === MOCK_TARGET,
    "SUPERVERSE_E2E_MODEL_TARGET is mock; this journey needs a real model.",
  );
  const directory = await privateOwnerStateDirectory();
  const loginFile = process.env.SUPERVERSE_E2E_LOGIN_FILE;
  if (!loginFile) throw new Error("A private local test-account file is required");
  const owner = ownerFixture(loginFile, "Private test-account file");
  const record = join(directory, "greeting-flow-journey.json");
  const resumeFile = process.env.SUPERVERSE_E2E_RESUME_FILE;
  const resume = resumeFile ? JSON.parse(await readFile(resumeFile, "utf8")) : null;
  if (resume && typeof resume.projectId !== "string") throw new Error("Invalid retained journey identity");
  let retained: Record<string, unknown> = resume ?? {};
  const save = async (values: Record<string, unknown>) => {
    retained = { ...retained, ...values };
    await writeFile(record, JSON.stringify(retained, null, 2), { mode: 0o600 });
  };

  const loginResponse = page.waitForResponse(
    (response) => new URL(response.url()).pathname === "/api/auth/login" && response.request().method() === "POST",
  );
  await signIn(page, owner);
  expect((await loginResponse).ok()).toBe(true);
  // A fresh owner lands on its first TeamSpace's creation, or on an empty TeamSpace's first Project.
  await expect(
    page
      .getByRole("heading", { name: "Create a project." })
      .or(page.getByRole("heading", { name: "Create your TeamSpace." })),
  ).toBeVisible();
  await save({ loginVerified: true, credentialsFile: loginFile });
  await context.storageState({ path: join(directory, "studio-storage-state.json") });
  await chmod(join(directory, "studio-storage-state.json"), 0o600);

  const prompt = [
    "Implement a simple approval-based GreetingFlow in this new template project.",
    "Use the template Go backend and toolchain; keep its Dex SDK unless a newer stable release is needed.",
    "Replace the reference TextSummaryFlow, its summary-model connection, text-summary endpoints and form with the concrete Go type GreetingFlow, using SDK default Flow and Step identities without naming overrides.",
    'Its durable steps must validate a trimmed nonempty name, wait for the real approval Channel, and complete with the exact result "Hello, <name>!".',
    "Replace the summary form with a real application HTTP start/approve/read path and UI controls named Automation request, Start process and Approve, plus a result element with test ID result.",
    'Change the app main heading to exactly "Local Native Greeting".',
    "Register every retained primitive and update dex-app.yaml, the Flow Definition artifacts, registry/client wiring and documentation.",
    "Use Dex as the durable owner; add no database, connectors or mock dependencies.",
    "Run the template real checks and strict FDG 2.0 validation, commit all application changes and push the accepted default branch.",
    "Complete implementation directly; the requested Flow and existing template UI are already authorized.",
  ].join(" ");
  const created = resume
    ? { projectId: String(resume.projectId), name: String(resume.name) }
    : await createProject(page, await openTeamSpace(page), typePrompt(prompt), info);
  if (resume) await page.goto(`/p/${created.projectId}/flow`);
  // The mock model has no GreetingFlow scenario: the journey needs a Session on the real model. A new
  // Project's Session runs the Sonnet that the creation page chose; a retained one may run the mock model.
  test.skip(
    resume !== null && (await runsMockModel(page, created.projectId)),
    "The retained Session runs the mock model; this journey needs the real model.",
  );
  await save({ ...created, phase: "agent-implementing" });
  await waitForCompletedTurn(page, created.projectId);
  const project = await readProject(page, created.projectId);
  const session = await readSession(page, created.projectId);
  // A retained, already-pushed source may outlive the workspace lease. Release
  // builds read the immutable Git commit, which is verified below.
  if (resume) expect(["WORKSPACE_STATUS_READY", "WORKSPACE_STATUS_CLOSED"]).toContain(session.workspace?.status);
  else expect(session.workspace?.status).toBe("WORKSPACE_STATUS_READY");
  const repository = await expectPushedHead(project, session);
  const tree = array(
    (await githubJSON(`repos/${repository.fullName}/git/trees/${session.headCommit}?recursive=1`)).tree,
    "Source tree",
  ).map((entry) => object(entry, "Source tree entry"));
  const flowPaths = tree
    .filter(
      (entry) =>
        entry.type === "blob" &&
        typeof entry.path === "string" &&
        entry.path.startsWith("internal/process/") &&
        entry.path.endsWith(".go") &&
        !entry.path.endsWith("_test.go"),
    )
    .map((entry) => String(entry.path));
  const sources = await Promise.all(
    flowPaths.map((path) => repositoryFile(repository.fullName, path, session.headCommit)),
  );
  const flowSource = sources.join("\n");
  expect(flowSource).toMatch(/type\s+GreetingFlow\s+struct/);
  expect(flowSource).toContain("Hello, ");
  await save({ repository: repository.fullName, headCommit: session.headCommit, phase: "publishing" });

  await page.goto(`/p/${created.projectId}/tools?tool=publishing`);
  const live = await publishLive(page, created.projectId);
  expect(live.release.sourceCommit).toBe(session.headCommit);
  const { flowDefinitions } = await rpcResult(page.request, "GetRelease", {
    projectId: created.projectId,
    releaseId: live.release.releaseId,
  });
  expect(flowDefinitions.some((definition) => definition.flowType.endsWith("GreetingFlow") && definition.valid)).toBe(
    true,
  );
  await save({
    liveURL: live.url,
    releaseId: live.release.releaseId,
    deploymentId: live.deployment.deploymentId,
    imageDigest: live.imageDigest,
    configurationRevision: live.deployment.configurationRevision,
    phase: "live-executing",
  });

  const app = await context.newPage();
  await app.goto(live.url);
  await expect(app.getByRole("heading", { name: "Local Native Greeting", exact: true })).toBeVisible();
  await app.getByLabel("Automation request", { exact: true }).fill("Local Native");
  await app.getByRole("button", { name: "Start process", exact: true }).click();
  await expect(app.locator("[data-flow-id] .status")).toHaveText("waiting for approval");
  const flowId = await app.locator("[data-flow-id]").getAttribute("data-flow-id");
  expect(flowId).toBeTruthy();
  await save({ applicationFlowId: flowId });
  await app.getByRole("button", { name: "Approve", exact: true }).click();
  await expect(app.getByTestId("result")).toHaveText("Hello, Local Native!");
  await app.reload();
  await expect(app.getByTestId("result")).toHaveText("Hello, Local Native!");
  await app.screenshot({ path: join(directory, "live-greeting.jpg"), fullPage: true });
  await page.screenshot({ path: join(directory, "studio-live.jpg"), fullPage: true });
  await save({ phase: "completed", durableResult: "Hello, Local Native!", completedAt: new Date().toISOString() });
  await info.attach("greeting-flow-identities", {
    contentType: "application/json",
    body: JSON.stringify({
      projectId: created.projectId,
      liveURL: live.url,
      releaseId: live.release.releaseId,
      deploymentId: live.deployment.deploymentId,
      headCommit: session.headCommit,
      applicationFlowId: flowId,
    }),
  });
});
