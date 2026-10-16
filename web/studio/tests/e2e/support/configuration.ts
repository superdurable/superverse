// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Locator, type Page, type Response } from "@playwright/test";
import { readFile } from "node:fs/promises";
import type { ProjectConfigurationSnapshot__Output } from "@/lib/generated/protobuf/superverse/v2/ProjectConfigurationSnapshot";
import { chooseOption, rpcResult, safeFill } from "./studio-api";
import { readProject, readSession } from "./project";

/**
 * Switches "Build each push automatically" in Project details to `on`, as the Session's preference
 * reads it back, and leaves the page on Project details.
 */
export async function setAutomaticBuilds(page: Page, projectId: string, on: boolean) {
  await page.goto(`/p/${projectId}`);
  const toggle = page
    .getByRole("region", { name: "Automatic builds", exact: true })
    .getByRole("switch", { name: "Build each push automatically", exact: true });
  await expect(toggle).toBeEnabled({ timeout: 60_000 });
  if ((await toggle.getAttribute("aria-checked")) !== String(on)) await toggle.click();
  await expect(toggle).toHaveAttribute("aria-checked", String(on));
  await expect
    .poll(async () => (await readSession(page, projectId)).agent?.description?.isPreviewAutoDeployEnabled)
    .toBe(on);
}

/**
 * Open the real Tools, Setup on the Preview tab. Pushed versions must finish their automatic build;
 * only the initial template root or an explicitly disabled preference uses the manual control,
 * switching automatic builds off in Project details for the manual build and on again after it.
 */
export async function openPreviewConfiguration(page: Page, projectId: string) {
  const setup = `/p/${projectId}/tools?tool=setup`;
  await page.goto(setup);
  const checklist = page.getByTestId("setup-checklist");
  // Build processes shows only while the latest push is not built; Setup's table only once it is.
  const control = page.getByRole("region", { name: "Build processes", exact: true });
  await expect(checklist.or(control)).toBeVisible({ timeout: 2 * 60_000 });
  const session = await readSession(page, projectId);
  const source = session.preview?.preparedSource;
  const ready = source?.status === "PREVIEW_SOURCE_STATUS_READY" && source.sourceCommit === session.headCommit;
  if (!ready) {
    const automatic = session.agent?.description?.isPreviewAutoDeployEnabled ?? true;
    const project = await readProject(page, projectId);
    const initialRoot = project.setup?.sourceCommit === session.headCommit;
    const manual = !automatic || initialRoot;
    if (manual && automatic) {
      await setAutomaticBuilds(page, projectId, false);
      await page.goto(setup);
    }
    try {
      if (manual) {
        const build = control.getByRole("button", { name: "Build processes", exact: true });
        await expect(build).toBeEnabled({ timeout: 10 * 60_000 });
        await build.click();
      }
      await expect
        .poll(
          async () => {
            const current = await readSession(page, projectId);
            const built = current.preview?.preparedSource;
            if (built?.sourceCommit !== current.headCommit) return false;
            if (built.status === "PREVIEW_SOURCE_STATUS_FAILED")
              throw new Error(`Process build failed (${built.failure?.code ?? "UNKNOWN"}); no automatic retry`);
            return built.status === "PREVIEW_SOURCE_STATUS_READY";
          },
          { timeout: 10 * 60_000, intervals: [1000, 2000] },
        )
        .toBe(true);
    } finally {
      if (manual && automatic) {
        await setAutomaticBuilds(page, projectId, true);
        await page.goto(setup);
      }
    }
  }
  await expect(checklist).toBeVisible({ timeout: 5 * 60_000 });
}

/**
 * Fills Setup's Preview column and waits until the server reports its configuration READY, which Setup checks on
 * every save.
 */
