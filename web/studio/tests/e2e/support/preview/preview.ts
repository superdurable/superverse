// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type BrowserContext, type Page } from "@playwright/test";
import type { AgentNotification__Output } from "@/lib/generated/protobuf/superverse/v2/AgentNotification";
import type { RpcOutput } from "@/lib/shared/rpc-methods";
import { configure, fillTemplateSummaryConnection } from "@/tests/e2e/support/configuration";
import { exerciseTextSummaryInApplication, exerciseTextSummaryInStudio } from "@/tests/e2e/support/flow-run";
import { expectStartPhases, readSession } from "@/tests/e2e/support/project";
import { rpcAnswered, UUID } from "@/tests/e2e/support/studio-api";

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
export async function awaitDeployedPreview(page: Page, projectId: string, headCommit: string) {
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
  await expect(page.getByText(`Deployed commit: ${headCommit}`, { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("button", { name: "Start Preview", exact: true })).toHaveCount(0);
  await expectPreviewLink(page, url);
  return url;
}

/** Whether notification is the configuration notice of an automatic Preview deployment of commit that names gap. */
function isConfigurationNotice(
  notification: AgentNotification__Output | null | undefined,
  commit: string,
  gap: string,
) {
  return (
    notification?.kind === "KIND_DELIVERY" &&
    notification.source === "SOURCE_PREVIEW" &&
    /^preview\/[^/]+\/deploy-preview-[1-9][0-9]*\/notice$/.test(notification.id) &&
    notification.commit === commit &&
    notification.summary.startsWith("The Preview needs configuration: ") &&
    notification.summary.includes(gap)
  );
}

/**
 * Waits until the automatic deployment of the Session's pushed head ends at the configuration its Preview lacks: the
 * conversation shows the configuration notice naming gap, and the Preview, which released what the deployment took,
 * is STOPPED with the refusal, so a person configures and starts it.
 */
export async function awaitConfigurationNotice(page: Page, projectId: string, headCommit: string, gap: string) {
  await expect
    .poll(
      async () => {
        const { preview, agent } = await readSession(page, projectId);
        const failure = preview?.failure;
        if (preview?.status === "PREVIEW_STATUS_READY") throw new Error("The unconfigured Preview was deployed");
        if (failure && (failure.code !== "PREVIEW_CONFIGURATION_INCOMPATIBLE" || failure.phase !== "deploy-preview"))
          throw new Error(`The automatic Preview deployment ended at ${failure.phase}: ${failure.code}`);
        const noticed = (agent?.history?.messages ?? []).some(({ message }) =>
          isConfigurationNotice(message?.notification, headCommit, gap),
        );
        // Refused before it started anything, the deployment leaves the Preview as it was.
        return (
          noticed &&
          Boolean(failure) &&
          (preview?.status === "PREVIEW_STATUS_NOT_STARTED" || preview?.status === "PREVIEW_STATUS_STOPPED")
        );
      },
      { timeout: 10 * 60_000, intervals: [1000, 2000] },
    )
    .toBe(true);
  await page.goto(`/p/${projectId}/flow`);
  // The notice shows the refusal as a problem whose fix opens Setup; the commit is only in its folded details.
  const notice = page
    .getByTestId("notification-notice")
    .filter({ has: page.getByTestId("problem-what").filter({ hasText: "Preview needs configuration" }) })
    .filter({ has: page.getByTestId("problem-details").filter({ hasText: headCommit }) });
  await expect(notice).toHaveAttribute("data-notification-kind", "delivery", { timeout: 30_000 });
  await expect(notice).toContainText(gap);
  await expect(notice.getByTestId("problem-fix").getByRole("link", { name: "Open Setup" })).toBeVisible();
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
 * the Flow says Live is read only, Runs lists Live runs with Live's events beside them, the Interface
 * shows the Live address, and the chat says changes are built on Preview; back on Preview the
 * Interface frames the Preview.
 */
export async function expectSurfacesFollowVersion(page: Page, projectId: string, liveURL: string) {
  await page.goto(`/p/${projectId}/flow`);
  const live = page.getByTestId("environment-live");
  await live.click();
  await expect(page).toHaveURL(/[?&]v=live(&|$)/);
  await expect(live).toHaveAttribute("aria-current", "page");
  await expect(page.getByText(/read only here/).first()).toBeVisible({ timeout: 60_000 });
  await expect(page.getByTestId("live-note")).toBeVisible();
  const surfaces = page.getByRole("navigation", { name: "Surface" });
  await surfaces.getByRole("link", { name: "Runs", exact: true }).click({ timeout: 30_000 });
  await expect(page).toHaveURL(new RegExp(`/p/${projectId}/runs\\?.*v=live`));
  await expect(page.getByText("Runs · Live", { exact: true })).toBeVisible({ timeout: 60_000 });
  // Beside the runs, Events lists what Live's triggers received.
  await page.getByRole("tab", { name: "Events", exact: true }).click();
  await expect(page.getByRole("region", { name: "Recent events received by Live", exact: true })).toBeVisible({
    timeout: 30_000,
  });
  await surfaces.getByRole("link", { name: "Interface", exact: true }).click({ timeout: 30_000 });
  await expect(page.getByText(liveURL, { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("link", { name: "Open", exact: true })).toHaveAttribute("href", liveURL);
  await page.getByTestId("environment-preview").click();
  await expect(page).not.toHaveURL(/[?&]v=live/);
  await expect(page.getByRole("region", { name: "Project Preview", exact: true })).toBeVisible();
  await expect(page.getByTestId("live-note")).toHaveCount(0);
}
