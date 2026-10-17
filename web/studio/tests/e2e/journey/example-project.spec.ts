// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import { choosesMockModel, runsMockModel, sendMockMessage } from "@/tests/e2e/support/mock/mock-composer";
import { MOCK_MODEL_OPTION, readProject, readSession } from "@/tests/e2e/support/project";
import { chooseOption, rpcAnswered } from "@/tests/e2e/support/studio-api";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";

const EXAMPLE_ID = "slack-thread-approval";
/** What the person types on the start page, which ranks the examples and then waits as the change to send. */
const DRAFT = "Approve Slack thread replies, and also tell the approver's manager in Slack.";

// A Project opened from the server's embedded slack-thread-approval example. Before the person types, the
// start page's grid offers it; while they type, the grid ranks the built examples closest to the description
// and marks Slack thread approval first. One click on Open example creates the Project from the example as
// it is: the creation carries the example and no message, and the Session starts with the example's card
// recorded as version 1 and built at the setup commit, its greeting and its processes, with no model call,
// then deploys the setup commit's Preview. The Flow tab shows only the flow, with no overview; the chat opens
// with the greeting and the "Recorded version 1" line, and what the person typed waits in its composer,
// unsent. On a lane that offers the mock model the example opens on it, and the example-change scenario
// records version 2 and pushes it.
test("a built example opens as it is in one click, ranked by the description, which waits as a draft", async ({
  page,
}) => {
  test.setTimeout(40 * 60_000);
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  const mock = await choosesMockModel(page);
  await page.goto(`/t/${teamSpaceId}/projects/start`);
  await expect(page.getByRole("heading", { name: "What process should Dex run?" })).toBeVisible();
  const starter = page.getByRole("region", { name: "Start from an example", exact: true });
  await expect(starter.getByRole("article", { name: "Slack thread approval", exact: true })).toBeVisible({
    timeout: 60_000,
  });
  await expect(page.getByRole("button", { name: "Browse all examples", exact: true })).toBeVisible();
  const composer = page.getByRole("textbox", { name: "Describe a change", exact: true });
  await composer.fill(DRAFT);
  const ranked = page.getByRole("region", { name: "Closest to your description", exact: true });
  await expect(ranked.getByTestId("start-example").first()).toHaveAttribute("aria-label", "Slack thread approval");
  const closest = ranked.locator('[data-closest="true"]');
  await expect(closest).toHaveAttribute("aria-label", "Slack thread approval");
  if (mock) {
    // A lane without a Claude key opens the example on the mock model, which the Superverse agent's model choice
    // offers; the description stays, and so does the rank.
    await chooseOption(page.getByRole("button", { name: "Model", exact: true }), MOCK_MODEL_OPTION);
    await expect(closest).toHaveAttribute("aria-label", "Slack thread approval");
  }
  const answered = page.waitForResponse(
    (response) => rpcAnswered("CreateProject")(response) && response.status() !== 503,
  );
  await closest.getByRole("button", { name: "Open example", exact: true }).click();
  const response = await answered;
  expect(response.ok()).toBe(true);
  const request: RpcInput<"CreateProject"> = response.request().postDataJSON();
  expect(request.managedRepository?.example?.exampleId).toBe(EXAMPLE_ID);
  // The example opens as it is: the description is not sent.
  expect(request.initialMessage ?? "").toBe("");
  expect(request.messageId ?? "").toBe("");
  const { projectId }: RpcOutput<"CreateProject"> = await response.json();
  await expect(page).toHaveURL(/\/p\/[a-z0-9]+\/flow(\?|$)/, { timeout: 60_000 });
  await expect
    .poll(async () => (await readProject(page, projectId)).status, { timeout: 10 * 60_000, intervals: [2000] })
    .toBe("PROJECT_STATUS_READY");
  const project = await readProject(page, projectId);
  expect(project.example?.exampleId).toBe(EXAMPLE_ID);
  expect(project.templateRevision).toBe(`app-template/${EXAMPLE_ID}@${project.example?.tree}`);

  // The brief is version 1 with R1 to R3, the conversation opens with the greeting, and no model replied.
  await expect
    .poll(
      async () => Number((await readSession(page, projectId)).agent?.description?.confirmedRequirements?.revision ?? 0),
      {
        timeout: 5 * 60_000,
        intervals: [2000],
      },
    )
    .toBe(1);
  const started = await readSession(page, projectId);
  const features = started.agent?.description?.confirmedRequirements?.spec?.features.map(({ id }) => id);
  expect(features).toEqual(["R1", "R2", "R3"]);
  expect(started.agent?.description?.confirmedRequirements?.spec?.designBuilt).toBe(true);
  expect(started.agent?.history?.messages.some(({ message }) => message?.role === "ROLE_ASSISTANT")).toBe(false);
  expect(started.agent?.history?.messages.some(({ message }) => message?.role === "ROLE_USER")).toBe(false);
  await page.goto(`/p/${projectId}/flow?flowType=SlackThreadApprovalFlow`);
  const conversation = page.getByRole("region", { name: "Conversation", exact: true });
  await expect(conversation).toContainText("This project starts from the Slack thread approval example");
  // The chat shows version 1 once, beside the greeting, as one line that opens to the card.
  const first = conversation.locator('[data-recorded-version="1"]');
  await expect(first.locator("summary")).toHaveText("Recorded version 1: Slack thread approval");
  await first.locator("summary").click();
  await expect(first.getByRole("region", { name: "Features", exact: true })).toContainText("R3");
  // What the person typed waits as the chat's unsent draft: in the composer's box, or, on the mock model, whose
  // composer takes scenario inputs, in the Project's stored draft.
  if (mock) {
    const key = `superversev2.composer-draft.v1:${projectId}`;
    await expect.poll(() => page.evaluate((stored) => localStorage.getItem(stored), key)).toBe(DRAFT);
  } else {
    await expect(page.getByRole("textbox", { name: "Message the agent", exact: true })).toHaveValue(DRAFT);
  }
  // The card is built, so the Flow tab shows only the flow, with no overview: the example's design until the
  // Preview's processes are built from the setup commit, then those processes, both with the Reply stage.
  await expect(
    page.locator('[data-design-phase="built"]').or(page.getByRole("region", { name: "Flow definition" })),
  ).toBeVisible({ timeout: 5 * 60_000 });
  await expect(page.locator("[data-brief-state]")).toHaveCount(0);
  await expect(page.getByTestId("design-status")).toHaveCount(0);
  await expect(page.getByRole("button", { name: /^Reply, 3 steps\b/ })).toBeVisible({ timeout: 5 * 60_000 });

  // The Session deploys the setup commit's Preview once; the Slack connection is a configuration gap.
  await expect
    .poll(
      async () => {
        const session = await readSession(page, projectId);
        const prepared = session.preview?.preparedSource;
        return prepared?.status === "PREVIEW_SOURCE_STATUS_READY" && prepared.sourceCommit === session.headCommit;
      },
      { timeout: 20 * 60_000, intervals: [5000] },
    )
    .toBe(true);

  // The draft was never sent.
  const waiting = await readSession(page, projectId);
  expect(waiting.agent?.history?.messages.some(({ message }) => message?.role === "ROLE_USER")).toBe(false);

  if (!mock) return;
  // The Session opened on the mock model and has called no model; it takes the change there.
  await expect.poll(() => runsMockModel(page, projectId), { timeout: 60_000 }).toBe(true);
  // The composer offers a new Project's scenarios only on the start page; the example's change goes through
  // SendMessage, the request the composer makes.
  await sendMockMessage(page, projectId, "example-change", 1);
  const changed = await readSession(page, projectId);
  expect(Number(changed.agent?.description?.confirmedRequirements?.revision ?? 0)).toBe(2);
  expect(changed.agent?.description?.confirmedRequirements?.spec?.features.map(({ id }) => id)).toEqual([
    "R1",
    "R2",
    "R3",
    "R4",
  ]);
  // The change's card records version 2 where the agent recorded it; version 1 keeps its line beside the greeting.
  await expect(conversation.locator('[data-recorded-version="2"] summary')).toHaveText(
    "Recorded version 2: Slack thread approval",
    { timeout: 60_000 },
  );
  await expect(conversation.locator('[data-recorded-version="1"]')).toHaveText(
    "Recorded version 1: the example's requirements",
  );
});
