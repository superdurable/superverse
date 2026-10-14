// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, test, type Page, type Request } from "@playwright/test";
import type { FlowV2Action, FlowV2Definition } from "@/lib/api/flow/flow-definition";
import type { FlowDisplay, FlowRuntimeCatalog } from "@/lib/api/flow/flow-types";
import { readProject, readSession } from "./project";

const summaryText =
  "Dex keeps application state in durable Flows. Build prepares and deploys the current pushed Sandbox commit as Preview. Publishing builds an immutable Release and deploys Live. Preview and Live keep their own configuration and credentials.";

// The template's public form calls its own API, which starts TextSummaryFlow and
// polls the read-only snapshot. This is a real provider call; nothing is faked.
export async function exerciseTextSummaryInApplication(app: Page) {
  await app.getByLabel("Text to summarize", { exact: true }).fill(summaryText);
  await app.getByRole("button", { name: "Summarize", exact: true }).click();
  const result = app.locator("dl");
  await expect(result.getByText("completed", { exact: true })).toBeVisible({ timeout: 5 * 60_000 });
  await expect(app.locator(".summary")).not.toBeEmpty();
  await expect(result).not.toContainText("Not reported");
}

const summaryFlowType = "TextSummaryFlow";

/** A TextSummaryFlow run started in Studio Runs, with the serving definition it runs under. */
interface StudioSummaryRun {
  flowId: string;
  /** The Runs page of the exact target, without a selected run. */
  runsURL: string;
  definitionRevision: string;
  definition: FlowV2Definition;
  /** The run's typed Display at definitionRevision. */
  read: () => Promise<FlowDisplay>;
}

/**
 * The Flow API path of the Preview or Live target and the headers naming the instance this test observes:
 * the Session's workspace for the Project's Preview, the Project's current deployment for Live.
 */
async function observedTarget(
  page: Page,
  projectId: string,
  environment: "preview" | "live",
  releaseId: string,
): Promise<{ prefix: string; headers: Record<string, string> }> {
  if (environment === "preview") {
    const { workspace } = await readSession(page, projectId);
    if (!workspace) throw new Error("The current Preview instance has no observed workspace");
    return {
      prefix: `/dex/projects/${projectId}/environments/preview`,
      headers: {
        "x-superverse-workspace-request-id": workspace.workspaceRequestId,
        "x-superverse-generation": workspace.generation,
      },
    };
  }
  const { live } = await readProject(page, projectId);
  if (!live) throw new Error("The Project has no current Live deployment");
  return {
    prefix: `/dex/projects/${projectId}/environments/live/releases/${releaseId}`,
    headers: { "x-superverse-deployment-id": live.deploymentId },
  };
}

