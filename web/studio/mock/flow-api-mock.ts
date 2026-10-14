// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { SearchFilter } from "@/lib/api/flow/flow-run";
import type { FlowSearchRow } from "@/lib/api/flow/flow-types";
import type { FlowApiSelection } from "@/lib/server/flow/flow-api-handler";
import { admitFlowApiRequest } from "@/lib/server/flow/flow-api-security";
import { errorResponse } from "@/lib/server/grpc-client";
import { httpError } from "@/lib/server/request-body";
import { delay } from "@/lib/shared/async";
import { buildDefinitionDexCatalog } from "@/lib/shared/flow-definition-catalog";
import { MOCK_DEFINITION_REVISION } from "@/mock/fixture/flow-definitions";
import {
  loadProjectFlowDefinitions,
  loadRecordedFlowRuns,
  type RecordedFlowRun,
  type RecordedFlowRuns,
} from "@/mock/fixture/flow-runs";
import { clockStartMs } from "@/mock/store/clock";
import { nativeConfigurationFixture } from "@/mock/store/configuration";
import { getStudioMockStore, type MockStore } from "@/mock/store/store";
import { mockSelectedConnection } from "@/mock/store/tenancy/connections";
import { existingMockTenancy } from "@/mock/store/tenancy/team-spaces";

/** A connections answer with what each name's selected TeamSpace connection shows, as Go adds it. */
function withSelectedConnections(store: MockStore, result: object): object {
  const tenancy = existingMockTenancy(store);
  if (!tenancy || !("connections" in result) || !Array.isArray(result.connections)) return result;
  return {
    ...result,
    connections: result.connections.map((connection: { sharedConnectionId?: string }) => ({
      ...connection,
      selectedConnection: connection.sharedConnectionId
        ? mockSelectedConnection(tenancy, connection.sharedConnectionId)
        : undefined,
    })),
  };
}

/*
 * Dex's read surface, answered from the mock Project's Flow Definitions and the runs recorded under them: the
 * same set its Releases and Preview carry, projected by the catalog builder the real Release surface uses. The
 * provider compares `definitionRevision` across the two catalog calls, so both come from that one immutable set.
 *
 * Flow execution mutations are unavailable in fixture mode, so the recorded runs never change.
 */
const MUTATIONS = new Set(["POST flows/stop", "POST v2/actions", "PATCH v2/display"]);

/**
 * The recorded runs never record anything new, so every history wait and Stream read finds nothing. Like the
 * Flow API's diagnostic reads, each ends with 408 after the 30-second server window, before Studio's
 * 35-second browser backstop.
 */
const POLL_WINDOW_MS = 30_000;

// The runs were recorded when the mock clock started, so the seed data and the runs share one moment.
let recordedRuns: Promise<RecordedFlowRuns> | undefined;
const loadRuns = () => (recordedRuns ??= loadRecordedFlowRuns(clockStartMs()));

function jsonResponse(body: unknown, status = 200): Response {
  return Response.json(body, { status, headers: { "cache-control": "no-store", "x-content-type-options": "nosniff" } });
}

async function readJson<T>(request: Request): Promise<T> {
  try {
    return (await request.json()) as T;
  } catch {
    throw httpError(400, "Invalid JSON body");
  }
}

async function projectCatalog() {
  return buildDefinitionDexCatalog(MOCK_DEFINITION_REVISION, await loadProjectFlowDefinitions(), "studio-mock");
}

/** The unscoped /dex/api fixture route, which exists only in mock mode. */
export async function handleUnscopedFlowApiMock(request: Request, path: string[]): Promise<Response> {
  try {
    return await answerFlowApi(request, path);
  } catch (error) {
    return errorResponse(error);
  }
}

/** The Project-scoped Runs and Configuration routes, after the same admission checks as production. */
export async function handleFlowApiMock(request: Request, selection: FlowApiSelection): Promise<Response> {
  try {
    const { projectId, preview = false, releaseId = "", nextPublish = false, purpose, path = [] } = selection;
    admitFlowApiRequest(request, path);
    return purpose === "configuration"
      ? await answerConfiguration(request, { projectId, preview, releaseId, nextPublish, path })
      : await answerFlowApi(request, path[0] === "api" ? path.slice(1) : path);
  } catch (error) {
    return errorResponse(error);
  }
}

async function nothingNew(_run: RecordedFlowRun, request: Request): Promise<Response> {
  await delay(POLL_WINDOW_MS, request.signal);
  return jsonResponse({ error: "context deadline exceeded", grpcCode: 4 }, 408);
}

/** The reads of one run that the query names by Flow ID and, when given, run ID. */
const RUN_READS: Record<
  string,
  (run: RecordedFlowRun, request: Request, query: URLSearchParams) => Response | Promise<Response>
