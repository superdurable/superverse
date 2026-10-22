// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomUUID } from "node:crypto";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { array, object, string } from "@/tests/e2e/support/json-assert";
import { readSession } from "@/tests/e2e/support/project";
import { rpcAnswered } from "@/tests/e2e/support/studio-api";
import { openExecutorMenu, runRemoteCase } from "./remote-fixture";
import { wireInteger } from "./remote-client";

// The external client supplies user behavior through the actual remote listener.
// Studio, BFF, gRPC, SQL and Dex remain the application under test.
test.use({ trace: "off", screenshot: "off", video: "off" });
test("Studio pairs a remote executor, renders its reply and takes over with the observed epoch", async ({ page }) => {
  test.setTimeout(180_000);
  await runRemoteCase(page, async (scope) => {
    const projectId = scope.fixture.projectId;
    const executor = page.locator("[data-session-executor]");
    await expect(executor).toHaveAttribute("data-session-executor", "platform");
    const client = await scope.connect();
    const current = await client.activate("Studio integration host");
    const reply = `A remote reply from the actual HTTP listener (${randomUUID()}).`;
    await expect(executor).toHaveAttribute("data-session-executor", "remote");
    await expect(executor.getByRole("button", { name: /^Agent:/ })).toHaveAccessibleName(
      /^Agent: Local agent · online · connected by /,
    );
    await client.post("report", {
      ...current,
      sequence: 1,
      events: [{ kind: "assistant_message", text: reply }],
    });
    const replyArticle = page.locator("article").filter({ hasText: reply });
    await expect(replyArticle.getByText(reply, { exact: true })).toBeVisible();
    await expect(replyArticle.getByText("Local agent · Remote reply", { exact: true })).toBeVisible();
    const authority = (await readSession(page, projectId)).agent?.description?.executor;
    if (authority?.remote?.connectionId !== client.connectionId) throw new Error("Reported remote lost authority");
    const menu = await openExecutorMenu(page);
    const switching = page.waitForResponse(rpcAnswered("SetSessionExecutor"));
    await menu.getByRole("button", { name: "Use the Platform agent", exact: true }).click();
    const switched = await switching;
    if (!switched.ok()) throw new Error(`Executor takeover returned HTTP ${switched.status()}`);
    const command = object(switched.request().postDataJSON(), "Executor takeover request");
    expect(command.basisEpoch).toBe(authority.epoch);
    expect(typeof command.basisEpoch).toBe("string");
    await expect(executor).toHaveAttribute("data-session-executor", "platform");
    await expect(
      page.locator("[data-executor-changed]").filter({ hasText: "Switched to the Platform agent" }).last(),
    ).toBeVisible();
    await expect(page.locator("[data-remote-idle]")).toContainText("connected and idle");
    await expect(executor).toContainText("local idle");
    await executor.getByRole("button", { name: /^Agent:/ }).click();
    await expect(page.getByRole("button", { name: "Claude Sonnet 5.5" })).toBeVisible();
    await page.keyboard.press("Escape");
    const idle = await client.post("sync", { epoch: current.epoch, after: 0, stateDigest: "" });
    expect(idle.idle).toBe(true);
    const refused = await client.post("report", { ...current, sequence: 2, turnCompleted: true }, 409);
    expect(refused.code).toBe("executor_changed");
    const localMenu = await openExecutorMenu(page);
    await expect(localMenu.getByText(/Connected · idle while the Platform agent is active/)).toBeVisible();
    const resuming = page.waitForResponse(rpcAnswered("SetSessionExecutor"));
    await localMenu.getByRole("button", { name: "Use the local agent", exact: true }).click();
    expect((await resuming).ok()).toBe(true);
    await expect(executor).toHaveAttribute("data-session-executor", "remote");
    await expect(
      page.locator("[data-executor-changed]").filter({ hasText: "Switched to the local agent" }).last(),
    ).toBeVisible();
    const resumed = await client.activate("Studio integration host");
    expect(BigInt(resumed.epoch)).toBeGreaterThan(BigInt(current.epoch));
    const resumedReply = `Same connection resumed (${randomUUID()}).`;
    await client.post("report", {
      ...resumed,
      sequence: 1,
      events: [{ kind: "assistant_message", text: resumedReply }],
    });
    const resumedTurn = page
      .getByRole("region", { name: "Conversation turn", exact: true })
      .filter({ hasText: resumedReply });
    await expect(resumedTurn.getByText("Switched to the local agent", { exact: true })).toBeVisible();
    await expect(resumedTurn.getByText(reply, { exact: true })).toHaveCount(0);
    const stale = await client.post("report", { ...current, sequence: 2, turnCompleted: true }, 409);
    expect(stale.code).toBe("executor_changed");
  });
});

