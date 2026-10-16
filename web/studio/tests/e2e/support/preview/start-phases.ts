// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Page } from "@playwright/test";
import { expectPhaseTimings, readSession } from "@/tests/e2e/support/project";

/** The phases a Preview start measures in order, with the labels the Preview panel gives them. */
const PREVIEW_START_PHASES = [
  ["configuration", "Validating configuration"],
  ["environment", "Starting engine"],
  ["grant", "Authorizing"],
  ["application", "Building app"],
  ["route", "Exposing route"],
] as const;

/** The phases a workspace start measures in order. */
const WORKSPACE_START_PHASES = ["provision", "checkout"] as const;

/**
 * Requires the READY Preview to carry the five phases of its start and the Session's workspace the two of its
 * start, all ended and in order, and the Preview panel to list the Preview's with their labels.
 */
export async function expectStartPhases(page: Page, projectId: string) {
  const { preview, workspace } = await readSession(page, projectId);
  const previewPhases = PREVIEW_START_PHASES.map(([phase]) => phase);
  expectPhaseTimings(preview?.phaseTimings ?? [], previewPhases);
  expect(
    preview?.phaseTimings.map((timing) => timing.phase),
    "the Preview phases follow the start",
  ).toEqual(previewPhases);
  expectPhaseTimings(workspace?.phaseTimings ?? [], WORKSPACE_START_PHASES);
  expect(
    workspace?.phaseTimings.map((timing) => timing.phase),
    "the workspace phases follow its start",
  ).toEqual([...WORKSPACE_START_PHASES]);
  // The last start's phases are under the Interface's Details, beside the history, whose starts list theirs too.
  const panel = page.getByRole("region", { name: "Project Preview" });
  const phases = panel.getByRole("region", { name: "Preview start phases", exact: true });
  await expect(async () => {
    if (!(await phases.isVisible())) await panel.getByText("Details", { exact: true }).click();
    await expect(phases).toBeVisible({ timeout: 2_000 });
  }).toPass({ timeout: 60_000 });
  for (const [, label] of PREVIEW_START_PHASES) await expect(phases.getByText(label, { exact: true })).toBeVisible();
}
