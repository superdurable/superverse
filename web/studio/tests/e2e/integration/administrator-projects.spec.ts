// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, test } from "@/tests/e2e/support/coverage/spec-coverage";
import { type APIRequestContext, type BrowserContext, type Page } from "@playwright/test";
import { randomUUID } from "node:crypto";
import type { RpcInput, RpcOutput, UnaryMethod } from "@/lib/shared/rpc-methods";
import { identity, platformAdminEmail } from "@/tests/e2e/support/account";
import { readPrivateJSON } from "@/tests/e2e/support/private-files";
import { rpc } from "@/tests/e2e/support/studio-api";

function administratorPassword(): string {
  const filename = process.env.SUPERVERSE_E2E_ADMINISTRATOR_FILE;
  if (!filename) throw new Error("Requires private credentials for the normally registered administrator");
  const value = readPrivateJSON(filename, "Private administrator credentials", 16 * 1024);
  if (
    !value ||
    typeof value !== "object" ||
    !("email" in value) ||
    value.email !== platformAdminEmail() ||
    !("password" in value) ||
    typeof value.password !== "string"
  )
    throw new Error("Invalid private administrator credentials");
  return value.password;
}

/** One RPC answered with `status`; its body is read only on success. */
async function expectRpc<M extends UnaryMethod>(
  context: APIRequestContext,
  method: M,
  input: RpcInput<M>,
  status: number,
): Promise<RpcOutput<M> | null> {
  const response = await rpc(context, method, input, { timeout: 15_000 });
  expect(response.status()).toBe(status);
  const body: RpcOutput<M> | null = response.ok() ? await response.json() : null;
  await response.dispose();
  return body;
}

/** The status of one RPC, asked again every second until it is `status`: the read model lags a write by seconds. */
async function eventually<M extends UnaryMethod>(
  context: APIRequestContext,
  method: M,
  input: RpcInput<M>,
  status: number,
) {
  await expect
    .poll(
      async () => {
        const response = await rpc(context, method, input, { timeout: 15_000 });
        const answered = response.status();
        await response.dispose();
        return answered;
      },
      { timeout: 60_000, intervals: [1000] },
    )
    .toBe(status);
}

async function snapshot(page: Page, projectId: string, cursor = ""): Promise<RpcOutput<"WatchProject">> {
  return page.evaluate(
    async ({ projectId, cursor }) => {
      const controller = new AbortController();
      const timer = window.setTimeout(() => controller.abort(), 15_000);
      let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
      try {
        const response = await fetch(`/api/rpc/WatchProject?${new URLSearchParams({ projectId })}`, {
          signal: controller.signal,
          headers: cursor ? { "last-event-id": cursor } : {},
        });
        if (!response.ok || !response.body) throw new Error("Project Watch unavailable");
        reader = response.body.getReader();
        const decoder = new TextDecoder();
        let text = "";
        while (text.length < 1024 * 1024) {
          const next = await reader.read();
          if (next.done) throw new Error("Project Watch ended without a snapshot");
          text += decoder.decode(next.value, { stream: true });
          const boundary = text.indexOf("\n\n");
          if (boundary < 0) continue;
          const lines = text.slice(0, boundary).split("\n");
          if (!lines.includes("event: snapshot")) throw new Error("Project Watch first frame was not a snapshot");
          const payload = lines.find((line) => line.startsWith("data: "));
          if (!payload) throw new Error("Project Watch snapshot payload is absent");
          return JSON.parse(payload.slice(6));
        }
        throw new Error("Project Watch exceeded the bounded snapshot budget");
      } finally {
        window.clearTimeout(timer);
        controller.abort();
        await reader?.cancel().catch(() => {});
        reader?.releaseLock();
      }
    },
    { projectId, cursor },
  );
}

