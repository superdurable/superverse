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
import { rpcAnswered, rpcPosted } from "@/tests/e2e/support/studio-api";
import { timestampToISO } from "@/lib/shared/protobuf";
import { inputText, wireInteger } from "./remote-client";
import { runRemoteCase } from "./remote-fixture";

test.use({ trace: "off", screenshot: "off", video: "off" });
test("Real last-seen expiry keeps remote authority; reconnect delivers the ordinary queued message once", async ({
  page,
}) => {
  test.setTimeout(300_000);
  await runRemoteCase(page, async (scope) => {
    const projectId = scope.fixture.projectId;
    const client = await scope.connect();
    const fence = await client.activate("Offline reconnect browser case");
    // A real public remote report opens a turn. Ordinary input must stay queued
    // until that remote turn ends; SQL liveness never controls its consumption.
    await client.post("report", {
      ...fence,
      sequence: 1,
      events: [{ kind: "assistant_message", text: "Remote work before reconnect." }],
    });
    await expect
      .poll(async () => (await readSession(page, projectId)).agent?.description?.status, { timeout: 60_000 })
      .toBe("STATUS_CALLING_MODEL");
    const baseline = await client.post("sync", { epoch: fence.epoch, after: 0, stateDigest: "" });
    const cursor = wireInteger(baseline.examinedThrough, "Delivery cursor");
    const initial = await readSession(page, projectId);
    const authority = initial.agent?.description?.executor;
    if (!authority?.remote || authority.remote.connectionId !== client.connectionId)
      throw new Error("Activated connection is not the canonical executor");
    const seen = timestampToISO(authority.remote.lastSeenAt);
    if (!seen || !Number.isFinite(Date.parse(seen))) throw new Error("Canonical SQL last-seen is unavailable");
    const menu = page.locator("[data-session-executor]");
    await expect(menu).toHaveAttribute("data-session-executor", "remote");
    await expect(page.getByLabel("Model", { exact: true })).toHaveCount(0);
    // No bearer request, heartbeats, clock replacement or projection mutation
    // occurs in this interval. GetSession reads the real SQL-derived timestamp.
    await expect
      .poll(
        async () => {
          const snapshot = await readSession(page, projectId);
          const current = snapshot.agent?.description?.executor;
          if (current?.epoch !== authority.epoch || current.remote?.connectionId !== client.connectionId)
            throw new Error("Executor changed while waiting for actual last-seen expiry");
          if (timestampToISO(current.remote.lastSeenAt) !== seen)
            throw new Error("Unexpected Bridge activity refreshed last-seen");
          return Date.now() - Date.parse(seen);
        },
        { timeout: 120_000, intervals: [5000] },
      )
      .toBeGreaterThan(90_000);
    await expect(page.locator("[data-remote-offline]")).toBeVisible({ timeout: 20_000 });
    await expect(menu).toHaveAttribute("data-session-executor", "remote");
    await expect(page.getByLabel("Model", { exact: true })).toHaveCount(0);
    const text = `Offline ordinary input ${randomUUID()}`;
    const posted = page.waitForRequest(rpcPosted("SendMessage"));
    const answered = page.waitForResponse(rpcAnswered("SendMessage"));
    await page.getByRole("textbox", { name: "Message the agent", exact: true }).fill(text);
    await page.getByRole("button", { name: "Send", exact: true }).click();
    const command = object((await posted).postDataJSON(), "Ordinary send request");
    const messageId = string(command.messageId, "Queued message ID");
    scope.ownedMessages.add(messageId);
    if (!(await answered).ok()) throw new Error("Ordinary offline SendMessage was refused");
    await expect
      .poll(
        async () =>
          (await readSession(page, projectId)).agent?.queued.filter((message) => message.messageId === messageId)
            .length,
        { timeout: 30_000 },
      )
      .toBe(1);
    const offline = await readSession(page, projectId);
    expect(offline.agent?.queued.find((message) => message.messageId === messageId)?.value?.content).toBe(text);
    expect(offline.agent?.description?.executor?.epoch).toBe(authority.epoch);
    // Reconnect uses the same credential/epoch and real sync; no activation or fallback.
    const reconnect = await client.post("sync", { epoch: fence.epoch, after: cursor, stateDigest: "" });
    expect(reconnect.epoch).toBe(fence.epoch);
    await expect
      .poll(
        async () =>
          timestampToISO((await readSession(page, projectId)).agent?.description?.executor?.remote?.lastSeenAt),
        { timeout: 30_000 },
      )
      .not.toBe(seen);
    await page.reload();
    await expect(page.locator("[data-remote-offline]")).toHaveCount(0);
    await expect(page.getByLabel("Model", { exact: true })).toHaveCount(0);
    await client.post("report", { ...fence, sequence: 2, turnCompleted: true });
    await expect
      .poll(
        async () =>
          (await readSession(page, projectId)).agent?.queued.some((message) => message.messageId === messageId),
        { timeout: 60_000 },
      )
      .toBe(false);
    const delivery = await client.post("sync", { epoch: fence.epoch, after: cursor, stateDigest: "" });
    const entries = array(delivery.entries, "Remote delivered entries");
    expect(entries.filter((entry) => inputText(entry) === text).length).toBe(1);
    const through = wireInteger(delivery.examinedThrough, "Advanced delivery cursor");
    expect(through).toBeGreaterThan(cursor);
    const resumed = await client.post("sync", { epoch: fence.epoch, after: through, stateDigest: "" });
    expect(array(resumed.entries, "Resumed delivery entries").filter((entry) => inputText(entry) === text).length).toBe(
      0,
    );
    await expect(page.getByText(text, { exact: true })).toHaveCount(1);
    await client.post("report", {
      ...fence,
      sequence: 3,
      events: [{ kind: "assistant_message", text: "Received the queued message once." }],
    });
    await client.post("report", { ...fence, sequence: 4, turnCompleted: true });
    await expect
      .poll(async () => (await readSession(page, projectId)).agent?.description?.status, { timeout: 60_000 })
      .toBe("STATUS_WAITING_FOR_MESSAGE");
    expect((await readSession(page, projectId)).agent?.description?.executor?.remote?.connectionId).toBe(
      client.connectionId,
    );
  });
});