> = {
  "GET v2/display": (run) => jsonResponse(run.display),
  "GET flows/summary": (run) => jsonResponse(run.summary),
  "GET flows/state": (run) =>
    run.state ? jsonResponse(run.state) : jsonResponse({ error: "A closed run has no live state" }, 404),
  // The recorded history is one page, and nothing follows its last event.
  "GET flows/history": (run, _request, query) =>
    jsonResponse({
      events: query.get("startInternalEventId") === "0" ? run.history.events : [],
      nextPageToken: "",
      nextInternalEventId: run.history.nextInternalEventId,
    }),
  "GET flows/wait": nothingNew,
  "GET flows/stream": nothingNew,
};

async function answerFlowApi(request: Request, path: string[]): Promise<Response> {
  const route = `${request.method} ${path.join("/")}`;
  if (MUTATIONS.has(route)) {
    return jsonResponse(
      { error: "Flow execution is unavailable in Studio fixture mode; use a real Preview or Live environment" },
      403,
    );
  }
  if (route === "GET flow-definitions") return jsonResponse((await projectCatalog()).definitions);
  if (route === "GET v2/catalog") return jsonResponse((await projectCatalog()).catalog);
  const { runs, blobs } = await loadRuns();
  // A read that must be a POST: its body carries a Flow type and search filters.
  if (route === "POST v2/search")
    return jsonResponse({ flows: searchRuns(runs, await readJson(request)), nextPageToken: "" });
  if (route === "POST blobs/load") {
    const { values = [] } = await readJson<{ values?: { id: string; kind: string }[] }>(request);
    const keys = values.map(({ kind, id }) => `${kind}:${id}`).filter((key) => Object.hasOwn(blobs, key));
    return jsonResponse({ values: Object.fromEntries(keys.map((key) => [key, blobs[key]])) });
  }
  if (!Object.hasOwn(RUN_READS, route)) return jsonResponse({ error: "Unsupported Dex route" }, 404);
  const query = new URL(request.url).searchParams;
  const flowId = query.get("flowId") ?? "";
  const runId = query.get("runId");
  const run = runs.find(
    (candidate) => candidate.summary.flowId === flowId && (!runId || candidate.summary.runId === runId),
  );
  // Go words Temporal's missing-execution error this way, with gRPC NotFound.
  if (!run) return jsonResponse({ error: `Flow run is not found for flow ID ${flowId}`, grpcCode: 5 }, 404);
  return RUN_READS[route](run, request, query);
}

/** One page of runs, newest first, as Dex answers a search of the Flow type with these filters. */
function searchRuns(
  runs: RecordedFlowRun[],
  search: { flowType?: string; workQueueOnly?: boolean; filters?: SearchFilter[] },
): FlowSearchRow[] {
  return runs
    .filter((run) => run.row.flowType === search.flowType)
    .filter((run) => !search.workQueueOnly || run.display.eligibleActions.length > 0)
    .map((run) => run.row)
    .filter((row) => (search.filters ?? []).every((filter) => passes(row, filter)))
    .sort((left, right) => Date.parse(right.startTime ?? "") - Date.parse(left.startTime ?? ""));
}

/** Whether a row passes one filter of the kinds Runs and Inbox send. */
function passes(row: FlowSearchRow, { field, operator, values: [expected] }: SearchFilter): boolean {
  const actual =
    field === "executionStatus"
      ? row.flowStatus
      : field === "flowId"
        ? row.flowId
        : field === "startTime"
          ? row.startTime
          : row.indexedAttributes[field];
  const ordered = (value: unknown) => (field === "startTime" ? Date.parse(String(value)) : Number(value));
  switch (operator) {
    case "eq":
      return actual === expected;
    case "contains":
      return typeof actual === "string" && actual.includes(String(expected));
    case "gte":
      return ordered(actual) >= ordered(expected);
    case "lte":
      return ordered(actual) <= ordered(expected);
  }
  return false;
}

async function answerConfiguration(
  request: Request,
  {
    projectId,
    preview,
    releaseId,
    nextPublish,
    path,
  }: { projectId: string; preview: boolean; releaseId: string; nextPublish: boolean; path: string[] },
): Promise<Response> {
  if (path.join("/") === "api/flow-definitions") return answerFlowApi(request, ["flow-definitions"]);
  let result: object;
  try {
    const store = getStudioMockStore(new URL(request.url).origin);
    result = withSelectedConnections(
      store,
      await nativeConfigurationFixture(request, store, { projectId, preview, releaseId, nextPublish, path }),
    );
  } catch (error) {
    // Like the configuration host, a coded refusal keeps its safe error code for Studio to act on.
    if (error instanceof Error && "code" in error && "status" in error && typeof error.status === "number")
      return jsonResponse({ error: error.message, code: error.code }, error.status);
    throw error;
  }
  // Like the Flow API handler, a HEAD answer has no body.
  return request.method === "HEAD"
    ? new Response(null, { headers: { "cache-control": "no-store" } })
    : jsonResponse(result);
}
