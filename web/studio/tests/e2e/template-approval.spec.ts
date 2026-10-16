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
import { exerciseApprovalInStudio } from "@/tests/e2e/support/flow-run";
import {
  chooseMockFirstInput,
  choosesMockModel,
  runMockScenario,
  waitForFirstMockInput,
} from "@/tests/e2e/support/mock/mock-composer";
import {
  createProject,
  deleteProject,
  expectPushedHead,
  readProject,
  readSession,
  typePrompt,
  waitForCompletedTurn,
  waitForRequirementsCard,
} from "@/tests/e2e/support/project";
import { awaitDeployedPreview, expectSurfacesFollowVersion } from "@/tests/e2e/support/preview/preview";
import { publishLive, removeLive } from "@/tests/e2e/support/publishing";
import {
  addProjectMember,
  addTeamSpaceMember,
  createTeamSpace,
  openAsOperator,
} from "@/tests/e2e/support/teamspace/teamspace";

// The only prompt of the case. It names the template's real identifiers and the
// in-repository skill section, so the Agent needs no follow-up question, and asks
// for the requirements card before any change.
const APPROVAL_PROMPT = [
  "In internal/process/text_summary_flow.go, add a human approval before the summary is saved.",
  "Add a Step AwaitSummaryApproval between GenerateSummary and SaveSummaryResult: route the GenerateSummary Generated branch to it and leave its other branches unchanged.",
  'Its WaitFor sets summary-status to "awaiting-approval", which Display shows while the run waits, and waits for one message on a new Channel SummaryApprovals; its Execute passes the generated result on to SaveSummaryResult.',
  'Add an Action RPC ApproveSummary with no input, label "Approve summary", condition summary-status == "awaiting-approval" and permission summary.approve; it rechecks that status and publishes to SummaryApprovals directly in the RPC method.',
  'Follow AGENTS.md and "Answer a waiting Step" in /opt/superverse/dex-skills/dex-app-builder/references/fdg-authoring.md. Change nothing else.',
  "Confirm these requirements with confirm_requirements before you change any file.",
  "Run go build ./... and make check-fdg-v2, then commit and push to the default branch with commit_and_push.",
].join(" ");

