// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Page } from "@playwright/test";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { fillTemplateSummaryConnection, setAutomaticBuilds } from "@/tests/e2e/support/configuration";
import { exerciseApprovalInStudio } from "@/tests/e2e/support/flow-run";
import {
  chooseMockFirstInput,
  choosesMockModel,
  sendMockMessage,
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
import { configureAndStartPreview, expectSurfacesFollowVersion } from "@/tests/e2e/support/preview/preview";
import { publishLive, removeLive } from "@/tests/e2e/support/publishing";
import {
  addProjectMember,
  addTeamSpaceMember,
  createTeamSpace,
  openAsOperator,
} from "@/tests/e2e/support/teamspace/teamspace";

// The only prompt of the case. It names the template's real identifiers and the
// in-repository skill section, so the Agent needs no follow-up question, and asks
// for the requirements card before any change; the card does not wait, so the
// same turn builds it.
const APPROVAL_PROMPT = [
  "In internal/process/text_summary_flow.go, add a human approval before the summary is saved.",
  "Add a Step AwaitSummaryApproval between GenerateSummary and SaveSummaryResult: route the GenerateSummary Generated branch to it and leave its other branches unchanged.",
  'Its WaitFor sets summary-status to "awaiting-approval", which Display shows while the run waits, and waits for one message on a new Channel SummaryApprovals; its Execute passes the generated result on to SaveSummaryResult.',
  'Add an Action RPC ApproveSummary with no input, label "Approve summary", condition summary-status == "awaiting-approval" and permission summary.approve; it rechecks that status and publishes to SummaryApprovals directly in the RPC method.',
  'Follow AGENTS.md and "Answer a waiting Step" in /opt/superverse/dex-skills/dex-app-builder/references/fdg-authoring.md. Change nothing else.',
  "Show these requirements with confirm_requirements before you change any file, then build them.",
  "Run go build ./... and make check-fdg-v2, then commit and push to the default branch with commit_and_push.",
].join(" ");

/**
 * On the mock model the card's reply streams for about 16 seconds: from its first draft until the card is
 * recorded, the Flow tab, to which the first conversation switches, shows the draft, with "Drafting" on the
 * canvas's corner and the designed Steps' names as a row of dashed boxes. The mock writes the card's keys in
 * sorted order, so its design, with the names of its Steps, arrives first and its title last. Once the card is
 * recorded the drawn design replaces the boxes under the overview the agent builds from: "Building · N of 6
 * steps written" on the corner, the card's own summary under the canvas, no Confirm and build and no Change
 * something…, and behind More details, in its nested code names, the outline, one line per Flow with the main
 * story in reading order and the failure-handling count, and what the design changes against the template's
 * initial design in the names the canvas shows. The script's edits, checks and gated push take minutes, so
 * the design is still being built while the overview is read. It settles to the failure, if any, so that the
 * watch can run beside the Project's creation.
 */
async function overviewShown(page: Page): Promise<unknown> {
  try {
    const drafting = page.locator('[data-brief-state="drafting"]');
    await expect(drafting.getByTestId("design-status")).toHaveText("Drafting", { timeout: 25 * 60_000 });
    await expect(drafting.getByRole("region", { name: "Drafted steps", exact: true })).toContainText("Prepare summary");
    const building = page.locator('[data-brief-state="building"]');
    await expect(building.getByTestId("design-status")).toHaveText(/^Building · \d of 6 steps written$/, {
      timeout: 10 * 60_000,
    });
    await expect(building.getByTestId("brief-summary")).toHaveText(
      "A person approves each generated summary in Studio before it is saved.",
    );
    await expect(building.getByRole("button", { name: /^(Confirm and build|Change something…)$/ })).toHaveCount(0);
    await building.locator('[data-more-details="brief.more"] > button').click();
    await building.locator('[data-more-details="brief.code"] > button').click();
    await expect(building.getByRole("region", { name: "Outline", exact: true })).toContainText(
      "TextSummaryFlow: PrepareSummary, GenerateSummary, AwaitSummaryApproval [Approve summary], SaveSummaryResult, RecordSummaryFailure; +1 failure-handling Step",
    );
    await expect(building.getByRole("list", { name: "What the design changes" })).toHaveText(
      "Text summary · changed: adds Await summary approval, Approve summary; changes Generate summary",
    );
    return null;
  } catch (failure) {
    return failure;
  }
}

/**
 * A change after the first build, on the mock model: input 2 of summary-approval revises the card to version 2,
 * which carries no Flow design, and the push of version 1 built its design, so version 2 does not carry that one
 * forward either; the change rewords AwaitSummaryApproval's explanation and pushes. With automatic builds off the
 * push stays unbuilt, so the Flow tab keeps the last built TextSummaryFlow, read-only under the Build processes
 * strip, with no design, design phase, Brief or Design marker. Build processes on that strip builds the push, and
 * the Step whose definition changed, or its closed Result stage, is marked changed for a moment while the flow
 * stays. This page read the earlier build, which the mark compares with.
 */
async function changeKeepsRealFlow(page: Page, projectId: string) {
  const noDesign = async () => {
    await expect(page.locator("[data-design-phase]")).toHaveCount(0);
    await expect(page.getByLabel("Flow design", { exact: true })).toHaveCount(0);
    await expect(page.locator("[data-brief-state]")).toHaveCount(0);
    await expect(page.getByTestId("design-status")).toHaveCount(0);
    await expect(page.getByTestId("design-marker")).toHaveCount(0);
  };
  await setAutomaticBuilds(page, projectId, false);
  await page.goto(`/p/${projectId}/flow?flowType=TextSummaryFlow`);
  const flow = page.getByRole("region", { name: "Flow definition" });
  const result = flow.getByRole("button", { name: /^Result, 3 steps\. Open the stage\.$/ });
  await expect(result).toBeVisible({ timeout: 5 * 60_000 });
  await sendMockMessage(page, projectId, "summary-approval", 2, 25 * 60_000);
  const changed = await readSession(page, projectId);
  const current = changed.agent?.description?.confirmedRequirements;
  expect(Number(current?.revision ?? 0)).toBe(2);
  expect(current?.spec?.features.map(({ id }) => id)).toEqual(["R1", "R2", "R3"]);
  expect(current?.spec?.design ?? null, "version 2 carries no design, and version 1's built one is dropped").toBeNull();
  await expectPushedHead(await readProject(page, projectId), changed);
  expect(changed.preview?.preparedSource?.sourceCommit, "automatic builds are off").not.toBe(changed.headCommit);
  await expect(result).toBeVisible();
  await noDesign();
  const changedStep = flow.getByRole("button", {
    name: /^Await summary approval\b.*, changed$|^Result, 3 steps, changed\. Open the stage\.$/,
  });
  // The mark shows for a few seconds from the first drawing of the new build, so the wait starts first.
  const marked = expect(changedStep).toBeVisible({ timeout: 15 * 60_000 });
  await page
    .getByRole("region", { name: "Build processes", exact: true })
    .getByRole("button", { name: "Build processes", exact: true })
    .click();
  await marked;
  await noDesign();
  await expect(changedStep).toHaveCount(0, { timeout: 30_000 });
  await expect(result).toBeVisible();
  const built = await readSession(page, projectId);
  expect(built.preview?.preparedSource?.sourceCommit).toBe(changed.headCommit);
}

test("template project Agent adds a summary approval that Preview and an operator on Live start, approve and complete", async ({
  page,
  browser,
}, testInfo) => {
  // One implementation turn, a Preview and a Live build and deployment, and on the
  // mock model a change turn with its build, fit the runner's 60-minute bound for
  // this case.
  test.setTimeout(55 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  // The creation page takes the first message of the Project's Session: the prompt, or on the mock
  // model input 1 of the scenario, as SUPERVERSE_E2E_MODEL_TARGET or the lane decides; CreateProject
  // carries it, and the Session starts with it.
  const mock = await choosesMockModel(page);
  let overview: Promise<unknown> = Promise.resolve(null);
  const created = await createProject(
    page,
    teamSpaceId,
    mock ? chooseMockFirstInput("summary-approval") : typePrompt(APPROVAL_PROMPT),
    testInfo,
    mock
      ? async () => {
          overview = overviewShown(page);
        }
      : undefined,
  );
  // The agent records its requirements card first and goes on building in the same turn, so the workspace
  // changes from the start: Preview's processes build from a clean pushed head, and the person configures
  // Preview once the push's automatic deployment stops at it with the configuration notice.
  await waitForRequirementsCard(page, created.projectId, 10 * 60_000);
  const overviewFailure = await overview;
  if (overviewFailure) throw overviewFailure;
  // The chat shows the recorded version once, where the agent recorded it, as one line that opens to the card.
  // On the Flow tab the diagram shows its marks against the template's process, fitted whole: the Result stage
  // that holds the approval Step is changed and the Step new, GenerateSummary changed, and nothing starts a run.
  if (mock) {
    await page.goto(`/p/${created.projectId}/flow?flowType=TextSummaryFlow`);
    const recorded = page.locator('[data-recorded-version="1"]');
    await expect(recorded).toContainText("Recorded version 1: Summary approval");
    await recorded.locator("summary").click();
    await expect(recorded.getByRole("region", { name: "Features", exact: true })).toContainText("R2");
    const design = page.getByRole("region", { name: "Flow design" });
    await design.getByRole("button", { name: "Fit", exact: true }).click();
    await design.getByRole("button", { name: /^Result, 3 steps, changed\. Open the stage\.$/ }).click();
    await expect(design.getByRole("button", { name: /^Await summary approval\b.*, new$/ })).toBeVisible();
    await expect(design.getByRole("button", { name: /^Generate summary\b.*, changed$/ })).toBeVisible();
    await expect(design.getByRole("button", { name: /^(Test run|Live run|Start)/ })).toHaveCount(0);
    // A designed Step is dashed until its code is written. The template holds the five it keeps or changes,
    // and the scripted edit declares AwaitSummaryApproval, which then turns solid: all six read written, unless
    // the push built the design first, which takes the overview away.
    const allWritten = page
      .locator('[data-brief-state="building"]')
      .getByTestId("design-status")
      .filter({ hasText: /^Building · 6 of 6 steps written$/ });
    await expect(allWritten.or(page.locator('[data-design-phase="built"]'))).toBeVisible({ timeout: 10 * 60_000 });
    await expect(design.getByRole("button", { name: /^Await summary approval\b.*, new$/ })).not.toHaveAttribute(
      "data-emphasis",
      "planned",
    );
  }
  if (mock) await waitForFirstMockInput(page, created.projectId, "summary-approval", 25 * 60_000);
  else await waitForCompletedTurn(page, created.projectId, -1, 25 * 60_000);
  const session = await readSession(page, created.projectId);
  await expectPushedHead(await readProject(page, created.projectId), session);
  // The scripted push, without blocked, completes the build of the recorded design. The overview goes with it:
  // the Flow tab shows only the flow, the design until the Preview's processes are built from that push.
  if (mock) {
    expect(
      session.agent?.description?.confirmedRequirements?.spec?.designBuilt,
      "the push built the recorded design",
    ).toBe(true);
    await page.goto(`/p/${created.projectId}/flow?flowType=TextSummaryFlow`);
    await expect(
      page.locator('[data-design-phase="built"]').or(page.getByRole("region", { name: "Flow definition" })),
    ).toBeVisible({ timeout: 5 * 60_000 });
    await expect(page.locator("[data-brief-state]")).toHaveCount(0);
    await expect(page.getByTestId("design-status")).toHaveCount(0);
  }

  await configureAndStartPreview(page, created.projectId, session.headCommit);
  // The processes built from that push replace the design: the implemented Flow draws without its label.
  if (mock) {
    await page.goto(`/p/${created.projectId}/flow?flowType=TextSummaryFlow`);
    const implemented = page.getByRole("region", { name: "Flow definition" });
    await expect(implemented.getByRole("button", { name: /^Result, 3 steps\. Open the stage\.$/ })).toBeVisible({
      timeout: 5 * 60_000,
    });
    await expect(page.getByRole("region", { name: "Flow design" })).toHaveCount(0);
    await expect(page.getByRole("status", { name: "Flow design" })).toHaveCount(0);
    await expect(page.locator("[data-brief-state]")).toHaveCount(0);
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
  if (mock) await changeKeepsRealFlow(page, created.projectId);
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
