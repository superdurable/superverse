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
import { fillTemplateSummaryConnection } from "@/tests/e2e/support/configuration";
import { choosesMockModel, runsMockModel, sendMockMessage } from "@/tests/e2e/support/mock/mock-composer";
import { observedTarget } from "@/tests/e2e/support/flow-run";
import { deleteProject, MOCK_MODEL_OPTION, readProject, readSession } from "@/tests/e2e/support/project";
import { configureAndStartPreview } from "@/tests/e2e/support/preview/preview";
import { publishLive } from "@/tests/e2e/support/publishing";
import { chooseOption, rpcAnswered, rpcResult, studioOrigin } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";
import type { FlowSearchResult } from "@/lib/api/flow/flow-types";
import type { RpcOutput } from "@/lib/shared/rpc-methods";

const TITLE = "Skip texts too short to summarize";
/** Live runs on both sides of the 20 bytes the scenario's change refuses; the Live build accepts each one. */
const TEXTS = [
  "Too short.",
  "Hi there.",
  "Ok, noted.",
  "Dex keeps every Flow's state durable.",
  "The connector shipped on time, and the release notes went out the same afternoon.",
  "A summary waits for a person to approve it in Studio before it is saved.",
  "Goal measurement reads the runs of the Live build and evaluates its goals.",
  "Short texts reach the model like any other request today.",
];

// An agent review that offers a proposal, approved by a person. Review now is admitted once a day per Project, so
// each case has its own Project: the summary-approval example opened from the start page on the mock model, whose
// evolve-review scenario pushes the goals, Preview configured with the lane's synthetic summary model, the goals'
// commit published to Live and Live runs started from Runs. Review now on the Goals tab measures them; the
// review's notification opens the agent's own turn, whose investigate and change subagents run the coding image's
// superverse-evolve over the placed Live export, and the agent offers the change. The Goals tab marks the waiting
// proposal. Approve, on the Goals tab or on the chat's card, records the decision; the agent merges the change and
// pushes it, the Preview serves it and the proposal reads On Preview with Publish…. The server, Dex, the Sandbox
// and the Git host are the lane's own.
test.describe.configure({ mode: "parallel" });

test("an offered proposal is approved from the Goals tab", async ({ page }) => {
  test.setTimeout(150 * 60_000);
  const projectId = await offeredProposal(page);
  await page.goto(`/p/${projectId}/goals?v=live`);
  const row = page.getByTestId("goal-proposal").filter({ hasText: TITLE });
  await expect(row).toHaveAttribute("data-status", "EVOLVE_PROPOSAL_STATUS_OFFERED");
  const document = page.waitForResponse(rpcAnswered("GetEvolveProposal"));
  await row.getByText(TITLE, { exact: true }).click();
  expect((await document).ok()).toBe(true);
  const detail = row.getByTestId("proposal-detail");
  await expect(detail.getByTestId("proposal-backtest")).toBeVisible();
  await expect(detail).toContainText("AwaitSummaryApproval");
  const decided = page.waitForResponse(rpcAnswered("DecideEvolveProposal"));
  await detail.getByTestId("proposal-approval").getByRole("button", { name: "Approve", exact: true }).click();
  expect((await decided).ok()).toBe(true);
  await expectAppliedOnPreview(page, projectId);
  await expect(row).toHaveAttribute("data-status", "EVOLVE_PROPOSAL_STATUS_APPLIED", { timeout: 60_000 });
  await expect(row.getByTestId("proposal-publish")).toBeVisible();
  await deleteProject(page, projectId);
});

test("an offered proposal is approved from the chat card", async ({ page }) => {
  test.setTimeout(150 * 60_000);
  const projectId = await offeredProposal(page);
  await page.goto(`/p/${projectId}/flow`);
  const card = page.getByTestId("proposal-dock").getByRole("region", { name: "Proposal", exact: true });
  await expect(card).toHaveAttribute("data-status", "EVOLVE_PROPOSAL_STATUS_OFFERED", { timeout: 60_000 });
  await expect(card).toContainText(TITLE);
  const decided = page.waitForResponse(rpcAnswered("DecideEvolveProposal"));
  await card.getByRole("button", { name: "Approve", exact: true }).click();
  expect((await decided).ok()).toBe(true);
  await expectAppliedOnPreview(page, projectId);
  // The Goals tab shows the same decision, and no proposal waits there any more.
  await page.goto(`/p/${projectId}/goals?v=live`);
  await expect(page.getByTestId("goal-proposal").filter({ hasText: TITLE })).toHaveAttribute(
    "data-status",
    "EVOLVE_PROPOSAL_STATUS_APPLIED",
  );
  await expect(goalsTab(page, false)).toBeVisible();
  await expect(goalsTab(page, false).getByTestId("needs-you-mark")).toHaveCount(0);
  await deleteProject(page, projectId);
});