test("a command notification starts remote work after idle without inheriting the preceding turn", async ({ page }) => {
  test.setTimeout(600_000);
  await runRemoteCase(page, async (scope) => {
    const client = await scope.connect();
    const fence = await client.activate("Notification timing host");
    const before = `Before the notification ${randomUUID()}`;
    const after = `After the notification ${randomUUID()}`;
    await client.post("report", { ...fence, sequence: 1, events: [{ kind: "assistant_message", text: before }] });
    await client.post("report", {
      ...fence,
      sequence: 2,
      toolCall: {
        name: "bash",
        arguments: {
          command: "sleep 20; printf 'notification timing command completed\\n'",
          description: "Finish a real background command after the remote turn ends",
          run_in_background: true,
        },
      },
    });
    let cursor = 0;
    const sync = async () => {
      const response = await client.post("sync", { epoch: fence.epoch, after: cursor, stateDigest: "" });
      cursor = wireInteger(response.examinedThrough, "Notification delivery cursor");
      return array(response.entries, "Actual remote entries").map((entry) => object(entry, "Actual remote entry"));
    };
    const result = (entries: Awaited<ReturnType<typeof sync>>, sequence: number) =>
      entries
        .flatMap((entry) => array(entry.toolResults ?? [], "Actual tool results"))
        .some((value) => {
          const item = object(value, "Actual tool result");
          if (item.callId !== `remote-${fence.epoch}-${sequence}`) return false;
          expect(item.isError ?? false).toBe(false);
          return true;
        });
    await expect.poll(async () => result(await sync(), 2), { timeout: 300_000 }).toBe(true);
    await client.post("report", { ...fence, sequence: 3, turnCompleted: true });
    await expect
      .poll(async () => (await readSession(page, scope.fixture.projectId)).agent?.description?.status)
      .toBe("STATUS_WAITING_FOR_MESSAGE");
    // The real BackgroundCommandFlow produces this; no notification, clock,
    // API response or dependency is replaced by the browser test.
    await expect
      .poll(async () => (await sync()).some((entry) => entry.kind === "notification"), { timeout: 90_000 })
      .toBe(true);
    await client.post("report", {
      ...fence,
      sequence: 4,
      toolCall: {
        name: "bash",
        arguments: { command: "printf 'notification reply work\\n'", description: "Handle the delivered notification" },
      },
    });
    await expect.poll(async () => result(await sync(), 4), { timeout: 60_000 }).toBe(true);
    await client.post("report", { ...fence, sequence: 5, events: [{ kind: "assistant_message", text: after }] });
    await client.post("report", { ...fence, sequence: 6, turnCompleted: true });
    const turn = page.getByRole("region", { name: "Conversation turn", exact: true }).filter({ hasText: after });
    await expect(turn.getByText("Background task finished", { exact: true })).toBeVisible();
    await expect(turn.getByText(before, { exact: true })).toHaveCount(0);
    await expect(turn.getByRole("button", { name: /^Worked.* · 1 step$/ })).toBeVisible();
    const history = (await readSession(page, scope.fixture.projectId)).agent?.history?.messages ?? [];
    const reply = history.find((entry) => entry.message?.content === after);
    const notice = history.findLast((entry) => entry.message?.role === "ROLE_NOTIFICATION");
    expect(reply?.sequence).toBeDefined();
    expect(notice?.sequence).toBeDefined();
    expect(BigInt(string(reply?.sequence, "Reply sequence"))).toBeGreaterThan(
      BigInt(string(notice?.sequence, "Notification sequence")),
    );
    const seconds = Math.floor(
      (Date.parse(string(reply?.message?.createdAt, "Reply consumption time")) -
        Date.parse(string(notice?.message?.createdAt, "Notification consumption time"))) /
        1000,
    );
    expect(seconds).toBeGreaterThanOrEqual(0);
    const duration =
      seconds < 60 ? `${seconds} s` : `${Math.floor(seconds / 60)} min${seconds % 60 ? ` ${seconds % 60} s` : ""}`;
    await expect(
      turn.getByRole("button", {
        name: seconds ? `Worked ${duration} · 1 step` : "Worked for a moment · 1 step",
        exact: true,
      }),
    ).toBeVisible();
  });
});
