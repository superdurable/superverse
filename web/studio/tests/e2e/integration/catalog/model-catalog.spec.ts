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
import { readProject, readSession } from "@/tests/e2e/support/project";
import { rpc, rpcAnswered, rpcResult } from "@/tests/e2e/support/studio-api";
import { sendMockInput, waitForFirstMockInput } from "@/tests/e2e/support/mock/mock-composer";

// Every command crosses the real Studio BFF, gRPC and Dex. Only the eventual model reply is mock.
test("catalog mappings preserve a stored Claude selection and refuse unavailable providers", async ({ page }) => {
  test.setTimeout(20 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  const created = await rpcResult(page.request, "CreateProject", {
    teamSpaceId,
    requestId: randomUUID(),
    name: "Catalog compatibility",
    managedRepository: {},
  });
  const projectId = created.projectId;
  let failure: unknown;
  try {
    await expect
      .poll(
        async () => {
          try {
            return (await readSession(page, projectId)).agent?.description?.status;
          } catch (reason) {
            if (reason && typeof reason === "object" && "status" in reason && reason.status === 404) return null;
            throw reason;
          }
        },
        {
          timeout: 10 * 60_000,
          intervals: [1000],
        },
      )
      .toBe("STATUS_WAITING_FOR_MESSAGE");
    const initial = (await readSession(page, projectId)).agent!.description!;
    const revision = initial.modelRevision;
    // New enum values decode correctly but cannot become selected, even on a key-bearing server.
    const refused = await rpc(page.request, "UpdateSessionModel", {
      projectId,
      model: "AGENT_MODEL_GPT_6_ASTRA",
      expectedRevision: revision,
    });
    expect(refused.status()).toBe(409);
    expect((await readSession(page, projectId)).agent!.description!.modelRevision).toBe(revision);
    await page.goto(`/p/${projectId}/flow`);
    await page.getByRole("button", { name: /^Agent:/ }).click();
    const picker = page.getByRole("dialog", { name: "Choose agent" });
    await expect(picker.getByRole("button", { name: /GPT-6 Astra/ })).toHaveCount(0);
    const selected = page.waitForResponse(rpcAnswered("UpdateSessionModel"));
    await picker.getByRole("button", { name: /Claude Sonnet 5.5/ }).click();
    expect((await selected).ok()).toBe(true);
    await expect
      .poll(async () => (await readSession(page, projectId)).agent!.description!.model)
      .toBe("AGENT_MODEL_SONNET_5_5");
    // Reload forces a new BFF/gRPC read of the durable historical Claude value.
    await page.reload();
    await expect(page.getByRole("button", { name: /Agent:.*Claude Sonnet 5.5/ })).toBeVisible();
    await page.getByRole("button", { name: /^Agent:/ }).click();
    const changed = page.waitForResponse(rpcAnswered("UpdateSessionModel"));
    await page.getByRole("dialog", { name: "Choose agent" }).getByRole("button", { name: /^Mock/ }).click();
    expect((await changed).ok()).toBe(true);
    await expect
      .poll(async () => (await readSession(page, projectId)).agent!.description!.model)
      .toBe("AGENT_MODEL_MOCK");
    await sendMockInput(page, projectId, "explain", 1);
    await waitForFirstMockInput(page, projectId, "explain");
    const final = (await readSession(page, projectId)).agent!.description!;
    expect(final.model).toBe("AGENT_MODEL_MOCK");
    expect(final.contextModel).toBe("AGENT_MODEL_MOCK");
  } catch (reason) {
    failure = reason;
    throw reason;
  } finally {
    try {
      const project = await readProject(page, projectId);
      await rpcResult(page.request, "DeleteProject", {
        projectId,
        requestId: randomUUID(),
        expectedRevision: project.revision,
      });
      // Terminal deletion revokes Project access; the real API returns PermissionDenied (403).
      await expect
        .poll(async () => (await rpc(page.request, "GetProject", { projectId })).status(), {
          timeout: 5 * 60_000,
          intervals: [1000],
        })
        .toBe(403);
    } catch (cleanup) {
      if (failure)
        throw new AggregateError([failure, cleanup], "Catalog verification and owned Project cleanup failed");
      throw cleanup;
    }
  }
});
