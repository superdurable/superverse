// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Page } from "@playwright/test";
import { takeFirstTeamSpace } from "@/tests/e2e/support/account";
import { chooseOption, rpc, rpcAnswered } from "@/tests/e2e/support/studio-api";

/** How long a change takes to reach the read model, which every read and authorization uses. */
const READ_MODEL_MS = 60_000;

/** The start page of an empty Teamspace's Home, which offers its first Project. */
export const START_HEADING = "What process should Dex run?";

/**
 * The first Teamspace of an account registered a moment ago, which Studio creates without a question, named
 * from the email: the one `register` saw created, or, for an account without one, the one `/` creates. Returns
 * its ID once its Home offers the first Project. A repeated create of the page's request names the same
 * Teamspace.
 */
export async function createTeamSpace(page: Page): Promise<string> {
  let first = takeFirstTeamSpace(page);
  if (!first) {
    const answered = page.waitForResponse(rpcAnswered("CreateTeamSpace"), { timeout: READ_MODEL_MS });
    await page.goto("/");
    const response = await answered;
    first = {
      status: response.status(),
      request: response.request().postDataJSON(),
      accepted: response.ok() ? await response.json() : { teamSpaceId: "" },
    };
  }
  expect(first.status).toBe(200);
  const { accepted, request } = first;
  expect(accepted.teamSpaceId).toMatch(/^[a-z0-9]+$/);
  expect(request.name).toMatch(/\S/);
  const replay = await rpc(page.request, "CreateTeamSpace", request);
  expect(await replay.json()).toEqual(accepted);
  await expect(page).toHaveURL(new RegExp(`/t/${accepted.teamSpaceId}$`));
  await expect(page.getByRole("heading", { name: START_HEADING })).toBeVisible({ timeout: READ_MODEL_MS });
  const name = String(request.name).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  await expect(page.getByRole("button", { name: new RegExp(`^${name} · admin`) })).toBeVisible();
  return accepted.teamSpaceId;
}

/**
 * The Teamspace `/` opens for the signed-in account: the one it last opened or its first, or the one Studio
 * creates when it has none, as for an account registered a moment ago.
 */
export async function openTeamSpace(page: Page): Promise<string> {
  if (takeFirstTeamSpace(page)) await page.waitForURL(/\/t\/[^/]+$/);
  else await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Projects" }).or(page.getByRole("heading", { name: START_HEADING })),
  ).toBeVisible({ timeout: READ_MODEL_MS });
  const id = new URL(page.url()).pathname.match(/^\/t\/([^/]+)/)?.[1];
  if (!id) throw new Error("The landing opened no Teamspace");
  return decodeURIComponent(id);
}

/** Adds the registered account `email` to the Teamspace through People, as one of its admins does. */
export async function addTeamSpaceMember(page: Page, teamSpaceId: string, email: string, role: "admin" | "member") {
  await page.goto(`/t/${teamSpaceId}/people`);
  const form = page.getByRole("form", { name: "Add a person" });
  await form.getByLabel("Email of a registered account", { exact: true }).fill(email);
  await chooseOption(form.getByRole("button", { name: "Role", exact: true }), role);
  const answered = page.waitForResponse(rpcAnswered("AddTeamSpaceMember"));
  await form.getByRole("button", { name: "Add", exact: true }).click();
  expect((await answered).ok()).toBe(true);
  const members = page.getByRole("table", { name: "Teamspace members" });
  await expect(members.getByRole("row").filter({ hasText: email })).toBeVisible();
}

/**
 * Gives a member of the Project's Teamspace a role through Tools, Permissions, as one of its owners does. The
 * Teamspace's new member reaches the read model the list reads within seconds.
 */
export async function addProjectMember(
  page: Page,
  projectId: string,
  email: string,
  role: "owner" | "builder" | "operator",
) {
  const form = page.getByRole("form", { name: "Add to the project" });
  const person = form.getByRole("button", { name: "A member of the Teamspace", exact: true });
  await expect
    .poll(
      async () => {
        await page.goto(`/p/${projectId}/tools?tool=permissions`);
        await expect(person).toBeVisible({ timeout: 30_000 });
        // The list of the Teamspace's people loads after the page; with nobody to add, the dropdown is off.
        await expect(person).not.toHaveText(/^Loading/, { timeout: 30_000 });
        if (await person.isDisabled()) return 0;
        await person.click();
        const offered = await page.getByRole("listbox").getByRole("option", { name: email, exact: true }).count();
        await page.keyboard.press("Escape");
        return offered;
      },
      { timeout: READ_MODEL_MS, intervals: [2000] },
    )
    .toBe(1);
  await chooseOption(person, email);
  await chooseOption(form.getByRole("button", { name: "Role", exact: true }), role);
  const answered = page.waitForResponse(rpcAnswered("AddProjectMember"));
  await form.getByRole("button", { name: "Add", exact: true }).click();
  expect((await answered).ok()).toBe(true);
  const members = page.getByRole("table", { name: "Project members" });
  await expect(members.getByRole("row").filter({ hasText: email })).toBeVisible();
}

/**
 * Opens the Project as an operator, once the read model grants it: Live only, with no Preview, no chat and no
 * Tools, and the role word "operate" in the bar.
 */
export async function openAsOperator(page: Page, projectId: string) {
  await expect
    .poll(async () => (await rpc(page.request, "GetProject", { projectId })).status(), {
      timeout: READ_MODEL_MS,
      intervals: [2000],
    })
    .toBe(200);
  await page.goto(`/p/${projectId}/flow`);
  const bar = page.getByRole("group", { name: "Which environment you are looking at" });
  await expect(bar.getByTestId("environment-live")).toBeVisible({ timeout: 60_000 });
  await expect(bar.getByTestId("environment-preview")).toHaveCount(0);
  await expect(page.getByText("operate", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("region", { name: "Project chat" })).toHaveCount(0);
  const surfaces = page.getByRole("navigation", { name: "Surface" });
  await expect(surfaces.getByRole("link", { name: "Inbox" })).toBeVisible();
  await expect(surfaces.getByRole("link", { name: "Tools" })).toHaveCount(0);
}