/** The Goals tab's link in the surface navigation, whose name says when a proposal waits for the person. */
function goalsTab(page: Page, proposalWaits: boolean) {
  return page
    .getByRole("navigation", { name: "Surface", exact: true })
    .getByRole("link", { name: proposalWaits ? "Goals, a proposal waits for you" : "Goals", exact: true });
}

/**
 * A new account's Project from the summary-approval example on the mock model, its goals pushed and published,
 * Live runs started, and Review now pressed on the Goals tab; returns once the agent's proposal is offered and the
 * tab marks it.
 */
async function offeredProposal(page: Page): Promise<string> {
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  if (!(await choosesMockModel(page))) throw new Error("The proposal cases run on the mock model");
  const projectId = await openSummaryApproval(page, teamSpaceId);
  // The example's Session opens on the mock model; until it has, SendMessage asks for a key.
  await expect.poll(() => runsMockModel(page, projectId), { timeout: 60_000 }).toBe(true);
  await sendMockMessage(page, projectId, "evolve-review", 1, 25 * 60_000);
  const { headCommit } = await readSession(page, projectId);
  // The example's own first commit may already have been refused for the same settings, which the Preview tells once.
  await configureAndStartPreview(page, projectId, headCommit, true);
  const live = await publishLive(page, projectId, fillTemplateSummaryConnection);
  expect(live.release.sourceCommit).toBe(headCommit);
  await expect
    .poll(async () => goalsOf(await rpcResult(page.request, "GetEvolve", { projectId })), {
      timeout: 10 * 60_000,
      intervals: [2000],
    })
    .toEqual({ commit: headCommit, status: "EVOLVE_GOALS_STATUS_VALID" });
  await startLiveRuns(page, projectId);
  await expectRunsAwaitApproval(page, projectId, live.release.releaseId);

  await page.goto(`/p/${projectId}/goals?v=live`);
  await expect(page.getByTestId("goals-surface")).toBeVisible({ timeout: 60_000 });
  const started = page.waitForResponse(rpcAnswered("StartEvolveReview"));
  await page.getByRole("button", { name: "Review now", exact: true }).click();
  const response = await started;
  expect(response.ok()).toBe(true);
  const { reviewId }: RpcOutput<"StartEvolveReview"> = await response.json();
  expect(reviewId).toMatch(/^r-[1-9][0-9]*$/);
  await expect
    .poll(
      async () =>
        (await rpcResult(page.request, "GetEvolve", { projectId })).evolve?.proposals.find(
          (proposal) => proposal.reviewId === reviewId,
        )?.status,
      { timeout: 90 * 60_000, intervals: [5000] },
    )
    .toBe("EVOLVE_PROPOSAL_STATUS_OFFERED");
  await page.reload();
  await expect(goalsTab(page, true).getByTestId("needs-you-mark")).toBeVisible({ timeout: 60_000 });
  return projectId;
}

/** Opens the summary-approval example from the Teamspace's start page and returns the READY Project. */
async function openSummaryApproval(page: Page, teamSpaceId: string): Promise<string> {
  await page.goto(`/t/${teamSpaceId}/projects/start`);
  const example = page.getByRole("article", { name: "Summary approval", exact: true });
  await expect(example).toBeVisible({ timeout: 60_000 });
  // An ordinary account starts on Sonnet, for which the lane has no key; the example opens on the mock model.
  await chooseOption(page.getByRole("button", { name: "Model", exact: true }), MOCK_MODEL_OPTION);
  await expect(example).toBeVisible();
  const answered = page.waitForResponse(
    (response) => rpcAnswered("CreateProject")(response) && response.status() !== 503,
  );
  await example.getByRole("button", { name: "Open example", exact: true }).click();
  const response = await answered;
  expect(response.ok()).toBe(true);
  const { projectId }: RpcOutput<"CreateProject"> = await response.json();
  await expect
    .poll(async () => (await readProject(page, projectId)).status, { timeout: 10 * 60_000, intervals: [2000] })
    .toBe("PROJECT_STATUS_READY");
  expect((await readProject(page, projectId)).example?.exampleId).toBe("summary-approval");
  return projectId;
}

