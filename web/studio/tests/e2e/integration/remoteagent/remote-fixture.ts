// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { platformAdminEmail } from "@/tests/e2e/support/account";
import { expect, type Browser, type BrowserContext, type Page } from "@playwright/test";
import { signIn } from "@/tests/e2e/support/account";
import { matching, object, requireCondition, string } from "@/tests/e2e/support/json-assert";
import { ownerFixture, ownerProjectFixture } from "@/tests/e2e/support/private-files";
import { readProject, readSession } from "@/tests/e2e/support/project";
import { rpcAnswered, rpcResult, studioOrigin } from "@/tests/e2e/support/studio-api";
import { addProjectMember } from "@/tests/e2e/support/teamspace/teamspace";
import type { RpcOutput } from "@/lib/shared/rpc-methods";
import { studioRoutes } from "@/lib/shared/studio-routes";
import { RemoteClient } from "./remote-client";
import { cleanupRemoteFixture } from "./remote-cleanup";

export async function openExecutorMenu(page: Page) {
  const picker = page.locator("[data-session-executor]");
  await expect(picker).toBeVisible();
  await picker.getByRole("button", { name: /^Agent:/ }).click();
  return page.getByRole("dialog", { name: "Choose agent" });
}

/** Owns only an exclusive disposable Project's test connections, exact added membership and secondary logins. */
class RemoteFixture {
  readonly fixture = ownerProjectFixture(
    string(process.env.SUPERVERSE_E2E_REMOTE_AGENT_FIXTURE, "Remote fixture path"),
    "Remote fixture",
  );
  readonly clients: RemoteClient[] = [];
  readonly ownedConnections = new Set<string>();
  readonly ownedMessages = new Set<string>();
  readonly #pairingAccounts = new Set<string>();
  readonly #logins: Page[] = [];
  readonly #contexts: BrowserContext[] = [];
  readonly #finalizers: (() => Promise<void>)[] = [];
  #ownerAccountId = "";
  #builderAccountId = "";
  #membershipRequested = false;
  #admitted = false;
  constructor(readonly page: Page) {}

  async prepare() {
    if (process.env.SUPERVERSE_STUDIO_MOCK === "true") throw new Error("Remote specs require actual lane dependencies");
    this.#logins.push(this.page);
    await signIn(this.page, this.fixture);
    const project = await readProject(this.page, this.fixture.projectId);
    if (project.status !== "PROJECT_STATUS_READY" || project.callerRole !== "PROJECT_ROLE_OWNER")
      throw new Error("Remote fixture requires a READY owner-authorized disposable Project");
    const snapshot = await readSession(this.page, this.fixture.projectId);
    const agent = snapshot.agent;
    if (
      snapshot.status !== "SESSION_STATUS_ACTIVE" ||
      agent?.description?.model !== "AGENT_MODEL_MOCK" ||
      agent.description.status !== "STATUS_WAITING_FOR_MESSAGE" ||
      agent.description.executor?.mode !== "SESSION_EXECUTOR_MODE_PLATFORM" ||
      agent.queued.length ||
      agent.steered.length ||
      agent.description.pendingUserInput ||
      agent.description.pendingApproval ||
      agent.description.backgroundCommands.length ||
      agent.description.subagents.length
    )
      throw new Error("Remote fixture requires an idle ACTIVE platform/mock Session with no pending input or tasks");
    const listed = await rpcResult(this.page.request, "ListRemoteAgentConnections", {
      projectId: this.fixture.projectId,
    });
    if (listed.connections.length || listed.currentConnectionId)
      throw new Error("Remote fixture must begin with no connections");
    const members = await rpcResult(this.page.request, "ListProjectMembers", { projectId: this.fixture.projectId });
    const own = members.members.filter(
      (member) => member.email === this.fixture.email.trim().toLowerCase() && member.role === "PROJECT_ROLE_OWNER",
    );
    if (own.length !== 1) throw new Error("Remote fixture must identify one explicit owner membership");
    this.#ownerAccountId = own[0].accountId;
    this.#admitted = true;
    await this.page.goto(studioRoutes.projectSurface(this.fixture.projectId, "flow"));
  }

