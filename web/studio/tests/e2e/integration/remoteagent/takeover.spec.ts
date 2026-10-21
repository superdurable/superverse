// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { object } from "@/tests/e2e/support/json-assert";
import { readSession } from "@/tests/e2e/support/project";
import { rpcAnswered, rpcResult } from "@/tests/e2e/support/studio-api";
import { openExecutorMenu, runRemoteCase } from "./remote-fixture";

test.use({ trace: "off", screenshot: "off", video: "off" });
test("A distinct Builder takes over remotely; replaced-valid is 409 and platform-revoked is 401", async ({
  page,
  browser,
}) => {
  test.setTimeout(240_000);
  await runRemoteCase(page, async (scope) => {
    const projectId = scope.fixture.projectId;
    const builder = await scope.addBuilder(browser);
    const first = await scope.connect();
    const oldFence = await first.activate("First Builder connection");
    await expect
      .poll(async () => (await readSession(page, projectId)).agent?.description?.executor?.remote?.connectionId, {
        timeout: 60_000,
      })
      .toBe(first.connectionId);
    // Pair after the first activation: the server captures the actual current basis.
    const second = await scope.connect(builder.page);
    const nextFence = await second.activate("Second Builder connection");
    expect(nextFence.epoch).toBeGreaterThan(oldFence.epoch);
    await expect
      .poll(async () => (await readSession(page, projectId)).agent?.description?.executor?.remote?.connectionId, {
        timeout: 60_000,
      })
      .toBe(second.connectionId);
    const authority = (await readSession(page, projectId)).agent?.description?.executor;
    expect(authority?.remote?.owner?.accountId).toBe(builder.accountId);
    expect(authority?.remote?.owner?.email).toBe(builder.email);
    await expect
      .poll(() => page.locator("[data-session-executor] > button").getAttribute("title"))
      .toContain(`connected by ${builder.email}`);
    const saved = await rpcResult(page.request, "ListRemoteAgentConnections", { projectId });
    expect(saved.connections.find((row) => row.connectionId === first.connectionId)?.status).toBe(
      "REMOTE_AGENT_CONNECTION_STATUS_PAIRED",
    );
    const replaced = await first.post("report", { ...oldFence, sequence: 1, turnCompleted: true }, 409);
    expect(replaced.code).toBe("executor_changed");
    const menu = await openExecutorMenu(page);
    const switched = page.waitForResponse(rpcAnswered("SetSessionExecutor"));
    await menu.getByRole("button", { name: "Use the Platform agent", exact: true }).click();
    const response = await switched;
    if (!response.ok()) throw new Error(`Owner platform switch returned HTTP ${response.status()}`);
    expect(object(response.request().postDataJSON(), "Owner switch").basisEpoch).toBe(authority?.epoch);
    await expect(page.locator("[data-session-executor]")).toHaveAttribute("data-session-executor", "platform");
    const revoked = await second.post("report", { ...nextFence, sequence: 1, turnCompleted: true }, 401);
    expect(revoked.code).toBe("unauthorized");
    // The older connection was replaced, not revoked by somebody else's switch.
    expect((await first.post("report", { ...oldFence, sequence: 1, turnCompleted: true }, 409)).code).toBe(
      "executor_changed",
    );
  });
});
