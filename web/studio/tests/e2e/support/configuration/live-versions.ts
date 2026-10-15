// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Locator, type Page, type Response } from "@playwright/test";
import {
  fillTemplateSummaryConnection,
  openPreviewConfiguration,
  setupSideOf,
  type SetupFill,
  type SetupSide,
} from "@/tests/e2e/support/configuration";
import { readSession } from "@/tests/e2e/support/project";
import { publishLive, type PublishOptions } from "@/tests/e2e/support/publishing";
import { chooseOption, rpcAnswered } from "@/tests/e2e/support/studio-api";

// The pieces of a Project whose Live runs one version while a newer version on main declares other
// configuration: publishing a chosen version, filling an environment value on Live's next publish or on
// Preview, and applying Preview's settings. Setup's Live tab specs, and the Publish popover's,
// cancellation's and shared-channel cases, build their fixtures from them.

const environmentSaved = (value: string) => (response: Response) =>
  new URL(response.url()).pathname.endsWith("/api/v2/application-environment") &&
  response.request().method() === "PUT" &&
  (response.request().postData() ?? "").includes(JSON.stringify(value));

/** A variable's control on one side of the Setup table on screen, its optional variables unfolded first. */
export async function variableControl(scope: Page | Locator, name: string, side: SetupSide) {
  const variables = scope.getByTestId("setup-variables");
  await expect(variables).toBeVisible({ timeout: 60_000 });
  const control = variables.getByLabel(`${name} ${side === "next" ? "for the next publish" : "on Preview"}`, {
    exact: true,
  });
  if (!(await control.isVisible())) await variables.getByRole("button", { name: /optional$/ }).click();
  await expect(control).toBeVisible();
  return control;
}

/**
 * Fills one declared environment value in Setup on `side` and waits until it is saved: Preview's column, or
 * Live's next publish, on the Preview tab's Next publish column or the Live tab, where a value that follows
 * Preview first becomes Live's own through "Different on Live…".
 */
export async function fillEnvironmentValue(page: Page, name: string, value: string, side = setupSideOf(page)) {
  let control = await variableControl(page, name, side);
  if ((await control.evaluate((element) => element.tagName)) === "BUTTON") {
    await chooseOption(control, "Different on Live…");
    control = await variableControl(page, name, side);
  }
  const saved = page.waitForResponse(environmentSaved(value), { timeout: 30_000 });
  await control.fill(value);
  expect((await saved).ok()).toBe(true);
}

/** On Live's next publish, the template's summary connection and one declared environment value. */
export function fillLiveSummaryAnd(name: string, value: string): SetupFill {
  return async (page, side = setupSideOf(page)) => {
    await fillTemplateSummaryConnection(page, side);
    await fillEnvironmentValue(page, name, value, side);
  };
}

/**
 * Publishes the version `commit` to Live through the Publish popover, with Live's next publish filled in
 * Setup's Next publish column first; a version that is not in the Preview record goes with a skip reason.
 */
export async function publishVersion(
  page: Page,
  projectId: string,
  commit: string,
  fillConnections: SetupFill,
  options: Omit<PublishOptions, "commit" | "mode"> = {},
) {
  await page.goto(`/p/${projectId}/flow`);
  return publishLive(page, projectId, fillConnections, {
    commit,
    skipReason: FIXTURE_SKIP_REASON,
    ...options,
  });
}

/** The reason the fixture publishes its first version with, before Preview ran it. */
export const FIXTURE_SKIP_REASON = "The fixture publishes its first version before Preview runs it";

/**
 * Sets one environment value in Setup's Preview column, which Setup applies on its own, then waits until the
 * Preview serves the Session's head with that configuration applied.
 */
export async function applyPreviewValue(page: Page, projectId: string, name: string, value: string) {
  const head = (await readSession(page, projectId)).headCommit;
  await openPreviewConfiguration(page, projectId);
  // Setup applies the saved value once edits settle and any update of a push has finished: to the running
  // Preview, or to one whose update to the push was refused for the value this sets.
  const applied = page.waitForResponse(
    (response) =>
      rpcAnswered("StartPreview")(response) &&
      (response.request().postDataJSON() as Record<string, unknown>).applyConfiguration === true &&
      response.ok(),
    { timeout: 10 * 60_000 },
  );
  await fillEnvironmentValue(page, name, value);
  await applied;
  await expect
    .poll(
      async () => {
        const preview = (await readSession(page, projectId)).preview;
        if (preview?.failure) throw new Error(`Preview failed at ${preview.failure.phase}: ${preview.failure.code}`);
        return preview?.status === "PREVIEW_STATUS_READY" && preview.appliedConfiguration?.sourceCommit === head;
      },
      { timeout: 10 * 60_000, intervals: [2000] },
    )
    .toBe(true);
}