// A Studio test run (Preview) or live run (Live) of the template's TextSummaryFlow in the exact target.
async function startSummaryInStudio(
  page: Page,
  projectId: string,
  environment: "preview" | "live",
  releaseId: string,
): Promise<StudioSummaryRun> {
  const runsURL = `/p/${projectId}/runs?${new URLSearchParams({ v: environment, flowType: summaryFlowType })}`;
  await page.goto(runsURL);
  await page.getByRole("button", { name: environment === "live" ? "Live run" : "Test run", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.locator('[data-start-path="$.text"]').fill(summaryText);
  const accepted = page.waitForResponse(
    (response) => new URL(response.url()).pathname.endsWith("/api/v2/start") && response.request().method() === "POST",
  );
  await dialog
    .getByRole("button", { name: environment === "live" ? "Start live run" : "Start Flow", exact: true })
    .click();
  const response = await accepted;
  expect(response.ok()).toBe(true);
  const started: { flowId: string } = await response.json();
  expect(Object.keys(started)).toEqual(["flowId"]);
  const { prefix, headers } = await observedTarget(page, projectId, environment, releaseId);
  const catalogResponse = await page.request.get(prefix + "/api/v2/catalog", { headers });
  expect(catalogResponse.ok()).toBe(true);
  const catalog: FlowRuntimeCatalog = await catalogResponse.json();
  const entry = catalog.flows.find((flow) => flow.flowType === summaryFlowType);
  if (!entry) throw new Error(`The ${environment} catalog does not serve ${summaryFlowType}`);
  const query = new URLSearchParams({ flowType: summaryFlowType, flowId: started.flowId });
  const read = async (): Promise<FlowDisplay> => {
    const current = await page.request.get(`${prefix}/api/v2/display?${query}`, {
      headers: { ...headers, "X-Dex-Flow-Definition-Revision": catalog.definitionRevision },
    });
    expect(current.ok()).toBe(true);
    return current.json();
  };
  return {
    flowId: started.flowId,
    runsURL,
    definitionRevision: catalog.definitionRevision,
    definition: entry.definition,
    read,
  };
}

// A real summary completes and offers no further Action; its Display survives a reload.
async function expectSummaryCompleted(
  page: Page,
  run: StudioSummaryRun,
  attachment: string,
  identity: { projectId: string; environment: string },
) {
  await expect
    .poll(
      async () => {
        const current = await run.read();
        return { status: current.flowStatusCode, state: current.display["summary-status"] };
      },
      { timeout: 5 * 60_000 },
    )
    .toEqual({ status: 2, state: "completed" });
  const completed = await run.read();
  expect(String(completed.display["summary-text"]).length).toBeGreaterThan(0);
  expect(String(completed.display["summary-model"]).length).toBeGreaterThan(0);
  expect(Number(completed.display["total-tokens"])).toBeGreaterThan(0);
  expect(completed.eligibleActions).toEqual([]);
  await page.reload();
  expect((await run.read()).display).toEqual(completed.display);
  await test.info().attach(attachment, {
    contentType: "application/json",
    body: JSON.stringify({
      ...identity,
      flowId: run.flowId,
      definitionRevision: run.definitionRevision,
      status: completed.display["summary-status"],
      model: completed.display["summary-model"],
      totalTokens: completed.display["total-tokens"],
    }),
  });
}

// Studio Start Flow, typed Display and retained read after completion for the
// unmodified template's TextSummaryFlow in the exact Preview or Live target.
export async function exerciseTextSummaryInStudio(
  page: Page,
  projectId: string,
  environment: "preview" | "live",
  releaseId = "",
) {
  const run = await startSummaryInStudio(page, projectId, environment, releaseId);
  await expectSummaryCompleted(page, run, `text-summary-${environment}`, { projectId, environment });
}

/**
 * Opens the run in Runs and clicks the Step it waits at, which the canvas rings "now": the run's
 * drawer opens only on a Step click. The canvas opens framed on that Step with the Steps a transition
 * joins to it, here the summary it approves and the result it saves, wholly in view.
 */
async function openWaitingStep(page: Page, run: StudioSummaryRun) {
  await page.goto(`${run.runsURL}&${new URLSearchParams({ flowId: run.flowId })}`);
  const now = page.getByRole("button", { name: /, now$/ });
  for (const card of [
    now,
    page.getByRole("button", { name: /^Generate summary,/ }),
    page.getByRole("button", { name: /^Save summary result,/ }),
  ]) {
    await expect(card).toBeInViewport({ ratio: 1, timeout: 60_000 });
  }
  await expectLinesKeptThroughPolls(page);
  await now.click();
  await expect(page.getByRole("region", { name: run.flowId, exact: true })).toBeVisible();
}

/**
 * Each poll of an open run rebuilds the drawing. Its lines must stay the same elements: a re-created
 * line vanished and drew itself in again on every poll. Waiting for two of the canvas's state reads
 * means the first poll's drawing has rendered.
 */
async function expectLinesKeptThroughPolls(page: Page) {
  const lines = page.getByTestId("canvas-line");
  await lines.first().waitFor({ state: "attached" });
  await lines.evaluateAll((paths) => paths.forEach((path) => path.setAttribute("data-e2e-seen", "")));
  const stateRead = () =>
    page.waitForResponse((response) => new URL(response.url()).pathname.endsWith("/api/flows/state"), {
      timeout: 30_000,
    });
  await stateRead();
  await stateRead();
  await expect(lines.first()).toBeAttached();
  await expect(page.locator('[data-testid="canvas-line"]:not([data-e2e-seen])')).toHaveCount(0);
}

/**
 * Opens Diagnostics of an open run from its drawer and requires its recorded
 * history to render: the run's summary, history and live state read through
 * the Flow API, the open run long-polls for its next event, and the Timeline
 * lists the recorded events from the start of the run. A wait that finds nothing
 * new returns the server's 408 after 30 seconds, and Diagnostics waits again
 * without reporting a failure. The empty Stream read follows the same contract.
 */
async function expectDiagnostics(page: Page, run: StudioSummaryRun) {
  await openWaitingStep(page, run);
  const flowRead = (name: string) =>
    page.waitForResponse((response) => {
      const url = new URL(response.url());
      // Diagnostics names the run; the drawer's own summary read does not.
      return url.pathname.endsWith(`/api/flows/${name}`) && url.searchParams.get("runId") !== null;
    });
  const reads = Promise.all(["summary", "history", "state"].map(flowRead));
  await expectIdleDiagnosticRead(page, run.flowId, "wait", async () => {
    await page
      .getByRole("region", { name: run.flowId, exact: true })
      .getByRole("link", { name: "Diagnostics", exact: true })
      .click();
    for (const response of await reads) expect(response.ok()).toBe(true);
    await expect(page.getByRole("heading", { name: run.flowId, exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Timeline", exact: true }).click();
    await expect(page.getByRole("heading", { name: /^[1-9][0-9]* events$/ })).toBeVisible();
    await expect(page.getByRole("heading", { name: "Flow started", exact: true }).first()).toBeVisible();
  });
  await expect(page.getByText(/signal timed out|History wait failed/)).toHaveCount(0);
  await page.getByRole("button", { name: "Streams", exact: true }).click();
  // Dex accepts an empty Stream key even before any producer has written to it.
  // This uses the serving target's actual Stream API without changing the Flow.
  await page.getByLabel("Stream name", { exact: true }).fill("diagnostics-idle");
  await expectIdleDiagnosticRead(page, run.flowId, "stream", () =>
    page.getByRole("button", { name: "Start listening", exact: true }).click(),
  );
  await expect(page.getByText(/signal timed out|Stream read failed/)).toHaveCount(0);
  await expect(page.getByText("Listening", { exact: true })).toBeVisible();
}

/** Requires a complete server timeout document and the next poll with the same cursor. */
async function expectIdleDiagnosticRead(
  page: Page,
  flowId: string,
  path: "wait" | "stream",
  start: () => Promise<void>,
) {
  const waits: { request: Request; sentAt: number }[] = [];
  const recordWait = (request: Request) => {
    const url = new URL(request.url());
    if (url.pathname.endsWith(`/api/flows/${path}`) && url.searchParams.get("flowId") === flowId)
      waits.push({ request, sentAt: Date.now() });
  };
  page.on("request", recordWait);
  try {
    await start();
    await expect.poll(() => waits.length).toBeGreaterThan(0);
    const first = waits[0];
    const response = await first.request.response();
    expect(response, "The first idle poll must receive a response instead of aborting").not.toBeNull();
    expect(response!.status()).toBe(408);
    expect(await response!.finished(), "The complete timeout document must arrive").toBeNull();
    expect(await response!.json()).toMatchObject({ grpcCode: 4 });
    await expect.poll(() => waits.length, { timeout: 10_000 }).toBeGreaterThan(1);
    const elapsed = waits[1].sentAt - first.sentAt;
    expect(elapsed).toBeGreaterThanOrEqual(30_000);
    expect(elapsed, "The server window must finish before the browser's 35-second backstop").toBeLessThan(35_000);
    expect(waits[1].request.url()).toBe(first.request.url());
    test.info().annotations.push({
      type: "Flow API idle read",
      description: JSON.stringify({ flowId, path, status: response!.status(), elapsedMs: elapsed }),
    });
  } finally {
    page.off("request", recordWait);
  }
}

// The Action whose addition template-approval.spec.ts asks the Agent for.
const approveSummaryAction: FlowV2Action = {
  rpcName: "ApproveSummary",
  label: "Approve summary",
  requiredPermission: "summary.approve",
  condition: { attributeKey: "summary-status", operator: "in", values: ["awaiting-approval"] },
  input: { kind: "none" },
};

/**
 * Studio Start Flow of a TextSummaryFlow that waits for approval after generating:
 * the run waits at summary-status awaiting-approval with the Approve summary Action
 * eligible, its Diagnostics show the recorded history, a person approves it in the
 * run drawer, and the run then completes.
 */
export async function exerciseApprovalInStudio(
  page: Page,
  projectId: string,
  environment: "preview" | "live",
  releaseId = "",
) {
  const run = await startSummaryInStudio(page, projectId, environment, releaseId);
  // A no-input Action may carry `fields: null` on the wire; only its kind and the absence of fields matter.
  expect(
    run.definition.actions.map((action) => ({
      ...action,
      input: { kind: action.input.kind, fields: action.input.fields ?? [] },
    })),
  ).toEqual([{ ...approveSummaryAction, input: { ...approveSummaryAction.input, fields: [] } }]);
  await expect
    .poll(
      async () => {
        const current = await run.read();
        if (!current.isActive)
          throw new Error(
            `${summaryFlowType} ${run.flowId} closed at summary-status ${String(current.display["summary-status"])} without waiting for approval`,
          );
        return {
          state: current.display["summary-status"],
          approvable: current.eligibleActions.includes(approveSummaryAction.rpcName),
        };
      },
      { timeout: 5 * 60_000 },
    )
    .toEqual({ state: "awaiting-approval", approvable: true });
  await expectDiagnostics(page, run);
  // The run drawer is the Studio control a person uses; its form posts the Action.
  await openWaitingStep(page, run);
  const approve = page
    .getByRole("region", { name: run.flowId, exact: true })
    .getByRole("button", { name: approveSummaryAction.label, exact: true });
  await expect(approve).toBeEnabled({ timeout: 60_000 });
  const [response] = await Promise.all([
    page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname.endsWith("/api/v2/actions") && response.request().method() === "POST",
    ),
    approve.click(),
  ]);
  expect(response.ok()).toBe(true);
  expect(response.request().postDataJSON()).toMatchObject({
    flowType: summaryFlowType,
    flowId: run.flowId,
    rpcName: approveSummaryAction.rpcName,
  });
  await expectSummaryCompleted(page, run, `summary-approval-${environment}`, { projectId, environment });
}
