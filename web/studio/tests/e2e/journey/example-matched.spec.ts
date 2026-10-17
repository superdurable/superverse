// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { test, expect } from "@/tests/e2e/support/coverage/spec-coverage";
import { register } from "@/tests/e2e/support/account";
import {
  chooseMockFirstInput,
  choosesMockModel,
  runMockScenario,
  waitForFirstMockInput,
} from "@/tests/e2e/support/mock/mock-composer";
import {
  createProject,
  deleteProject,
  MOCK_TARGET,
  modelTargetDecision,
  readProject,
  readSession,
} from "@/tests/e2e/support/project";
import { createTeamSpace } from "@/tests/e2e/support/teamspace/teamspace";

const EXAMPLE_ID = "slack-thread-approval";

// A Project created from a description close to a built example, on the mock model, through the real Studio, BFF and
// server. Build sends example-matched's first input as the first message and no example; the server, whose
// closest-example pick the local lanes turn on, starts the Project from the slack-thread-approval example. Nothing
// says so: the Session names the example only as matched, the chat opens with the person's message and no greeting,
// and the agent's first turn records its own version 1 without a Flow design, changes the reply text and pushes it,
// without naming the example. While that turn runs, the Builder keeps its Building stage and the Flow tab shows the
// real flow at once, the example's own Flow Definitions, with no design, design phase or overview and nothing that
// names the example; the push, the first build, records that its Flow keeps all 4 of the example's real Steps. The
// stage stays Building, never Shaping, until the Preview has built the push, whose processes then draw the same
// flow and end the stage. Input 2 then runs on a segment that no longer describes the example.
test("a description close to a built example starts from it without a word about it", async ({ page }, info) => {
  test.setTimeout(60 * 60_000);
  const target = modelTargetDecision();
  test.skip(
    target !== undefined && target !== MOCK_TARGET,
    "this journey runs the mock model's example-matched scenario",
  );
  await register(page);
  const teamSpaceId = await createTeamSpace(page);
  expect(await choosesMockModel(page), "the lane offers the mock model").toBe(true);
  const { projectId } = await createProject(page, teamSpaceId, chooseMockFirstInput("example-matched"), info);

  const project = await readProject(page, projectId);
  expect(project.example?.exampleId, "the server started the Project from the closest example").toBe(EXAMPLE_ID);
  expect(project.templateRevision).toBe(`app-template/${EXAMPLE_ID}@${project.example?.tree}`);
  const started = await readSession(page, projectId);
  expect(started.agent?.description?.startingExample).toMatchObject({
    exampleId: EXAMPLE_ID,
    choice: "CHOICE_MATCHED",
  });
  // The agent's first reply records its own version 1 and the turn goes on to build it: the Builder names the stage
  // Building, and the Flow tab draws the real flow at once, the matched example's own Flow Definitions, which no
  // design replaces and nothing names.
  await expect
    .poll(
      async () => Number((await readSession(page, projectId)).agent?.description?.confirmedRequirements?.revision ?? 0),
      { timeout: 10 * 60_000, intervals: [1000] },
    )
    .toBe(1);
  await page.goto(`/p/${projectId}/flow?flowType=SlackThreadApprovalFlow`);
  const flow = page.getByRole("region", { name: "Flow definition", exact: true });
  await expect(flow.getByRole("button", { name: /^Reply, 3 steps\b/ })).toBeVisible({ timeout: 60_000 });
  const stage = page.getByTestId("project-stage");
  await expect(stage).toHaveText("Building");
  const noDesign = async () => {
    await expect(page.locator("[data-design-phase]")).toHaveCount(0);
    await expect(page.getByLabel("Flow design", { exact: true })).toHaveCount(0);
    await expect(page.locator("[data-brief-state]")).toHaveCount(0);
    await expect(page.getByTestId("design-status")).toHaveCount(0);
    await expect(page.getByTestId("design-marker")).toHaveCount(0);
    await expect(flow).not.toContainText(/\bexamples?\b/i);
  };
  await noDesign();

  // The matched start records no card of the example: version 1 is the agent's own, from its first turn.
  await waitForFirstMockInput(page, projectId, "example-matched", 25 * 60_000);
  const built = await readSession(page, projectId);
  const requirements = built.agent?.description?.confirmedRequirements;
  expect(Number(requirements?.revision ?? 0)).toBe(1);
  expect(requirements?.spec?.features.map(({ id }) => id)).toEqual(["R1", "R2", "R3"]);
  expect(requirements?.spec?.features[2]?.text).toContain("Approved.");
  expect(requirements?.spec?.design ?? null, "version 1 carries no Flow design").toBeNull();
  // The push, the first build, kept all 4 of the example's Steps, counted in real Flow definitions on both sides.
  expect(built.agent?.description?.startingExample?.firstBuild).toEqual({
    exampleSteps: 4,
    builtSteps: 4,
    keptSteps: 4,
    commit: built.headCommit,
  });
  const messages = built.agent?.history?.messages.map(({ message }) => message) ?? [];
  expect(messages[0]?.role, "the chat opens with the person's message").toBe("ROLE_USER");
  expect(
    messages.some((message) => message?.role === "ROLE_SYSTEM" && message.content.includes("starts from the")),
  ).toBe(false);
  const replies = messages.filter((message) => message?.role === "ROLE_ASSISTANT");
  expect(replies.length).toBeGreaterThan(0);
  expect(
    replies.some((message) => /example/i.test(message?.content ?? "")),
    "no reply names the example",
  ).toBe(false);
  // The turn ended with the push unbuilt: the Flow tab no longer draws the setup commit's definitions, and the bar
  // names the stage Building, never Shaping (or none, once the Preview has built the push).
  expect(await stage.allTextContents(), "the stage while the Preview builds the push").not.toContain("Shaping");
  await expect
    .poll(
      async () => {
        const session = await readSession(page, projectId);
        const prepared = session.preview?.preparedSource;
        return prepared?.status === "PREVIEW_SOURCE_STATUS_READY" && prepared.sourceCommit === built.headCommit;
      },
      { timeout: 15 * 60_000, intervals: [5000] },
    )
    .toBe(true);

  // Studio names no example: no greeting, version 1 where the agent recorded it, and the same real flow, now the
  // processes the Preview built from the push, still with no design; the stage is built, so the bar names none.
  await page.goto(`/p/${projectId}/flow?flowType=SlackThreadApprovalFlow`);
  const conversation = page.getByRole("region", { name: "Conversation", exact: true });
  await expect(conversation.locator('[data-recorded-version="1"] summary')).toHaveText(
    "Recorded version 1: Slack thread approval",
    { timeout: 60_000 },
  );
  await expect(conversation).not.toContainText("This project starts from the");
  await expect(conversation).not.toContainText("the example's requirements");
  await expect(flow.getByRole("button", { name: /^Reply, 3 steps\b/ })).toBeVisible({ timeout: 60_000 });
  await expect(stage).toHaveCount(0);
  await noDesign();

  // After the build a compacted segment no longer describes the example; the scenario's last reply requires it.
  await runMockScenario(page, projectId, "example-matched", { inputs: [2] });
  const checked = await readSession(page, projectId);
  // A Preview configuration notice may land after the scenario's last reply.
  const last = checked.agent?.history?.messages
    .filter(({ message }) => message?.role === "ROLE_ASSISTANT")
    .at(-1)?.message;
  expect(last?.content).toBe("The context holds version 1 and nothing more to adapt.");
  await deleteProject(page, projectId);
});