// Actual Studio/BFF/gRPC, Teamspace and Project Flows, the read model, the repository host, the runtime and
// project Dex. This scenario owns two new Teamspaces, each with one managed Project, deletes them and revokes all
// its logins. Provider credentials and response payloads never enter test attachments.
test.use({ trace: "off", screenshot: "off", video: "off" });
test("administrator lists, observes and manages the Teamspaces and Projects of different accounts", async ({
  browser,
  page,
  context,
}, testInfo) => {
  test.setTimeout(15 * 60_000);
  const password = administratorPassword();
  const baseURL = process.env.SUPERVERSE_E2E_BASE_URL!;
  const origin = new URL(baseURL).origin;
  const owners: {
    context: BrowserContext;
    projectId?: string;
    accountId: string;
    creationRequestId?: string;
    teamSpaceId?: string;
    teamSpaceName: string;
  }[] = [];
  const cleaned = new Set<string>();
  const deletedTeamSpaces = new Set<string>();
  let adminLoggedIn = false,
    phase = "administrator login";
  const failures: Error[] = [];
  try {
    const login = await context.request.post("/api/auth/login", {
      headers: { origin },
      data: { email: platformAdminEmail(), password },
      timeout: 15_000,
    });
    adminLoggedIn = login.status() === 200;
    expect(adminLoggedIn).toBe(true);
    expect((await login.json()).account.isAdmin === true).toBe(true);
    await login.dispose();
    await page.goto("/");
    for (let index = 0; index < 2; index++) {
      phase = `owner ${index + 1} registration and Project creation`;
      const ownerContext = await browser.newContext({ baseURL });
      const owner = {
        context: ownerContext,
        accountId: "",
        projectId: undefined as string | undefined,
        creationRequestId: randomUUID(),
        teamSpaceId: undefined as string | undefined,
        teamSpaceName: `Administrator acceptance ${randomUUID().slice(0, 8)}`,
      };
      owners.push(owner);
      const account = identity();
      const registration = await ownerContext.request.post("/api/auth/register", {
        headers: { origin },
        data: account,
        timeout: 15_000,
      });
      expect(registration.status()).toBe(200);
      const registered = await registration.json();
      owner.accountId = registered.account.accountId;
      expect(registered.account.isAdmin === false).toBe(true);
      expect(owner.accountId.length > 0).toBe(true);
      await registration.dispose();
      // Each owner works in a Teamspace of their own, which they create and administer.
      const teamSpace = await rpc(
        ownerContext.request,
        "CreateTeamSpace",
        { requestId: randomUUID(), name: owner.teamSpaceName },
        { timeout: 30_000 },
      );
      expect(teamSpace.status()).toBe(200);
      owner.teamSpaceId = (await teamSpace.json()).teamSpaceId as string;
      await teamSpace.dispose();
      await eventually(ownerContext.request, "GetTeamSpace", { teamSpaceId: owner.teamSpaceId }, 200);
      const create = await rpc(
        ownerContext.request,
        "CreateProject",
        {
          requestId: owner.creationRequestId,
          name: `Administrator acceptance ${owner.creationRequestId}`,
          managedRepository: {},
          teamSpaceId: owner.teamSpaceId,
        },
        { timeout: 30_000 },
      );
      expect(create.status()).toBe(200);
      owner.projectId = (await create.json()).projectId;
      expect(typeof owner.projectId === "string" && /^[a-z0-9]+$/.test(owner.projectId)).toBe(true);
      await create.dispose();
      await expect
        .poll(
          async () => {
            const input = { projectId: owner.projectId };
            const response = await rpc(ownerContext.request, "GetProject", input, { timeout: 15_000 });
            const status = response.status();
            if (status === 404) {
              await response.dispose();
              return false;
            }
            expect(status).toBe(200);
            const { project }: RpcOutput<"GetProject"> = await response.json();
            await response.dispose();
            expect(project?.creatorAccountId === owner.accountId).toBe(true);
            if (project?.status === "PROJECT_STATUS_SETUP_FAILED" || project?.failure)
              throw new Error("Actual Project setup failed");
            return project?.status === "PROJECT_STATUS_READY";
          },
          { timeout: 5 * 60_000, intervals: [1000, 2000] },
        )
        .toBe(true);
      // ProjectFlow starts the Project's one Session once the Project is READY.
      await expect
        .poll(
          async () => {
            const response = await rpc(ownerContext.request, "GetSession", { projectId: owner.projectId! });
            const status = response.status();
            await response.dispose();
            return status;
          },
          { timeout: 60_000, intervals: [1000] },
        )
        .toBe(200);
    }
    expect(owners[0].accountId !== owners[1].accountId).toBe(true);
    phase = "administrator listing and principal-bound pages";
    // The administrator lists every Teamspace, belonging to none of them, and every Project of each as its owner.
    await expect
      .poll(
        async () => {
          const found = new Map<string, string>();
          let cursor = "";
          for (let index = 0; index < 100; index++) {
            const input = { pageSize: 25, pageToken: cursor };
            const list = (await expectRpc(context.request, "ListTeamSpaces", input, 200))!;
            for (const item of list.teamSpaces) found.set(item.teamSpaceId, item.callerRole);
            if (owners.every((owner) => found.has(owner.teamSpaceId!))) break;
            if (!list.nextPageToken) return false;
            if (list.nextPageToken === cursor) throw new Error("Administrator cursor did not advance");
            cursor = list.nextPageToken;
          }
          return owners.every((owner) => found.get(owner.teamSpaceId!) === "TEAM_SPACE_ROLE_UNSPECIFIED");
        },
        { timeout: 3 * 60_000, intervals: [1000, 2000] },
      )
      .toBe(true);
    for (const owner of owners) {
      const input = { teamSpaceId: owner.teamSpaceId!, pageSize: 25 };
      const list = (await expectRpc(context.request, "ListProjects", input, 200))!;
      const listed = list.projects.find((project) => project.projectId === owner.projectId);
      expect(listed?.callerRole).toBe("PROJECT_ROLE_OWNER");
    }
    for (const [index, owner] of owners.entries()) {
      phase = `Project ${index + 1} administrator reads and ordinary-user isolation`;
      const projectId = owner.projectId!;
      const other = owners[1 - index];
      const teamSpaceId = owner.teamSpaceId!;
      const personal = (await expectRpc(owner.context.request, "ListProjects", { teamSpaceId }, 200))!;
      expect(personal.projects.every((project) => project.creatorAccountId === owner.accountId)).toBe(true);
      // The other owner is outside this Teamspace: it neither reads nor lists it.
      await expectRpc(other.context.request, "GetTeamSpace", { teamSpaceId }, 403);
      await expectRpc(other.context.request, "ListProjects", { teamSpaceId }, 403);
      const first = await snapshot(page, projectId);
      expect(first.snapshot?.projectId === projectId && first.snapshot.creatorAccountId === owner.accountId).toBe(true);
      const reconnected = await snapshot(page, projectId, first.resumeCursor);
      expect(reconnected.snapshot?.projectId === projectId).toBe(true);
      // The administrator reads the Project's resources; the other owner is denied each read. Live
      // configuration names a Release, so an authorized read without one is an invalid target.
      const readProjectResources = async (request: APIRequestContext, denied: boolean) => {
        const status = (permitted: number) => (denied ? 403 : permitted);
        await expectRpc(request, "GetProject", { projectId }, status(200));
        await expectRpc(request, "GetSession", { projectId }, status(200));
        await expectRpc(request, "ListReleases", { projectId }, status(200));
        await expectRpc(request, "ListDeployments", { projectId }, status(200));
        const target = { projectId, scope: "PROJECT_SCOPE_LIVE" } as const;
        await expectRpc(request, "GetProjectConfiguration", { target }, status(400));
      };
      await readProjectResources(context.request, false);
      await readProjectResources(other.context.request, true);
      const forbiddenDelete = { projectId, requestId: randomUUID(), expectedRevision: first.snapshot!.revision };
      await expectRpc(other.context.request, "DeleteProject", forbiddenDelete, 403);
      phase = `Project ${index + 1} administrator deletion`;
      await page.goto(`/p/${projectId}`);
      await expect(page.getByRole("link", { name: "Open the project", exact: true })).toBeVisible();
      // Deletion is fenced on the revision the administrator reads now.
      const shown = (await expectRpc(context.request, "GetProject", { projectId }, 200))!.project!;
      const deletion = { projectId, requestId: randomUUID(), expectedRevision: shown.revision };
      await expectRpc(context.request, "DeleteProject", deletion, 200);
      await expect
        .poll(
          async () => {
            const current = (await snapshot(page, projectId)).snapshot;
            expect(current?.projectId === projectId && current.creatorAccountId === owner.accountId).toBe(true);
            return current!.status === "PROJECT_STATUS_DELETED";
          },
          { timeout: 5 * 60_000, intervals: [1000, 2000] },
        )
        .toBe(true);
      cleaned.add(projectId);
      await expectRpc(context.request, "GetProject", { projectId }, 403);
    }
    // Only the administrator deletes a Teamspace: the first from its Home, after typing its name, the second
    // through the API. Their Projects are deleted already, so each deletion ends soon.
    for (const [index, owner] of owners.entries()) {
      phase = `Teamspace ${index + 1} administrator deletion`;
      const teamSpaceId = owner.teamSpaceId!;
      await expectRpc(owner.context.request, "DeleteTeamSpace", { teamSpaceId, requestId: randomUUID() }, 403);
      if (index === 0) {
        await page.goto(`/t/${teamSpaceId}`);
        await page.getByRole("button", { name: "Delete Teamspace…", exact: true }).click();
        const dialog = page.getByRole("dialog", { name: `Delete ${owner.teamSpaceName}` });
        const confirm = dialog.getByRole("button", { name: "Delete Teamspace", exact: true });
        await expect(confirm).toBeDisabled();
        await dialog.getByLabel("Type the Teamspace's name to confirm", { exact: true }).fill(owner.teamSpaceName);
        await confirm.click();
        await expect(page.getByRole("heading", { name: `${owner.teamSpaceName} is being deleted` })).toBeVisible();
      } else await expectRpc(context.request, "DeleteTeamSpace", { teamSpaceId, requestId: randomUUID() }, 200);
      await eventually(context.request, "GetTeamSpace", { teamSpaceId }, 404);
      deletedTeamSpaces.add(teamSpaceId);
    }
  } catch {
    failures.push(new Error(`Real administrator acceptance failed during ${phase}`));
  } finally {
    for (const owner of owners) {
      if (owner.creationRequestId && !owner.projectId)
        failures.push(
          new Error(`Unconfirmed Project start ${owner.creationRequestId} requires same-request reconciliation`),
        );
      if (owner.projectId && !cleaned.has(owner.projectId)) {
        // Use the normal owner cookie jar created by this test, never a log token.
        // Preserve the exact ID on failure; no blanket namespace/repository deletion.
        try {
          const read = await rpc(owner.context.request, "GetProject", { projectId: owner.projectId });
          const { project }: RpcOutput<"GetProject"> = await read.json();
          await read.dispose();
          const deletion = {
            projectId: owner.projectId,
            requestId: `admin-acceptance-cleanup-${owner.projectId}`,
            expectedRevision: project?.revision ?? "0",
          };
          const response = await rpc(owner.context.request, "DeleteProject", deletion, { timeout: 15_000 });
          if (response.status() !== 200)
            failures.push(new Error(`Project ${owner.projectId} cleanup admission failed`));
          else
            failures.push(
              new Error(`Project ${owner.projectId} cleanup accepted; terminal cleanup still requires reconciliation`),
            );
          await response.dispose();
        } catch {
          failures.push(new Error(`Project ${owner.projectId} cleanup remains unverified`));
        }
      }
      if (owner.teamSpaceId && !deletedTeamSpaces.has(owner.teamSpaceId))
        failures.push(
          new Error(`Teamspace ${owner.teamSpaceId} remains; the administrator deletes it after its Projects`),
        );
      try {
        const logout = await owner.context.request.post("/api/auth/logout", { headers: { origin }, timeout: 15_000 });
        if (owner.accountId && logout.status() !== 200) failures.push(new Error("Owner login revocation failed"));
        await logout.dispose();
      } catch {
        failures.push(new Error("Owner login revocation remains unverified"));
      }
      await owner.context.close();
    }
    if (adminLoggedIn) {
      try {
        const logout = await context.request.post("/api/auth/logout", { headers: { origin }, timeout: 15_000 });
        expect(logout.status()).toBe(200);
        await logout.dispose();
      } catch {
        failures.push(new Error("Administrator login revocation remains unverified"));
      }
    }
  }
  if (failures.length) throw new AggregateError(failures, "Actual administrator acceptance failed");
  await testInfo.attach("administrator-project-access", {
    body: JSON.stringify({
      projects: [...cleaned],
      teamSpaces: [...deletedTeamSpaces],
      distinctOwners: owners.length,
      firstAndReconnectSnapshots: true,
      ordinaryUserIsolation: true,
      deletedProjectsConfirmed: true,
    }),
    contentType: "application/json",
  });
});