async function configurePreview(
  page: Page,
  projectId: string,
  fillConnections?: (page: Page) => Promise<void>,
): Promise<ProjectConfigurationSnapshot__Output> {
  await openPreviewConfiguration(page, projectId);
  if (fillConnections) await fillConnections(page);
  const target = { projectId, scope: "PROJECT_SCOPE_PREVIEW" as const };
  let current: ProjectConfigurationSnapshot__Output | null = null;
  await expect
    .poll(
      async () => {
        current = (await rpcResult(page.request, "GetProjectConfiguration", { target })).configuration;
        return current?.status;
      },
      { timeout: 3 * 60_000, intervals: [1000, 2000] },
    )
    .toBe("CONFIGURATION_STATUS_READY");
  if (!current) throw new Error("GetProjectConfiguration returned no configuration");
  return current;
}

/** Fills Setup's Preview column with `fillConnections` and waits until the server reports it READY. */
export async function configure(
  page: Page,
  scope: "Preview",
  fillConnections?: (page: Page) => Promise<void>,
): Promise<ProjectConfigurationSnapshot__Output> {
  const projectId = new URL(page.url()).pathname.split("/")[2];
  return configurePreview(page, projectId, fillConnections);
}
async function requireSelectionSaved(response: Response) {
  if (response.ok()) return;
  const result: unknown = await response.json();
  const code =
    result &&
    typeof result === "object" &&
    "code" in result &&
    typeof result.code === "string" &&
    /^[A-Z_]+$/.test(result.code)
      ? result.code
      : "UNKNOWN";
  throw new Error(
    `Connection selection returned HTTP ${response.status()} (${code}); expected configuration ${response.request().headers()["x-dex-configuration-revision"] ?? "absent"}`,
  );
}

// The private file holds {"apiKey":"sk-..."}; missing configuration fails the case.
async function readPrivateOpenAIKey(): Promise<string> {
  const keyFile = process.env.SUPERVERSE_E2E_OPENAI_FILE;
  if (!keyFile) throw new Error("SUPERVERSE_E2E_OPENAI_FILE is required for the real OpenAI summary connection");
  const credentials = JSON.parse(await readFile(keyFile, "utf8"));
  if (typeof credentials.apiKey !== "string" || !credentials.apiKey.startsWith("sk-"))
    throw new Error("Invalid private OpenAI configuration");
  return credentials.apiKey;
}

/** A selection of a connection the Teamspace does not hold, replayed from a saved one, which Go refuses. */
async function expectRejectedSelection(page: Page, saved: Response) {
  const original = await saved.request().allHeaders();
  const headers = Object.fromEntries(
    Object.entries(original).filter(
      ([name]) =>
        name === "origin" ||
        name === "content-type" ||
        name === "x-csrf-token" ||
        name.startsWith("x-dex-") ||
        name.startsWith("x-superverse-"),
    ),
  );
  const unknown = await page.request.put(saved.url(), {
    headers,
    data: { teamSpaceConnectionId: "c00000000000" },
    maxRetries: 1,
  });
  expect(unknown.status()).toBe(404);
}

const selectionSaved = (r: Response) =>
  new URL(r.url()).pathname.includes("/api/v2/connector-selections/") && r.request().method() === "PUT";

/** A column of Tools, Setup, by the words its controls' accessible names end with. */
export type SetupSide = "preview" | "next";
const SIDE_WORDS: Record<SetupSide, string> = { preview: "on Preview", next: "for the next publish" };

/**
 * The column a fill on the page edits by default: Setup on the Live tab has only the next publish's; on the
 * Preview tab Preview's own, while its Next publish column is named explicitly.
 */
export function setupSideOf(page: Page): SetupSide {
  return new URL(page.url()).searchParams.get("v") === "live" ? "next" : "preview";
}

/** Fills Setup on the page, on `side`: Preview's column, or Live's next publish on either tab. */
export type SetupFill = (page: Page, side?: SetupSide) => Promise<void>;