  attemptPairing(page: Page) {
    if (!this.#logins.includes(page)) throw new Error("Pairing page is not a test-owned login");
    this.#pairingAccounts.add(page === this.page ? this.#ownerAccountId : this.#builderAccountId);
  }

  track(pairing: RpcOutput<"CreateRemoteAgentPairing">) {
    this.ownedConnections.add(pairing.connectionId);
    const client = new RemoteClient(pairing.connectionId, pairing.endpoint);
    this.clients.push(client);
    return client;
  }

  async connect(page = this.page) {
    await page.goto(studioRoutes.projectSurface(this.fixture.projectId, "flow"));
    const menu = await openExecutorMenu(page);
    await menu.getByRole("button", { name: /^Connect (a|another) local agent/ }).click();
    const dialog = page.getByRole("dialog", { name: "Connect my agent" });
    await dialog.getByRole("checkbox", { name: "I understand these local-machine permissions." }).check();
    this.attemptPairing(page);
    const created = page.waitForResponse(rpcAnswered("CreateRemoteAgentPairing"));
    await dialog.getByRole("button", { name: "Create pairing code", exact: true }).click();
    const response = await created;
    if (!response.ok()) throw new Error(`Pairing returned HTTP ${response.status()}`);
    const pairing: RpcOutput<"CreateRemoteAgentPairing"> = await response.json();
    const client = this.track(pairing);
    await client.pair(pairing);
    await dialog.getByRole("button", { name: "Close", exact: true }).click();
    return client;
  }

  async addBuilder(browser: Browser) {
    const extra = ownerFixture(
      string(process.env.SUPERVERSE_E2E_REMOTE_AGENT_FIXTURE, "Remote fixture path"),
      "Remote fixture",
    );
    const builder = object(extra.builder, "Separate Builder credentials");
    requireCondition(
      typeof builder.email === "string" && builder.email.includes("@") && typeof builder.password === "string",
      "Builder credentials are required",
    );
    const email = builder.email.trim().toLowerCase();
    if (email === this.fixture.email.trim().toLowerCase() || email === platformAdminEmail())
      throw new Error("Builder must be a distinct ordinary account");
    const project = await readProject(this.page, this.fixture.projectId);
    const team = await rpcResult(this.page.request, "ListTeamSpaceMembers", { teamSpaceId: project.teamSpaceId });
    const people = team.members.filter((member) => member.email === email);
    if (
      people.length !== 1 ||
      people[0].role !== "TEAM_SPACE_ROLE_MEMBER" ||
      people[0].accountId === this.#ownerAccountId
    )
      throw new Error("Builder fixture must identify one ordinary Teamspace MEMBER");
    const members = await rpcResult(this.page.request, "ListProjectMembers", { projectId: this.fixture.projectId });
    if (members.members.some((member) => member.accountId === people[0].accountId))
      throw new Error("Builder fixture already has a Project role; this test cannot own that membership");
    this.#builderAccountId = people[0].accountId;
    const context = await browser.newContext({ baseURL: studioOrigin() });
    this.#contexts.push(context);
    const page = await context.newPage();
    this.#logins.push(page);
    await signIn(page, { email, password: builder.password });
    this.#membershipRequested = true;
    await addProjectMember(this.page, this.fixture.projectId, email, "builder");
    await expect
      .poll(async () => (await readProject(page, this.fixture.projectId)).callerRole, { timeout: 60_000 })
      .toBe("PROJECT_ROLE_BUILDER");
    await this.page.goto(studioRoutes.projectSurface(this.fixture.projectId, "flow"));
    return { page, accountId: this.#builderAccountId, email };
  }

  async otherProject() {
    const extra = ownerFixture(
      string(process.env.SUPERVERSE_E2E_REMOTE_AGENT_FIXTURE, "Remote fixture path"),
      "Remote fixture",
    );
    const id = matching(extra.otherProjectId, /^[a-z0-9]{1,32}$/, "Other Project ID");
    if (id === this.fixture.projectId) throw new Error("Navigation fixture requires a distinct Project");
    const project = await readProject(this.page, id);
    const current = await readProject(this.page, this.fixture.projectId);
    if (
      project.status !== "PROJECT_STATUS_READY" ||
      project.callerRole !== "PROJECT_ROLE_OWNER" ||
      project.teamSpaceId !== current.teamSpaceId
    )
      throw new Error("Navigation fixture must be a READY owner Project in the same Teamspace");
    return project;
  }

  defer(cleanup: () => Promise<void>) {
    this.#finalizers.push(cleanup);
  }

  async cleanup() {
    await cleanupRemoteFixture({
      page: this.page,
      projectId: this.fixture.projectId,
      admitted: this.#admitted,
      pairingAccounts: this.#pairingAccounts,
      connections: this.ownedConnections,
      messages: this.ownedMessages,
      clients: this.clients,
      logins: this.#logins,
      contexts: this.#contexts,
      finalizers: this.#finalizers,
      builderAccountId: this.#membershipRequested ? this.#builderAccountId : null,
    });
  }
}

/** Retains the first assertion/transport failure when independent cleanup also fails. */
export async function runRemoteCase(page: Page, work: (scope: RemoteFixture) => Promise<void>) {
  const scope = new RemoteFixture(page);
  const failures: unknown[] = [];
  try {
    await scope.prepare();
    await work(scope);
  } catch (error) {
    failures.push(error);
  }
  try {
    await scope.cleanup();
  } catch (error) {
    failures.push(error);
  }
  if (failures.length === 1) throw failures[0];
  if (failures.length > 1) throw new AggregateError(failures, "Remote test failed first; cleanup also failed");
}
