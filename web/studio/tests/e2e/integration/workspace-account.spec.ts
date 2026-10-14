// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { type Page, type Request } from "@playwright/test";
import { constants, openSync, closeSync, fstatSync, statSync, readSync } from "node:fs";
import { delay } from "@/lib/shared/async";
import { ownerProjectFixture, type OwnerProject } from "@/tests/e2e/support/private-files";

const MAX_TRACE_BYTES = 8 * 1024 * 1024;
// A channel trace line that creates a GetCurrentAccount call: its phase and call identity.
const ACCOUNT_CALL =
  /create(Resolving|Retrying)Call \[([0-9]+)\] method="\/superverse\.v2\.AccountService\/GetCurrentAccount"/;

function openPrivateFile(path: string): number {
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  const info = fstatSync(fd);
  if (!info.isFile() || (info.mode & 0o077) !== 0) {
    closeSync(fd);
    throw new Error("Account navigation input must be a private regular file");
  }
  return fd;
}

function fixture(): OwnerProject {
  const path = process.env.SUPERVERSE_E2E_ACCOUNT_NAV_FIXTURE;
  if (!path) throw new Error("Account navigation requires a private real owner/Project fixture");
  return ownerProjectFixture(path, "Account navigation fixture");
}

// grpc-js 1.14.4's existing channel tracer writes method and call identities,
// never request bodies. Read only bytes appended after the secondary login.
// No trace files or account/Session contents enter Playwright attachments.
class AccountCalls {
  private readonly fd: number;
  private readonly device: number;
  private readonly inode: number;
  private readonly start: number;
  private offset: number;
  private partial = "";
  private processId: string | undefined;
  private readonly resolving = new Set<string>();
  private readonly retrying = new Set<string>();

  constructor(private readonly path: string) {
    this.fd = openPrivateFile(path);
    const info = fstatSync(this.fd);
    this.device = info.dev;
    this.inode = info.ino;
    this.start = info.size;
    this.offset = info.size;
    if (info.size) {
      const last = Buffer.alloc(1);
      if (readSync(this.fd, last, 0, 1, info.size - 1) !== 1 || last[0] !== 10) {
        closeSync(this.fd);
        throw new Error("Studio gRPC trace must end on a complete line before observation");
      }
    }
  }

  read() {
    const info = fstatSync(this.fd),
      named = statSync(this.path);
    if (
      named.dev !== this.device ||
      named.ino !== this.inode ||
      info.size < this.offset ||
      info.size - this.start > MAX_TRACE_BYTES
    ) {
      throw new Error("Studio gRPC trace was replaced, truncated or exceeded the observation budget");
    }
    const bytes = Buffer.alloc(info.size - this.offset);
    const count = readSync(this.fd, bytes, 0, bytes.length, this.offset);
    if (count !== bytes.length) throw new Error("Studio gRPC trace could not be read completely");
    this.offset += count;
    const lines = (this.partial + bytes.toString("utf8")).split("\n");
    this.partial = lines.pop()!;
    if (this.partial.length > 16 * 1024) throw new Error("Studio gRPC trace line exceeded the observation budget");
    for (const line of lines) {
      if (!line.includes("| channel |")) continue;
      const process = /\| v1\.14\.4 ([0-9]+) \| channel \|/.exec(line);
      if (!process || (this.processId && this.processId !== process[1]))
        throw new Error("Studio gRPC trace version or process identity changed");
      this.processId = process[1];
      const match = ACCOUNT_CALL.exec(line);
      if (!match) continue;
      const calls = match[1] === "Resolving" ? this.resolving : this.retrying;
      if (calls.has(match[2])) throw new Error("Studio gRPC trace repeated a call identity");
      calls.add(match[2]);
    }
    return {
      invocations: this.resolving.size,
      dispatched: this.retrying.size,
      completeLine: this.partial.length === 0,
      bytes: this.offset - this.start,
    };
  }

  close() {
    closeSync(this.fd);
  }
}

