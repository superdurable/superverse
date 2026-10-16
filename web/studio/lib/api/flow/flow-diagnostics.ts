// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { sendFlowApiJson, type FlowApiClient } from "@/lib/api/transport/flow-api-client";
import type { FlowHistoryEvent, FlowState, FlowSummary, HistoryPage, StreamMessage } from "./flow-types";
import { isTransientGatewayResponse, readFlowApiJson } from "@/lib/api/transport/api-error";
import { isTimeoutError } from "@/lib/shared/async";

/**
 * The diagnostic operations on one Flow execution: reads of its summary, live state, semantic
 * history and Streams, and the explicit mutations Stop, Time Travel and Channel message removal.
 */

const HISTORY_PAGE_SIZE = "200";

/** The current run of a Flow, or the given run when `runId` is set. */
export async function getFlowSummary(
  client: FlowApiClient,
  flowId: string,
  runId = "",
  signal?: AbortSignal,
): Promise<FlowSummary> {
  const params = new URLSearchParams({ flowId });
  if (runId) params.set("runId", runId);
  return readFlowApiJson<FlowSummary>(
    await client.fetch(`/api/flows/summary?${params}`, { cache: "no-store", signal }),
  );
}

export async function getFlowState(
  client: FlowApiClient,
  flowId: string,
  runId: string,
  signal?: AbortSignal,
): Promise<FlowState> {
  const params = new URLSearchParams({ flowId, runId });
  return readFlowApiJson<FlowState>(await client.fetch(`/api/flows/state?${params}`, { cache: "no-store", signal }));
}

/**
 * Every history page of a run from `startInternalEventId`, and the event id to continue from. A
 * repeated page token would loop forever, so it fails instead.
 */
export async function loadRunHistory(
  client: FlowApiClient,
  flowId: string,
  runId: string,
  startInternalEventId = 0,
  signal?: AbortSignal,
): Promise<{ events: FlowHistoryEvent[]; nextInternalEventId: number }> {
  const events: FlowHistoryEvent[] = [];
  const seenPageTokens = new Set<string>();
  let nextPageToken = "";
  let nextInternalEventId = startInternalEventId;
  do {
    if (nextPageToken) {
      if (seenPageTokens.has(nextPageToken)) {
        throw new Error("History pagination returned a repeated page token");
      }
      seenPageTokens.add(nextPageToken);
    }
    const params = new URLSearchParams({
      flowId,
      runId,
      startInternalEventId: String(nextInternalEventId),
      estimatePageSize: HISTORY_PAGE_SIZE,
    });
    if (nextPageToken) params.set("nextPageToken", nextPageToken);
    const page = await readFlowApiJson<HistoryPage>(
      await client.fetch(`/api/flows/history?${params}`, { cache: "no-store", signal }),
    );
    events.push(...page.events);
    nextPageToken = page.nextPageToken;
    nextInternalEventId = page.nextInternalEventId;
  } while (nextPageToken);
  return { events, nextInternalEventId };
}

/** How a history wait ended: with a newly recorded event, at its deadline with nothing new, or unreachable. */
type HistoryWait = "recorded" | "deadline" | "unavailable";

/**
 * One long poll, or null when the client's backstop ended it first. The server's 30-second
 * window normally returns 408 before the BFF and browser's 35-second bounds. Keep the backstop:
 * Chrome can silently resend a 408 that arrives on a reused HTTP/1.1 connection.
 */
async function longPoll(client: FlowApiClient, path: string, signal: AbortSignal): Promise<Response | null> {
  try {
    return await client.fetch(path, { cache: "no-store", signal });
  } catch (error) {
    if (isTimeoutError(error)) return null;
    throw error;
  }
}

/**
 * Long-polls until the run records a new event, continuing from `nextInternalEventId`. A wait that
 * finds nothing new ends at its deadline (the server's 408 or the client's bound), and a gateway status
 * means the run is unreachable.
 */
export async function waitFlowHistory(
  client: FlowApiClient,
  flowId: string,
  runId: string,
  nextInternalEventId: number,
  signal: AbortSignal,
): Promise<HistoryWait> {
  const params = new URLSearchParams({ flowId, runId, nextInternalEventId: String(nextInternalEventId) });
  const response = await longPoll(client, `/api/flows/wait?${params}`, signal);
  if (!response || response.status === 408) return "deadline";
  if (isTransientGatewayResponse(response)) return "unavailable";
  await readFlowApiJson(response);
  return "recorded";
}

/**
 * Long-polls one Stream until `signal` aborts. A poll that ends at its deadline (408 or the client's
 * bound) closed with nothing new, so the same resume token is asked again.
 */
export async function listenToStream(
  client: FlowApiClient,
  { flowId, flowType, streamName }: { flowId: string; flowType: string; streamName: string },
  signal: AbortSignal,
  onMessage: (message: StreamMessage) => Promise<void>,
): Promise<void> {
  let resumeToken = "";
  while (!signal.aborted) {
    const params = new URLSearchParams({
      flowId,
      flowType,
      streamName,
      resumeToken,
    });
    const response = await longPoll(client, `/api/flows/stream?${params}`, signal);
    if (!response) continue;
    if (response.status === 408) {
      await response.text();
      continue;
    }
    const rawMessage = await readFlowApiJson<StreamMessage>(response);
    resumeToken = rawMessage.resumeToken;
    await onMessage(rawMessage);
  }
}

/** Stops a run: stop type 1 cancels it, 2 terminates it and 3 fails it. */
export async function stopFlow(
  client: FlowApiClient,
  stop: { flowId: string; runId: string; stopType: number; reason: string },
  signal: AbortSignal,
): Promise<void> {
  await sendFlowApiJson(client, "/api/flows/stop", "POST", stop, { signal });
}

/** The Dex TimeTravelType and TimeTravelStepMethod enum values. */
export const TIME_TRAVEL_TYPE = {
  BEGINNING: 1,
  HISTORY_EVENT_TIME: 2,
  STEP_TYPE: 3,
  STEP_EXECUTION_ID: 4,
} as const;

export const TIME_TRAVEL_STEP_METHOD = {
  WAIT_FOR: 1,
  EXECUTE: 2,
} as const;

/** Where a Time Travel starts the new run; only the fields of the chosen Dex TimeTravelType are set. */
export interface TimeTravelRequest {
  flowId: string;
  runId: string;
  timeTravelType: number;
  reason: string;
  stepType?: string;
  stepExecutionId?: string;
  stepMethod?: number;
  historyEventTime?: string;
}

/** Starts a new run of the Flow from a point of an existing run; the answer names the new run. */
export function timeTravelFlow(
  client: FlowApiClient,
  request: TimeTravelRequest,
  signal: AbortSignal,
): Promise<{ runId?: string }> {
  return sendFlowApiJson<{ runId?: string }>(client, "/api/flows/time-travel", "POST", request, { signal });
}

export async function deleteFlowChannelMessage(
  client: FlowApiClient,
  message: { flowId: string; runId: string; channelName: string; messageId: string },
): Promise<void> {
  await sendFlowApiJson(client, "/api/flows/channels/messages", "DELETE", message);
}
