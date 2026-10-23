// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type BrowserContext, type Page, type Route } from "@playwright/test";
import type { AgentNotification__Output } from "@/lib/generated/protobuf/superverse/v2/AgentNotification";
import type { RpcOutput } from "@/lib/shared/rpc-methods";
import { configure, fillTemplateSummaryConnection } from "@/tests/e2e/support/configuration";
import { exerciseTextSummaryInApplication, exerciseTextSummaryInStudio } from "@/tests/e2e/support/flow-run";
import { readSession } from "@/tests/e2e/support/project";
import { expectStartPhases } from "./start-phases";
import { rpcAnswered, rpcResult, UUID } from "@/tests/e2e/support/studio-api";

// The Project's Preview: starting it from the Interface, the automatic deployment of a push and its
// configuration notice, and the template edit seen on Preview.

async function startPreview(page: Page, projectId: string): Promise<string> {
  await page.goto(`/p/${projectId}/interface`);
  const start = page.getByRole("button", { name: "Start Preview", exact: true });
  await expect(start).toBeEnabled({ timeout: 30_000 });
  // Observe the prepared instance before dispatch. A BFF read after the click
  // must never silently select a newer instance on behalf of this operation.
  const observed = (await readSession(page, projectId)).preview;
  if (!observed?.workspaceRequestId) throw new Error("Start Preview requires an observed prepared workspace");
  // Start also takes Setup's saved settings that the Preview does not run yet, as Setup applies them on its own.
  const { configuration } = await rpcResult(page.request, "GetProjectConfiguration", {
    target: { projectId, scope: "PROJECT_SCOPE_PREVIEW" },
  });
  const applying = (configuration?.unappliedChangeCount ?? 0) > 0;
  const [response] = await Promise.all([
    page.waitForResponse(rpcAnswered("StartPreview"), { timeout: 30_000 }),
    start.click({ timeout: 30_000 }),
  ]);
  // The page sends its request ID and the instance it observed; the BFF fills in neither.
  expect(response.request().postDataJSON()).toEqual({
    projectId,
    requestId: expect.stringMatching(UUID),
    expectedRevision: observed.revision,
    generation: observed.generation,
    workspaceRequestId: observed.workspaceRequestId,
    ...(applying ? { applyConfiguration: true, configurationRevision: expect.stringMatching(/^[1-9][0-9]*$/) } : {}),
  });
  expect(response.ok()).toBe(true);
  const { preview: accepted }: RpcOutput<"StartPreview"> = await response.json();
  if (!accepted) throw new Error("StartPreview returned no Preview");
  expect(accepted.projectId).toBe(projectId);
  expect(accepted.workspaceRequestId).toBe(observed.workspaceRequestId);
  expect(accepted.generation).toMatch(/^[1-9][0-9]*$/);
  expect(accepted.revision).toMatch(/^[1-9][0-9]*$/);
  let url = "";
  await expect
    .poll(
      async () => {
        const current = (await readSession(page, projectId)).preview;
        if (!current) throw new Error("Accepted Preview has no authoritative snapshot");
        expect(current.projectId).toBe(projectId);
        expect(current.workspaceRequestId).toBe(accepted.workspaceRequestId);
        expect(current.generation).toBe(accepted.generation);
        expect(BigInt(current.revision) >= BigInt(accepted.revision)).toBe(true);
        if (current.failure) throw new Error(`Preview failed at ${current.failure.phase}: ${current.failure.code}`);
        if (current.status !== "PREVIEW_STATUS_STARTING" && current.status !== "PREVIEW_STATUS_READY")
          throw new Error(`Preview reached unexpected status ${current.status}`);
        if (current.status !== "PREVIEW_STATUS_READY") return false;
        expect(current.url).toMatch(/^https?:\/\//);
        url = current.url;
        return true;
      },
      { timeout: 5 * 60_000, intervals: [1000, 2000] },
    )
    .toBe(true);
  await expectPreviewLink(page, url);
  await expectStartPhases(page, projectId);
  return url;
}

/** Opens the Interface's Details under the page: the deployed commit, the start phases and the history. */
async function openInterfaceDetails(page: Page) {
  const details = page
    .getByRole("region", { name: "Project Preview", exact: true })
    .getByText("Details", { exact: true });
  // The panel remounts once the Session's read settles, which folds Details again: open it until it stays.
  const commit = page.getByText(/^Deployed commit: /);
  await expect(async () => {
    if (!(await commit.isVisible())) await details.click();
    await expect(commit).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 60_000 });
}

/** Requires the Interface's link to the READY Preview at url, which it shows once the browser reaches the URL. */
async function expectPreviewLink(page: Page, url: string) {
  const link = page.getByRole("link", { name: "Open Preview", exact: true });
  await expect(link).toBeVisible({ timeout: 30_000 });
  await expect(link).toHaveAttribute("href", url);
}

/**
 * Waits until the automatic deployment of the Session's pushed head serves it: a configured Preview deploys a push
 * by itself, so the Interface shows it READY at that commit with no Start Preview. Returns the Preview URL.
 */
async function awaitDeployedPreview(page: Page, projectId: string, headCommit: string) {
  let url = "";
  await expect
    .poll(
      async () => {
        const current = (await readSession(page, projectId)).preview;
        if (current?.failure)
          throw new Error(
            `The automatic Preview deployment failed at ${current.failure.phase}: ${current.failure.code}`,
          );
        if (current?.status !== "PREVIEW_STATUS_READY" || current.sourceCommit !== headCommit) return false;
        url = current.url;
        return true;
      },
      { timeout: 10 * 60_000, intervals: [1000, 2000] },
    )
    .toBe(true);
  expect(url).toMatch(/^https?:\/\//);
  await page.goto(`/p/${projectId}/interface`);
  await openInterfaceDetails(page);
  await expect(page.getByText(`Deployed commit: ${headCommit}`, { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("button", { name: "Start Preview", exact: true })).toHaveCount(0);
  await expectPreviewLink(page, url);
  return url;
}

/** Whether notification is the configuration notice of an automatic Preview deployment that names gap. */
function isConfigurationNotice(notification: AgentNotification__Output | null | undefined, gap: string) {
  return (
    notification?.kind === "KIND_DELIVERY" &&
    notification.source === "SOURCE_PREVIEW" &&
    /^preview\/[^/]+\/deploy-preview-[1-9][0-9]*\/notice$/.test(notification.id) &&
    notification.summary.startsWith("The Preview needs configuration: ") &&
    notification.summary.includes(gap)
  );
}

/**
 * Waits until the automatic deployment of the Session's pushed head ends at the configuration its Preview lacks: the
 * conversation shows the configuration notice naming gap, and the Preview, which released what the deployment took,
 * is STOPPED with the refusal, so a person configures and starts it. The Preview tells the same gaps only once in a
 * row, so when an earlier commit of the Session was refused for them first, the notice names that commit:
 * earlierNotice accepts it, and the Preview's automatic deployment shows the head refused.
 */
export async function awaitConfigurationNotice(
  page: Page,
  projectId: string,
  headCommit: string,
  gap: string,
  earlierNotice = false,
) {
  let noticedCommit = headCommit;
  await expect
    .poll(
      async () => {
        const { preview, agent } = await readSession(page, projectId);
        const failure = preview?.failure;
        if (preview?.status === "PREVIEW_STATUS_READY") throw new Error("The unconfigured Preview was deployed");
        if (failure && (failure.code !== "PREVIEW_CONFIGURATION_INCOMPATIBLE" || failure.phase !== "deploy-preview"))
          throw new Error(`The automatic Preview deployment ended at ${failure.phase}: ${failure.code}`);
        const notice = (agent?.history?.messages ?? [])
          .map(({ message }) => message?.notification)
          .find(
            (notification) =>
              isConfigurationNotice(notification, gap) && (earlierNotice || notification?.commit === headCommit),
          );
        const deployment = preview?.automaticDeployment;
        const headRefused =
          !earlierNotice ||
          (deployment?.commit === headCommit &&
            deployment.status === "PREVIEW_DEPLOYMENT_STATUS_INCOMPATIBLE" &&
            !deployment.queuedCommit);
        if (!notice || !headRefused) return false;
        noticedCommit = notice.commit;
        // Refused before it started anything, the deployment leaves the Preview as it was.
        return (
          Boolean(failure) &&
          (preview?.status === "PREVIEW_STATUS_NOT_STARTED" || preview?.status === "PREVIEW_STATUS_STOPPED")
        );
      },
      { timeout: 10 * 60_000, intervals: [1000, 2000] },
    )
    .toBe(true);
  await page.goto(`/p/${projectId}/flow`);
  // The notice says what the Preview still needs and offers Setup; the commit is only in its More details,
  // which one notice opens for all of them.
  const notices = page
    .getByTestId("notification-notice")
    .filter({ has: page.getByTestId("problem-what").filter({ hasText: "Preview needs a few settings" }) });
  const details = notices.first().locator('[data-more-details="chat.notice"] > button');
  await expect(details).toBeVisible({ timeout: 30_000 });
  if ((await details.getAttribute("aria-expanded")) !== "true") await details.click();
  const notice = notices.filter({ has: page.getByTestId("problem-details").filter({ hasText: noticedCommit }) });
  await expect(notice).toHaveAttribute("data-notification-kind", "delivery", { timeout: 30_000 });
  await expect(notice).toContainText(gap);
  await expect(notice.getByTestId("problem-fix").getByRole("link", { name: "Open Setup" })).toBeVisible();
}

/**
 * The push's automatic deployment stopped at the Session's unconfigured Preview with the configuration notice:
 * the person configures Preview and starts it, which then serves headCommit. earlierNotice is awaitConfigurationNotice's.
 */
export async function configureAndStartPreview(
  page: Page,
  projectId: string,
  headCommit: string,
  earlierNotice = false,
) {
  await awaitConfigurationNotice(page, projectId, headCommit, "summary-model", earlierNotice);
  await page.goto(`/p/${projectId}/tools?tool=publishing`);
  await configure(page, "Preview", fillTemplateSummaryConnection);
  await startPreview(page, projectId);
  await awaitDeployedPreview(page, projectId, headCommit);
}

/** The prompt that asks the Agent to change the template's main heading to marker and push the change. */
export function headingChangePrompt(marker: string): string {
  return `Change the app main heading to exactly "${marker}". Preserve TextSummaryFlow, its summary form and its API. Run the project source checks, commit and push this UI change to the default branch. Do not add connectors.`;
}

/**
 * Previews a template Project whose Session changed the main heading to marker
 * and pushed it: the push's automatic deployment stops at the Session's
 * unconfigured Preview with the configuration notice, so the person configures
 * Preview and starts it, then the heading and the summary Flow are checked in
 * the application and in Studio Runs. afterPreviewStarted runs while the build
 * page still shows the started Preview.
 */
export async function previewTemplateEdit(
  page: Page,
  context: BrowserContext,
  projectId: string,
  marker: string,
  afterPreviewStarted?: () => Promise<void>,
) {
  const { headCommit } = await readSession(page, projectId);
  await awaitConfigurationNotice(page, projectId, headCommit, "summary-model");
  await page.goto(`/p/${projectId}/tools?tool=publishing`);
  await configure(page, "Preview", fillTemplateSummaryConnection);
  const previewURL = await startPreview(page, projectId);
  await afterPreviewStarted?.();
  const app = await context.newPage();
  await app.goto(previewURL);
  await expect(app.getByRole("heading", { name: marker, exact: true })).toBeVisible();
  await exerciseTextSummaryInApplication(app);
  await exerciseTextSummaryInStudio(page, projectId, "preview");
  return { app, previewURL };
}

/**
 * Switches the Builder's bar from Preview to Live and back, and requires every surface to follow:
 * the Flow says Live is read only, Runs lists Live runs and reads Live's events, the Interface
 * shows the Live address, and the chat says changes are built on Preview; back on Preview the
 * Interface frames the Preview.
 */
export async function expectSurfacesFollowVersion(page: Page, projectId: string, liveURL: string) {
  // Hold real Preview definitions until Live is clicked, and keep Live's original navigation
  // pending until Preview's default selection has run. No dependency response is substituted.
  let releaseDefinitions!: () => void;
  const definitionsGate = new Promise<void>((resolve) => (releaseDefinitions = resolve));
  let releaseLive!: () => void;
  const liveGate = new Promise<void>((resolve) => (releaseLive = resolve));
  let definitionsSeen = false;
  let liveSeen = false;
  const definitionsPath = `/dex/projects/${projectId}/source/api/flow-definitions`;
  const previewDefinitions = (url: URL) => url.pathname === definitionsPath;
  const delayDefinitions = async (route: Route) => {
    definitionsSeen = true;
    await definitionsGate;
    await route.continue();
  };
  const pendingLive = (url: URL) =>
    url.pathname === `/p/${projectId}/flow` &&
    url.searchParams.get("v") === "live" &&
    !url.searchParams.has("flowType") &&
    url.searchParams.has("_rsc");
  const delayLive = async (route: Route) => {
    liveSeen = true;
    await liveGate;
    await route.continue();
  };
  await page.route(previewDefinitions, delayDefinitions);
  await page.route(pendingLive, delayLive);
  const live = page.getByTestId("environment-live");
  try {
    await page.goto(`/p/${projectId}/flow`);
    await expect.poll(() => definitionsSeen).toBe(true);
    expect(new URL(page.url()).searchParams.has("flowType")).toBe(false);
    await live.click();
    await expect.poll(() => liveSeen).toBe(true);
    releaseDefinitions();
    await expect(page).toHaveURL(
      (url) => url.searchParams.get("v") === "live" && url.searchParams.get("flowType") === "TextSummaryFlow",
    );
  } finally {
    releaseDefinitions();
    releaseLive();
    await page.unroute(previewDefinitions, delayDefinitions);
    await page.unroute(pendingLive, delayLive);
  }
  // The automatic replacement completes the pending click as one history entry.
  await page.goBack();
  await expect(page).toHaveURL(
    (url) =>
      url.pathname === `/p/${projectId}/flow` &&
      !url.searchParams.has("v") &&
      url.searchParams.get("flowType") === "TextSummaryFlow",
  );
  await page.goForward();
  await expect(page).toHaveURL(
    (url) => url.searchParams.get("v") === "live" && url.searchParams.get("flowType") === "TextSummaryFlow",
  );
  await expect(live).toHaveAttribute("aria-current", "page");
  await expect(page.getByText(/read only here/).first()).toBeVisible({ timeout: 60_000 });
  await expect(page.getByTestId("live-note")).toBeVisible();
  const surfaces = page.getByRole("navigation", { name: "Surface" });
  // Runs reads what Live's triggers received, for Live: the bar's Live has no Setup at hand to say
  // whether a process relays a trigger, so the run list names events once some arrive.
  const liveEvents = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/rpc/ListRecentTriggerEvents" &&
      response.request().postDataJSON()?.scope === "PROJECT_SCOPE_LIVE",
    { timeout: 60_000 },
  );
  await surfaces.getByRole("link", { name: "Runs", exact: true }).click({ timeout: 30_000 });
  await expect(page).toHaveURL(new RegExp(`/p/${projectId}/runs\\?.*v=live`));
  await expect(page.getByText("Runs · Live", { exact: true })).toBeVisible({ timeout: 60_000 });
  // The template relays no trigger, so nothing reached Live and the list names no events.
  const events = await liveEvents;
  expect(events.ok()).toBe(true);
  expect((await events.json()).events ?? []).toEqual([]);
  await expect(page.getByRole("button", { name: /^Events received/ })).toHaveCount(0);
  await surfaces.getByRole("link", { name: "Interface", exact: true }).click({ timeout: 30_000 });
  // The Interface shows Live's page under its address bar, which opens and copies the address.
  await expect(page.getByText(liveURL, { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("link", { name: "Open Live", exact: true })).toHaveAttribute("href", liveURL);
  await expect(page.getByTitle("Live application", { exact: true })).toHaveAttribute("src", liveURL);
  await page.getByTestId("environment-preview").click();
  await expect(page).not.toHaveURL(/[?&]v=live/);
  await expect(page.getByRole("region", { name: "Project Preview", exact: true })).toBeVisible();
  await expect(page.getByTestId("live-note")).toHaveCount(0);
}
