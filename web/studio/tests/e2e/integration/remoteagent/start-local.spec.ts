// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomUUID } from "node:crypto";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { signIn } from "@/tests/e2e/support/account";
import { array, object, string } from "@/tests/e2e/support/json-assert";
import { ownerProjectFixture } from "@/tests/e2e/support/private-files";
import { deleteProject, readProject, readSession } from "@/tests/e2e/support/project";
import { rpcAnswered, rpcPosted } from "@/tests/e2e/support/studio-api";
import type { RpcOutput } from "@/lib/shared/rpc-methods";
import { RemoteClient, inputText, wireInteger } from "./remote-client";

// The pairing code and Bridge bearer token remain in memory; recordings are disabled.
test.use({ trace: "off", screenshot: "off", video: "off" });

test("A locally chosen agent receives the creation message once, after remote activation", async ({
  page,
  context,
}) => {
  test.setTimeout(15 * 60_000);
  if (process.env.SUPERVERSE_STUDIO_MOCK === "true")
    throw new Error("Local-agent creation requires real lane dependencies");
  const fixture = ownerProjectFixture(
    string(process.env.SUPERVERSE_E2E_REMOTE_AGENT_FIXTURE, "Remote fixture path"),
    "Remote fixture",
  );
  await signIn(page, fixture);
  const teamSpaceId = (await readProject(page, fixture.projectId)).teamSpaceId;
  const firstMessage = `Build a local-agent Project ${randomUUID()}`;
  let createdProject = "";
  let modelUpdates = 0;
  page.on("request", (request) => {
    if (rpcPosted("UpdateSessionModel")(request)) modelUpdates++;
  });
  try {
    await page.goto(`/t/${teamSpaceId}/projects/start`);
    await page.getByRole("radio", { name: /My local agent/ }).check();
    await expect(page.getByRole("button", { name: "Model", exact: true })).toHaveCount(0);
    await page.getByRole("textbox", { name: "Describe a change" }).fill(firstMessage);
    const created = page.waitForResponse(
      (response) => rpcAnswered("CreateProject")(response) && response.status() !== 503,
    );
    await page.getByRole("button", { name: "Build", exact: true }).click();
    const response = await created;
    if (!response.ok()) throw new Error(`CreateProject returned HTTP ${response.status()}`);
    const creation = object(response.request().postDataJSON(), "Local CreateProject");
    expect(creation.initialMessage).toBeUndefined();
    expect(creation.messageId).toBeUndefined();
    expect(creation.model).toBeUndefined();
    createdProject = string((await response.json()).projectId, "Created Project ID");
    await expect
      .poll(async () => (await readProject(page, createdProject)).status, { timeout: 10 * 60_000, intervals: [2000] })
      .toBe("PROJECT_STATUS_READY");
    await expect(page.getByRole("checkbox", { name: "I understand these local-machine permissions." })).toBeVisible();
    await expect
      .poll(
        async () => {
          try {
            return (await readSession(page, createdProject)).status;
          } catch {
            return "starting";
          }
        },
        { timeout: 60_000, intervals: [1000] },
      )
      .toBe("SESSION_STATUS_ACTIVE");
    const session = await readSession(page, createdProject);
    expect(session.status).toBe("SESSION_STATUS_ACTIVE");
    expect(session.agent?.description?.executor?.mode).toBe("SESSION_EXECUTOR_MODE_PLATFORM");
    expect(session.agent?.queued).toHaveLength(0);
    const pairingResponse = page.waitForResponse(rpcAnswered("CreateRemoteAgentPairing"));
    await page.getByRole("checkbox", { name: "I understand these local-machine permissions." }).check();
    const paired = await pairingResponse;
    if (!paired.ok()) throw new Error(`Pairing returned HTTP ${paired.status()}`);
    const pairing: RpcOutput<"CreateRemoteAgentPairing"> = await paired.json();
    await expect.poll(async () => (await page.getByLabel("Pairing code").textContent()) === pairing.code).toBe(true);
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.getByRole("button", { name: "Copy code", exact: true }).click();
    expect((await page.evaluate(() => navigator.clipboard.readText())) === pairing.code).toBe(true);
    await expect(page.getByText(/Use macOS with Homebrew/)).toBeVisible();
    await expect(
      page.getByRole("region", { name: "Connect your local agent", exact: true }).getByText(/Orca|dexai-connect/),
    ).toHaveCount(0);
    await expect(page.getByText("brew install superdurable/tap/dexai", { exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: /Local agent help and troubleshooting/ })).toHaveAttribute(
      "href",
      "/help/local-agent",
    );
    await expect(page.getByText(/Codex opens its TUI after that brief/)).toBeVisible();
    await expect(page.getByText(`dexai connect --endpoint ${pairing.endpoint}`)).toBeVisible();
    const client = new RemoteClient(pairing.connectionId, pairing.endpoint);
    await client.pair(pairing);
    const firstSent = page.waitForRequest(rpcPosted("SendMessage"));
    const firstAnswered = page.waitForResponse(rpcAnswered("SendMessage"));
    const { fence, lastSequence } = await client.activateWithCursor("New local-agent Project");
    const request = object((await firstSent).postDataJSON(), "First local message");
    expect(request.projectId).toBe(createdProject);
    expect(request.text).toBe(firstMessage);
    if (!(await firstAnswered).ok()) throw new Error("The first local message was refused");
    await expect(page).toHaveURL(new RegExp(`/p/${createdProject}/flow(?:[?#]|$)`));
    await expect
      .poll(async () => (await readSession(page, createdProject)).agent?.description?.executor?.mode, {
        timeout: 30_000,
      })
      .toBe("SESSION_EXECUTOR_MODE_REMOTE");
    const delivered = await client.post("sync", {
      epoch: fence.epoch,
      after: Math.max(0, lastSequence - 20),
      stateDigest: "",
    });
    const entries = array(delivered.entries, "Remote Sync entries");
    const matches = entries.filter((entry) => inputText(entry) === firstMessage);
    expect(matches).toHaveLength(1);
    expect(wireInteger(object(matches[0], "First entry").sequence, "First entry sequence")).toBeGreaterThan(
      lastSequence,
    );
    expect(
      entries.filter(
        (entry) =>
          wireInteger(object(entry, "Brief entry").sequence, "Brief sequence") <= lastSequence &&
          inputText(entry) === firstMessage,
      ),
    ).toHaveLength(0);
    expect(modelUpdates).toBe(0);
    const through = wireInteger(delivered.examinedThrough, "Remote delivery cursor");
    const resumed = await client.post("sync", { epoch: fence.epoch, after: through, stateDigest: "" });
    expect(array(resumed.entries, "Resumed entries").filter((entry) => inputText(entry) === firstMessage)).toHaveLength(
      0,
    );
  } finally {
    if (createdProject) await deleteProject(page, createdProject);
  }
});