// This is a separate real-dependency scenario, not one of the three full
// acceptance cases. Its fixture owns a READY Project, whose Session is active;
// this test only navigates them and owns/revokes its secondary login.
test.use({ trace: "off", screenshot: "off", video: "off" });
test("Workspace reads the account once per document and retains it across real navigation", async ({
  page,
  context,
}, testInfo) => {
  test.setTimeout(120_000);
  const input = fixture();
  const origin = new URL(process.env.SUPERVERSE_E2E_BASE_URL!).origin;
  const log = process.env.SUPERVERSE_E2E_STUDIO_GRPC_TRACE_FILE;
  if (!log || process.env.SUPERVERSE_E2E_ISOLATED_STUDIO !== "true")
    throw new Error(
      "Account navigation requires a complete private Studio channel trace and an isolated Studio with no competing clients",
    );
  let calls: AccountCalls | undefined,
    loggedIn = false,
    loggedOut = false;
  let phase = "secondary login",
    sessionRequests = 0,
    documents = 0;
  const outcomes: { phase: string; invocations: number; dispatched: number; documents: number }[] = [];
  const failures: Error[] = [];
  const onRequest = (request: Request) => {
    if (new URL(request.url()).pathname === "/api/auth/session") sessionRequests++;
    if (request.isNavigationRequest() && request.resourceType() === "document" && request.frame() === page.mainFrame())
      documents++;
  };
  page.on("request", onRequest);
  async function accountMenu(current: Page) {
    const menu = current.getByTestId("account-menu");
    await menu.locator('summary[aria-label="Account"]').click();
    await expect(menu.getByRole("button", { name: "Sign out", exact: true })).toBeVisible();
    // Do not put the private email in assertion diagnostics.
    expect((await menu.locator("p").first().textContent()) === input.email).toBe(true);
    await menu.locator('summary[aria-label="Account"]').click();
    await expect(menu.getByRole("button", { name: "Sign out", exact: true })).toBeHidden();
  }
  async function observe(expected: number, expectedDocuments: number) {
    // The collector must be a complete, unfiltered live stderr capture. The
    // bounded quiet interval detects deferred menu effects; it is not a claim
    // that arbitrary missing/delayed collector output can be reconstructed.
    await expect
      .poll(
        () => {
          const current = calls!.read();
          return current.completeLine && current.invocations === expected && current.dispatched === expected;
        },
        { timeout: 5000, intervals: [100, 250] },
      )
      .toBe(true);
    const until = Date.now() + 1000;
    while (Date.now() < until) {
      const current = calls!.read();
      expect(current.invocations).toBe(expected);
      expect(current.dispatched).toBe(expected);
      expect(sessionRequests).toBe(0);
      expect(documents).toBe(expectedDocuments);
      await delay(100);
    }
    outcomes.push({ phase, invocations: expected, dispatched: expected, documents });
  }
  try {
    const login = await context.request.post("/api/auth/login", {
      headers: { origin },
      data: { email: input.email, password: input.password },
      timeout: 15_000,
    });
    loggedIn = login.status() === 200;
    expect(loggedIn).toBe(true);
    await login.dispose();
    calls = new AccountCalls(log);
    phase = "initial Workspace document";
    await page.goto("/");
    // The fixture account owns a Project, so / opens its TeamSpace, whose Home lists it.
    await expect(page.getByRole("heading", { name: "Projects", exact: true })).toBeVisible();
    const documentOrigin = await page.evaluate(() => performance.timeOrigin);
    await observe(1, 1);
    phase = "account menu toggles";
    for (let index = 0; index < 3; index++) await accountMenu(page);
    await observe(1, 1);
    phase = "Project navigation from the sidebar";
    await page.getByTestId("workspace-project-list").locator(`a[href="/p/${input.projectId}/flow"]`).click();
    await expect(page).toHaveURL(new RegExp(`/p/${input.projectId}/flow$`));
    await expect(page.getByRole("navigation", { name: "Surface" })).toBeVisible();
    await accountMenu(page);
    await observe(1, 1);
    phase = "Tools navigation in the Builder bar";
    await page.getByRole("navigation", { name: "Surface" }).getByRole("link", { name: "Tools", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/p/${input.projectId}/tools`));
    await accountMenu(page);
    await observe(1, 1);
    phase = "Project details navigation";
    await page.getByRole("button", { name: /, switch project$/ }).click();
    await page.getByRole("link", { name: "Project details", exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/p/${input.projectId}$`));
    await expect(page.getByRole("link", { name: "Open the project", exact: true })).toBeVisible();
    await accountMenu(page);
    await observe(1, 1);
    expect(await page.evaluate(() => performance.timeOrigin)).toBe(documentOrigin);
    phase = "new Workspace document";
    await page.reload();
    await expect(page.getByRole("navigation", { name: "Surface" })).toBeVisible();
    await accountMenu(page);
    await observe(2, 2);
    expect(await page.evaluate(() => performance.timeOrigin)).not.toBe(documentOrigin);
    phase = "normal menu logout";
    await page.locator('summary[aria-label="Account"]').click();
    const [logout] = await Promise.all([
      page.waitForResponse(
        (response) => new URL(response.url()).pathname === "/api/auth/logout" && response.request().method() === "POST",
        { timeout: 15_000 },
      ),
      page.getByRole("button", { name: "Sign out", exact: true }).click(),
    ]);
    loggedOut = logout.status() === 200;
    expect(loggedOut).toBe(true);
    await expect(page).toHaveURL(/\/login$/);
    phase = "unauthenticated Workspace redirect";
    await page.goto("/");
    await expect(page).toHaveURL(/\/login$/);
    expect(sessionRequests).toBe(0);
    const final = calls.read();
    expect(final.invocations).toBe(2);
    expect(final.dispatched).toBe(2);
  } catch {
    // Never expose credentials, account/Session payloads, trace lines or SDK
    // error objects through Playwright reports, even when navigation fails.
    failures.push(new Error(`Real Workspace account check failed during ${phase}`));
  } finally {
    page.off("request", onRequest);
    try {
      calls?.close();
    } catch {
      failures.push(new Error("Studio gRPC trace reader cleanup failed"));
    }
    if (loggedIn && !loggedOut) {
      try {
        const response = await context.request.post("/api/auth/logout", {
          headers: { origin },
          data: {},
          timeout: 15_000,
        });
        loggedOut = response.status() === 200;
        await response.dispose();
      } catch {
        /* Report incomplete owned-login cleanup below. */
      }
      if (!loggedOut) failures.push(new Error("Secondary account navigation login cleanup is incomplete"));
    }
  }
  if (failures.length) throw new AggregateError(failures, "Actual Workspace account navigation failed");
  await testInfo.attach("workspace-account-call-counts", {
    body: JSON.stringify({
      outcomes,
      browserSessionRequests: sessionRequests,
      secondaryLoginRevoked: loggedOut,
      mechanism: "grpc-js 1.14.4 channel resolving/retrying call trace; real Studio/Go/Dex",
      observation:
        "complete isolated Studio trace; bounded one-second post-action observation; sensitive OAuth excluded",
    }),
    contentType: "application/json",
  });
});