test("template project Agent adds a summary approval that Preview and an operator on Live start, approve and complete", async ({
  page,
  browser,
}, testInfo) => {
  // One implementation turn, a Preview and a Live build and deployment fit the
  // runner's 60-minute bound for this case.
  test.setTimeout(55 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  // The creation page takes the first message of the Project's Session: the prompt, or on the mock
  // model input 1 of the scenario, as SUPERVERSE_E2E_MODEL_TARGET or the lane decides; the Builder sends it
  // once the Session starts.
  const mock = await choosesMockModel(page);
  const created = await createProject(
    page,
    teamSpaceId,
    mock ? chooseMockFirstInput("summary-approval") : typePrompt(APPROVAL_PROMPT),
    testInfo,
  );
  // The first turn ends at the requirements card, before any change.
  if (mock) await waitForFirstMockInput(page, created.projectId, "summary-approval");
  else await waitForRequirementsCard(page, created.projectId, 10 * 60_000);
  // The scripted card carries the Flow design of the approval. The card arrived while the Flow tab was open, so
  // the tab shows it at once as its Process Brief: a summary grid above the drawn design, where the mock composer
  // answers the card on the mock model. The chat leaves out its pointer while the Brief shows. Behind the Brief's
  // More details are the outline, one line per Flow with the main story in reading order and the
  // failure-handling count, and what the design changes against the template's initial design in the names
  // the canvas shows.
  if (mock) {
    await expect(page).toHaveURL(/[?&]flowType=TextSummaryFlow(&|$)/);
    const brief = page.getByRole("region", { name: "Brief", exact: true });
    await expect(brief.getByText("Waiting for you", { exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Open the brief" })).toHaveCount(0);
    await brief.locator('[data-more-details="brief.more"]').getByRole("button").click();
    await expect(brief.getByRole("region", { name: "Outline", exact: true })).toContainText(
      "TextSummaryFlow: PrepareSummary, GenerateSummary, AwaitSummaryApproval [Approve summary], SaveSummaryResult, RecordSummaryFailure; +1 failure-handling Step",
    );
    await expect(brief.getByRole("list", { name: "What the design changes" })).toHaveText(
      "Text summary · changed: adds Await summary approval, Approve summary; changes Generate summary",
    );
    // Below the summary the diagram shows its marks, fitted whole: the Result stage that holds the approval Step
    // is changed and the Step new, GenerateSummary changed, and nothing starts a run.
    const design = page.getByRole("region", { name: "Flow design" });
    await expect(design).toContainText("Design · waiting for your confirmation");
    await design.getByRole("button", { name: "Fit", exact: true }).click();
    await design.getByRole("button", { name: /^Result, 3 steps, changed\. Open the stage\.$/ }).click();
    await expect(design.getByRole("button", { name: /^Await summary approval\b.*, new$/ })).toBeVisible();
    await expect(design.getByRole("button", { name: /^Generate summary\b.*, changed$/ })).toBeVisible();
    await expect(design.getByRole("button", { name: /^(Test run|Live run|Start)/ })).toHaveCount(0);
  }
  // While the card waits, the person configures the Project's Preview, so the push of the
  // confirmed change deploys itself.
  await page.goto(`/p/${created.projectId}/tools?tool=publishing`);
  await configure(page, "Preview", fillTemplateSummaryConnection);
  await page.goto(`/p/${created.projectId}/flow`);
  // Input 2 confirms the card through the mock composer and builds the approval; with a real model
  // the completed-turn wait confirms it with Confirm and build on the Process Brief.
  if (mock)
    await runMockScenario(page, created.projectId, "summary-approval", {
      inputs: [2],
      timeoutMs: 25 * 60_000,
      // Until the push builds it, the Flow tab names the confirmed design as building.
      whileRunning: () =>
        expect(page.getByRole("status", { name: "Flow design" })).toContainText("Design · building", {
          timeout: 60_000,
        }),
    });
  else await waitForCompletedTurn(page, created.projectId, -1, 25 * 60_000);
  const session = await readSession(page, created.projectId);
  await expectPushedHead(await readProject(page, created.projectId), session);
  // The scripted push, without blocked, completes the build of the confirmed design.
  if (mock)
    expect(
      session.agent?.description?.confirmedRequirements?.spec?.designBuilt,
      "the push built the confirmed design",
    ).toBe(true);

  await awaitDeployedPreview(page, created.projectId, session.headCommit);
  // The processes built from that push replace the design: the implemented Flow draws without its label.
  if (mock) {
    await page.goto(`/p/${created.projectId}/flow?flowType=TextSummaryFlow`);
    const implemented = page.getByRole("region", { name: "Flow definition" });
    await expect(implemented.getByRole("button", { name: /^Result, 3 steps\. Open the stage\.$/ })).toBeVisible({
      timeout: 5 * 60_000,
    });
    await expect(page.getByRole("region", { name: "Flow design" })).toHaveCount(0);
    await expect(page.getByRole("status", { name: "Flow design" })).toHaveCount(0);
  }
  await exerciseApprovalInStudio(page, created.projectId, "preview");

  const live = await publishLive(page, created.projectId, fillTemplateSummaryConnection);
  expect(live.release.sourceCommit).toBe(session.headCommit);
  // A second account joins the Teamspace through People and the Project as its operator through Permissions,
  // then sees Live only and approves the Live run there.
  const operator = await browser.newContext({ baseURL: process.env.SUPERVERSE_E2E_BASE_URL });
  const operatorPage = await operator.newPage();
  const operatorAccount = await register(operatorPage);
  await addTeamSpaceMember(page, teamSpaceId, operatorAccount.email, "member");
  await addProjectMember(page, created.projectId, operatorAccount.email, "operator");
  await openAsOperator(operatorPage, created.projectId);
  await exerciseApprovalInStudio(operatorPage, created.projectId, "live", live.release.releaseId);
  await operator.close();
  // The bar's Preview | Live switch sets what every surface shows.
  await expectSurfacesFollowVersion(page, created.projectId, live.url);

  await removeLive(page, created.projectId, live.release.releaseId);
  await deleteProject(page, created.projectId);
  await testInfo.attach("accepted-source", {
    body: JSON.stringify({
      projectId: created.projectId,
      remoteSHA: session.headCommit,
      imageDigest: live.imageDigest,
      releaseId: live.release.releaseId,
    }),
    contentType: "application/json",
  });
});