/**
 * A connection name's dropdown on one side of Tools, Setup. On a narrow tool panel the row shows status
 * chips and its dropdowns move into the open row, so the row is unfolded first when it is folded.
 */
export async function connectionPicker(checklist: Locator, connectionName: string, side: SetupSide) {
  const row = checklist.locator(`[data-testid="setup-connection"][data-connection-name="${connectionName}"]`);
  await expect(row).toBeVisible({ timeout: 60_000 });
  const picker = row.getByRole("button", { name: `connection ${connectionName} ${SIDE_WORDS[side]}` });
  if ((await row.getAttribute("data-open")) !== "true" && !(await picker.isVisible()))
    await row.getByRole("button", { name: /^Unfold / }).click();
  await expect(picker).toBeVisible();
  return picker;
}

// Selects a Teamspace connection for an llm connection name in the Setup table on screen, on `side`:
// Preview's column, or the next publish's on the Preview tab's Next publish column or the Live tab.
async function fillOpenAIConnection(page: Page, key: string, connectionName: string, side: SetupSide) {
  const checklist = page.getByTestId("setup-checklist");
  await expect(checklist).toBeVisible({ timeout: 60_000 });
  return fillSetupConnection(page, checklist, key, connectionName, side);
}

/** The action row of a connection name's dropdown that creates a Teamspace connection in its row. */
const NEW_CONNECTION = "New connection…";

/**
 * A connection name in Tools, Setup: a dropdown that already resolves to a connection keeps it, whether it is
 * one the side chose or "Same as Preview" of Preview's; otherwise it takes the Teamspace's first listed
 * connection, which an earlier case or side created, or New connection… creates one inline in the row with the
 * openai provider, the key and the browser cases' model, and selects it for that side.
 */
async function fillSetupConnection(
  page: Page,
  checklist: Locator,
  key: string,
  connectionName: string,
  side: SetupSide,
) {
  const trigger = await connectionPicker(checklist, connectionName, side);
  await expect(trigger).toBeEnabled({ timeout: 30_000 });
  await trigger.click();
  const listbox = page.getByRole("listbox");
  if ((await listbox.getByRole("option", { selected: true }).count()) > 0) {
    await page.keyboard.press("Escape");
    await expect(listbox).toHaveCount(0);
    return;
  }
  const choices = listbox
    .getByRole("option", { disabled: false })
    .filter({ hasNotText: NEW_CONNECTION })
    .filter({ hasNotText: "Same as Preview" });
  const saved = page.waitForResponse(selectionSaved);
  if ((await choices.count()) > 0) await choices.first().click();
  else {
    await listbox.getByRole("option", { name: NEW_CONNECTION, exact: true }).click();
    const editor = checklist.getByTestId("new-connection-inline").getByTestId("team-space-connection-editor");
    await editor.getByLabel("Connection name", { exact: true }).fill(`OpenAI ${Date.now().toString(36)}`);
    await chooseOption(editor.getByRole("button", { name: /^Provider/ }), "openai");
    // The model is an optional setting, folded until opened.
    const model = editor.getByLabel(/^Model/);
    if (!(await model.isVisible())) await editor.getByRole("button", { name: /^Optional settings/ }).click();
    await model.fill("gpt-5-mini");
    await safeFill(editor.getByLabel(/^API key/), key);
    await editor.getByRole("button", { name: "Save", exact: true }).click();
  }
  const answer = await saved;
  await requireSelectionSaved(answer);
  await expectRejectedSelection(page, answer);
  await expect(trigger).not.toHaveText(/^(Not connected|Needs its own|Loading)/);
  await expect(page.getByTestId("new-connection-inline")).toHaveCount(0);
}

// The application template declares one llm connection for TextSummaryFlow.
export const fillTemplateSummaryConnection: SetupFill = async (page, side = setupSideOf(page)) => {
  await fillOpenAIConnection(page, await readPrivateOpenAIKey(), "summary-model", side);
};