/** Starts one Live run of TextSummaryFlow per text from Runs, as a person does with Live run. */
async function startLiveRuns(page: Page, projectId: string) {
  const runs = `/p/${projectId}/runs?${new URLSearchParams({ v: "live", flowType: "TextSummaryFlow" })}`;
  for (const text of TEXTS) {
    await page.goto(runs);
    await page.getByRole("button", { name: "Live run", exact: true }).click();
    const dialog = page.getByRole("dialog");
    await dialog.locator('[data-start-path="$.text"]').fill(text);
    const accepted = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname.endsWith("/api/v2/start") && response.request().method() === "POST",
    );
    await dialog.getByRole("button", { name: "Start live run", exact: true }).click();
    expect((await accepted).ok()).toBe(true);
  }
}

/**
 * Waits until every Live run waits for a person's approval (summary-status awaiting-approval, AwaitSummaryApproval),
 * so that the review exports settled runs: a run exported before GenerateSummary or before PrepareSummary recorded
 * its text size would break the backtest's calibration or leave it short of runs.
 */
async function expectRunsAwaitApproval(page: Page, projectId: string, releaseId: string) {
  const { prefix, headers } = await observedTarget(page, projectId, "live", releaseId);
  const catalog = await page.request.get(prefix + "/api/v2/catalog", { headers });
  expect(catalog.ok()).toBe(true);
  const { definitionRevision }: { definitionRevision: string } = await catalog.json();
  // Search is a POST, which the Flow API admits only from Studio's origin with the CSRF token its catalog issued.
  const csrf = catalog.headers()["x-superverse-csrf-token"];
  if (!csrf) throw new Error("Live Runs catalog did not issue a CSRF token");
  await expect
    .poll(
      async () => {
        const searched = await page.request.post(prefix + "/api/v2/search", {
          headers: {
            ...headers,
            origin: studioOrigin(),
            "X-Dex-CSRF-Token": csrf,
            "X-Dex-Flow-Definition-Revision": definitionRevision,
          },
          data: { flowType: "TextSummaryFlow", workQueueOnly: false, filters: [], pageSize: 50 },
        });
        if (!searched.ok()) return -1;
        const { flows }: FlowSearchResult = await searched.json();
        return flows.filter((run) => run.indexedAttributes["summary-status"] === "awaiting-approval").length;
      },
      { timeout: 10 * 60_000, intervals: [2000] },
    )
    .toBe(TEXTS.length);
}

/** The goals commit and status of a GetEvolve answer. */
function goalsOf(answer: RpcOutput<"GetEvolve">) {
  return { commit: answer.evolve?.goals?.commit, status: answer.evolve?.goals?.status };
}

/**
 * Waits until the agent applied the approved proposal: it pushed the change, which the Preview serves, and
 * reported the commit.
 */
async function expectAppliedOnPreview(page: Page, projectId: string) {
  let applied = "";
  await expect
    .poll(
      async () => {
        const proposal = (await rpcResult(page.request, "GetEvolve", { projectId })).evolve?.proposals.find(
          ({ title }) => title === TITLE,
        );
        applied = proposal?.appliedCommit ?? "";
        return proposal?.status;
      },
      { timeout: 45 * 60_000, intervals: [5000] },
    )
    .toBe("EVOLVE_PROPOSAL_STATUS_APPLIED");
  await expect
    .poll(
      async () => {
        const preview = (await readSession(page, projectId)).preview;
        return preview?.status === "PREVIEW_STATUS_READY" && preview.sourceCommit === applied;
      },
      { timeout: 15 * 60_000, intervals: [2000] },
    )
    .toBe(true);
}
