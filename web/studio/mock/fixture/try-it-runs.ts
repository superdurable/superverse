// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// The `try-it` Setup fixture's runs: the recorded runs hidden, so Runs opens on Try it, and test runs that
// start. Development only.
import type { RecordedFlowRun } from "./flow-runs";

/** The start target the catalog offers while the fixture is on. */
const TRY_IT_TARGET = "mock-start-target-1";

/**
 * Whether the fixture is on, and the runs started since, newest last, each with the request that started it.
 * Once on, it stays on until the mock server restarts, as the other named fixtures do. The page that seeds it
 * and the `/dex` route that reads it are separate bundles, so the state lives on globalThis like the store.
 */
interface TryItRuns {
  started: { requestId: string; run: RecordedFlowRun }[];
}
const shared = globalThis as { superverseMockTryItRuns?: TryItRuns };

export function enableTryItRuns() {
  shared.superverseMockTryItRuns ??= { started: [] };
}

/** The runs Runs and Inbox read: the recorded ones, or only those started while the fixture is on. */
export function visibleRuns(recorded: RecordedFlowRun[]): RecordedFlowRun[] {
  const state = shared.superverseMockTryItRuns;
  return state ? state.started.map((entry) => entry.run) : recorded;
}

/** The catalog's start capability: enabled while the fixture is on, absent otherwise. */
export function tryItStartFlow(): { enabled: boolean; targetRevision: string } | undefined {
  return shared.superverseMockTryItRuns ? { enabled: true, targetRevision: TRY_IT_TARGET } : undefined;
}

interface StartBody {
  flowType?: string;
  flowId?: string;
  requestId?: string;
}

/**
 * Starts a run, or with `recover` answers which run an earlier request started: the waiting recorded run
 * copied under the new Flow ID and type, started now. The same request ID answers the run it started.
 */
export function startTryItRun(
  recorded: RecordedFlowRun[],
  body: StartBody,
  { recover, now }: { recover: boolean; now: string },
): { status: number; body: object } {
  const state = shared.superverseMockTryItRuns;
  if (!state) return { status: 404, body: { error: "Unsupported Dex route" } };
  const earlier = state.started.find((entry) => entry.requestId === body.requestId);
  if (earlier) return { status: 200, body: { flowId: earlier.run.row.flowId } };
  if (recover) return { status: 404, body: { error: "No run started for this request", code: "START_NOT_FOUND" } };
  const template = recorded.find((run) => run.state !== null) ?? recorded[0];
  if (!template || !body.flowId || !body.flowType || !body.requestId)
    return { status: 400, body: { error: "A start names its Flow type, Flow ID and request ID" } };
  const text = JSON.stringify(template)
    .replaceAll(template.row.flowId, body.flowId)
    .replaceAll(`"${template.row.flowType}"`, JSON.stringify(body.flowType));
  const run = JSON.parse(text) as RecordedFlowRun;
  run.row.startTime = now;
  state.started.push({ requestId: body.requestId, run });
  return { status: 200, body: { flowId: body.flowId } };
}
