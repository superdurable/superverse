// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Page, type Request, type Response, type TestInfo } from "@playwright/test";
import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { promisify } from "node:util";
import { isRecord } from "@/lib/shared/json";
import { openPreviewConfiguration } from "@/tests/e2e/support/configuration";

const execute = promisify(execFile);
type ProcessIdentity = { pid: number; group: number; started: string };

function identity(value: unknown): ProcessIdentity {
  if (
    !isRecord(value) ||
    !Number.isSafeInteger(value.pid) ||
    !Number.isSafeInteger(value.group) ||
    typeof value.started !== "string" ||
    Number(value.pid) <= 1 ||
    Number(value.group) <= 1
  )
    throw new Error("Native read fault requires a recorded process identity");
  return { pid: Number(value.pid), group: Number(value.group), started: value.started };
}

async function requireIdentity(processIdentity: ProcessIdentity) {
  const { stdout } = await execute("ps", ["-p", String(processIdentity.pid), "-o", "pid=,pgid=,lstart="], {
    timeout: 5000,
  });
  const parts = stdout.trim().match(/^(\d+)\s+(\d+)\s+(.+)$/);
  expect(parts && { pid: Number(parts[1]), group: Number(parts[2]), started: parts[3] }).toEqual(processIdentity);
}

/** Real BFF/gRPC faults in the existing case; opt in only with the exact owned Native run. */
export async function expectNativeConfigurationRecovery(page: Page, projectId: string, testInfo: TestInfo) {
  const runFile = process.env.SUPERVERSE_E2E_NATIVE_READ_FAULT_RUN_FILE;
  if (!runFile) return;
  const runId = process.env.SUPERVERSE_E2E_NATIVE_READ_FAULT_RUN_ID;
  const run: unknown = JSON.parse(await readFile(runFile, "utf8"));
  if (
    !isRecord(run) ||
    run.runId !== runId ||
    run.phase !== "ready" ||
    !Array.isArray(run.children) ||
    typeof run.binary !== "string" ||
    !runId
  )
    throw new Error("Native read fault requires the ready run's exact ID and run.json");
  const retained: unknown = JSON.parse(await readFile(join(dirname(dirname(dirname(runFile))), "owner.json"), "utf8"));
  if (!isRecord(retained) || retained.runId !== runId || retained.phase !== "ready")
    throw new Error("Native read fault run is no longer the retained owner");
  expect(new URL(page.url()).origin).toBe("http://localhost:21000");
  const ready = await page.request.get("http://127.0.0.1:26082/readyz");
  expect(ready.headers()["x-superverse-native-run-id"]).toBe(runId);
  expect(ready.ok()).toBe(true);
  const owner = identity(run.ownerProcess);
  const server = identity(run.children.find((child: unknown) => isRecord(child) && child.role === "server"));
  expect(server.group).toBe(owner.group);
  await requireIdentity(owner);
  await requireIdentity(server);
  const command = await execute("ps", ["-p", String(server.pid), "-o", "command="], { timeout: 5000 });
  expect(command.stdout.trim()).toBe(run.binary);

  let paused = false;
  const resume = async () => {
    if (!paused) return;
    await requireIdentity(server);
    process.kill(server.pid, "SIGCONT");
    paused = false;
  };
  const pauseDuring = async (check: () => Promise<void>) => {
    await requireIdentity(owner);
    await requireIdentity(server);
    process.kill(server.pid, "SIGSTOP");
    paused = true;
    let watchdogFailure: unknown;
    const watchdog = setTimeout(
      () =>
        void resume().catch((reason: unknown) => {
          watchdogFailure = reason;
        }),
      80_000,
    );
    try {
      await check();
      if (watchdogFailure) throw watchdogFailure;
    } finally {
      clearTimeout(watchdog);
      await resume();
    }
  };

  await openPreviewConfiguration(page, projectId);
  const surface = page.getByTestId("project-configuration-surface");
  const refresh = page.getByRole("button", { name: "Refresh configuration", exact: true });
  const configurationTab = page.getByRole("tab", { name: "2. Configuration", exact: true });
  const designTab = page.getByRole("tab", { name: "1. Design", exact: true });
  const unavailable = page.getByRole("heading", { name: "Configuration unavailable", exact: true });
  const retry = page.getByRole("button", { name: "Retry configuration", exact: true });
  const revision = surface.locator("span").first();
  const originalRevision = await revision.innerText();
  let phase = "automatic";
  const requests = new Map<Request, { phase: string; elapsed: number; status: number | string }>();
  const started = Date.now();
  const matches = (request: Request) =>
    new URL(request.url()).pathname === "/api/rpc/GetProjectConfiguration" &&
    request.postDataJSON()?.target?.projectId === projectId;
  const onRequest = (request: Request) => {
    if (matches(request)) {
      if (requests.size >= 20) throw new Error("Configuration read observation exceeded its bounded budget");
      requests.set(request, { phase, elapsed: Date.now() - started, status: "pending" });
    }
  };
  const onResponse = (response: Response) => {
    const entry = requests.get(response.request());
    if (entry) entry.status = response.status();
  };
  const onFailure = (request: Request) => {
    const entry = requests.get(request);
    if (entry) entry.status = "aborted";
  };
  const answered = (response: Response) => matches(response.request());
  page.on("request", onRequest);
  page.on("response", onResponse);
  page.on("requestfailed", onFailure);
  try {
    await pauseDuring(async () => {
      const failed = page.waitForResponse((response) => answered(response) && [503, 504].includes(response.status()), {
        timeout: 45_000,
      });
      await refresh.click();
      await expect(surface).toHaveAttribute("aria-busy", "true");
      await failed;
      await expect(surface).toBeVisible();
      await expect(revision).toHaveText(originalRevision);
      await expect(unavailable).toHaveCount(0);
      const recovered = page.waitForResponse((response) => answered(response) && response.ok(), { timeout: 25_000 });
      await resume();
      await recovered;
      await expect(surface).toHaveAttribute("aria-busy", "false");
      await expect(surface.getByRole("alert")).toHaveCount(0);
    });
    expect([...requests.values()].filter((entry) => entry.phase === "automatic").map((entry) => entry.status)).toEqual([
      504, 200,
    ]);

    phase = "exhaustion";
    await designTab.click();
    await expect(surface).toHaveCount(0);
    const budgetStarted = Date.now();
    await pauseDuring(async () => {
      await configurationTab.click();
      await expect(unavailable).toBeVisible({ timeout: 70_000 });
      await expect(retry).toBeEnabled();
      expect(Date.now() - budgetStarted).toBeGreaterThanOrEqual(55_000);
      expect(Date.now() - budgetStarted).toBeLessThan(70_000);
      const attempts = [...requests.values()].filter((entry) => entry.phase === "exhaustion");
      expect(attempts.length).toBeGreaterThanOrEqual(1);
      expect(attempts.length).toBeLessThanOrEqual(4);
      phase = "manual";
      await resume();
      await retry.click();
      await expect(surface).toBeVisible({ timeout: 25_000 });
      await expect(unavailable).toHaveCount(0);
      await expect(revision).toHaveText(originalRevision);
    });

    phase = "cancellation";
    await pauseDuring(async () => {
      const pending = page.waitForRequest(matches);
      await refresh.click();
      const request = await pending;
      const aborted = page.waitForEvent("requestfailed", { predicate: (failed) => failed === request });
      await designTab.click();
      await aborted;
      await expect(surface).toHaveCount(0);
      await resume();
      await page.waitForTimeout(3000);
      expect(
        [...requests.values()].filter((entry) => entry.phase === "cancellation").map((entry) => entry.status),
      ).toEqual(["aborted"]);
    });
    await configurationTab.click();
    await expect(surface).toBeVisible();
  } finally {
    await resume();
    page.off("request", onRequest);
    page.off("response", onResponse);
    page.off("requestfailed", onFailure);
    await testInfo.attach("configuration-read-recovery", {
      body: JSON.stringify({ runId, projectId, server, requests: [...requests.values()] }),
      contentType: "application/json",
    });
  }
}
