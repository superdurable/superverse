// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomUUID } from "node:crypto";
import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";
import { array, object, string } from "@/tests/e2e/support/json-assert";
import { deleteProject, readProject, readSession } from "@/tests/e2e/support/project";
import { rpcAnswered, rpcPosted } from "@/tests/e2e/support/studio-api";
import type { RpcOutput } from "@/lib/shared/rpc-methods";
import { RemoteClient, inputText, wireInteger } from "./remote-client";

// The pairing code and Bridge bearer token remain in memory; recordings are disabled.
test.use({ trace: "off", screenshot: "off", video: "off" });

for (const { loseResponse, manual, label } of [
  { loseResponse: false, manual: false, label: "after remote activation" },
  { loseResponse: false, manual: true, label: "when sent manually after clearing the draft" },
  { loseResponse: true, manual: false, label: "despite a lost response" },
]) {
  test(`A locally chosen agent receives the creation message once ${label}`, async ({ page, context }) => {
    test.setTimeout(15 * 60_000);
    if (process.env.SUPERVERSE_STUDIO_MOCK === "true")
      throw new Error("Local-agent creation requires real lane dependencies");
    await register(page);
    const teamSpaceId = await createTeamSpace(page);
    const originalDescription = `Build a local-agent Project ${randomUUID()}`;
    const firstMessage = `${originalDescription} with an editable description`;
    let createdProject = "";
    let modelUpdates = 0;
    let messageSends = 0;
    let pairingRequests = 0;
    page.on("request", (request) => {
      if (rpcPosted("UpdateSessionModel")(request)) modelUpdates++;
      if (rpcPosted("SendMessage")(request)) messageSends++;
      if (rpcPosted("CreateRemoteAgentPairing")(request)) pairingRequests++;
    });
    try {
      await page.goto(`/t/${teamSpaceId}/projects/start`);
      await page.getByRole("radio", { name: /My local agent/ }).check();
      await expect(page.getByRole("button", { name: "Model", exact: true })).toHaveCount(0);
      await page.getByRole("textbox", { name: "Describe a change" }).fill(originalDescription);
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
      await expect(page).toHaveURL(new RegExp(`/p/${createdProject}/flow(?:[?#]|$)`));
      await expect
        .poll(async () => (await readProject(page, createdProject)).status, { timeout: 10 * 60_000, intervals: [2000] })
        .toBe("PROJECT_STATUS_READY");
      // The Project opens before pairing; nothing submits the description to the platform agent.
      await expect(page).toHaveURL(new RegExp(`/p/${createdProject}/flow(?:[?#]|$)`));
      const composer = page.getByRole("textbox", { name: "Message the agent" });
      await expect(composer).toHaveValue(originalDescription);
      await expect(composer).toBeEditable();
      await expect(page.getByRole("button", { name: "Send", exact: true })).toBeDisabled();
      await expect(page.getByText("Connect your local agent to send. Your draft stays editable.")).toBeVisible();
      await composer.fill(firstMessage);
      await page.reload();
      await expect(composer).toHaveValue(firstMessage);
      await expect(composer).toBeEditable();
      await expect(page.getByRole("button", { name: "Send", exact: true })).toBeDisabled();
      await expect(page.getByRole("checkbox", { name: "I understand these local-machine permissions." })).toBeVisible();
      expect(messageSends).toBe(0);
      expect(pairingRequests).toBe(0);
      await expect(page.locator('[data-startup-step="reading"]')).toHaveCount(0);
      await expect(
        page.getByText(
          "Acknowledge the local-machine permissions above to show your pairing code and connection command here.",
        ),
      ).toBeVisible();
      await page.screenshot({ path: test.info().outputPath("local-project-editable-draft.png") });
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
      if (manual) await composer.fill("");
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
      if (loseResponse) {
        await page.route("**/api/rpc/SendMessage", async (route) => {
          const admitted = await route.fetch();
          if (!admitted.ok()) throw new Error(`SendMessage returned HTTP ${admitted.status()}`);
          await route.abort("failed");
          await admitted.dispose();
        });
      }
      const firstAnswered = loseResponse
        ? page.waitForEvent("requestfailed", { predicate: rpcPosted("SendMessage") }).then(() => undefined)
        : page.waitForResponse(rpcAnswered("SendMessage")).then((result) => {
            if (!result.ok()) throw new Error("The first local message was refused");
          });
      const { fence, lastSequence } = await client.activateWithCursor("New local-agent Project");
      if (manual) {
        await expect(page.getByText("Local agent · not connected")).toHaveCount(0);
        expect(messageSends).toBe(0);
        await composer.fill(firstMessage);
        await page.getByRole("button", { name: "Send", exact: true }).click();
      }
      const request = object((await firstSent).postDataJSON(), "First local message");
      expect(request.projectId).toBe(createdProject);
      expect(request.text).toBe(firstMessage);
      await firstAnswered;
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
      if (loseResponse) {
        const recoveryDraft = `${firstMessage} — keep this follow-up draft`;
        await composer.fill(recoveryDraft);
        await page.reload();
        await expect(page.getByRole("button", { name: "Send", exact: true })).toBeDisabled();
        await expect(composer).toHaveValue(recoveryDraft);
        await expect(
          page.getByRole("button", { name: "I checked the conversation; enable manual sending" }),
        ).toBeVisible();
        await page.screenshot({ path: test.info().outputPath("local-project-uncertain-send.png") });
        await page.getByRole("button", { name: "I checked the conversation; enable manual sending" }).click();
        await expect(composer).toHaveValue(recoveryDraft);
        await expect(page.getByRole("button", { name: "Send", exact: true })).toBeEnabled();
      } else {
        await expect(composer).toHaveValue("");
        await page.reload();
        await expect(composer).toHaveValue("");
      }
      expect(messageSends).toBe(1);
      expect(modelUpdates).toBe(0);
      await page.screenshot({ path: test.info().outputPath("local-project-connected.png") });
      const through = wireInteger(delivered.examinedThrough, "Remote delivery cursor");
      const resumed = await client.post("sync", { epoch: fence.epoch, after: through, stateDigest: "" });
      expect(
        array(resumed.entries, "Resumed entries").filter((entry) => inputText(entry) === firstMessage),
      ).toHaveLength(0);
    } finally {
      if (createdProject) await deleteProject(page, createdProject);
    }
  });
}
