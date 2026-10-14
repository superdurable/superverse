// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { readFlowApiJson } from "@/lib/api/transport/api-error";
import { definitionRevisionHeaders, sendFlowApiJson, type FlowApiClient } from "@/lib/api/transport/flow-api-client";
import type { FlowDefinitionCatalog, FlowDisplay, FlowRuntimeCatalog, FlowSearchResult } from "./flow-types";

/**
 * The Run operations of one Flow API environment: its definitions and catalog, the run search, a
 * run's Display fields and Actions, and Flow starts. Every write names the definition revision its
 * caller rendered, so the server rejects it when the definition changed underneath.
 */

/** The definition revision a call is bound to, and the signal that abandons it. */
type DefinitionBound = { definitionRevision: string; signal?: AbortSignal };

export async function getFlowDefinitions(client: FlowApiClient, signal?: AbortSignal): Promise<FlowDefinitionCatalog> {
  return readFlowApiJson<FlowDefinitionCatalog>(await client.fetch("/api/flow-definitions", { signal }));
}

export async function getFlowCatalog(client: FlowApiClient, signal?: AbortSignal): Promise<FlowRuntimeCatalog> {
  return readFlowApiJson<FlowRuntimeCatalog>(await client.fetch("/api/v2/catalog", { signal }));
}

/** One filter of a run search, with values already typed for the index. */
export interface SearchFilter {
  field: string;
  operator: string;
  values: unknown[];
}

/** One page of a Flow type's runs; `workQueueOnly` keeps those whose current state awaits a declared Action. */
export function searchFlows(
  client: FlowApiClient,
  search: { flowType: string; workQueueOnly: boolean; filters: readonly SearchFilter[]; pageSize: number },
  options: DefinitionBound,
): Promise<FlowSearchResult> {
  return sendFlowApiJson<FlowSearchResult>(client, "/api/v2/search", "POST", search, options);
}

/** A run's Display fields and Actions, read for the definition revision the caller renders. */
export async function getFlowDisplay(
  client: FlowApiClient,
  { flowType, flowId }: { flowType: string; flowId: string },
  { definitionRevision, signal }: DefinitionBound,
): Promise<FlowDisplay> {
  const query = new URLSearchParams({ flowType, flowId });
  return readFlowApiJson<FlowDisplay>(
    await client.fetch(`/api/v2/display?${query}`, { headers: definitionRevisionHeaders(definitionRevision), signal }),
  );
}

export async function updateFlowDisplayField(
  client: FlowApiClient,
  field: { flowType: string; flowId: string; attributeKey: string; value: unknown },
  options: DefinitionBound,
): Promise<void> {
  await sendFlowApiJson(client, "/api/v2/display", "PATCH", field, options);
}

/** Invokes an Action against the Display attributes the person saw when deciding. */
export async function invokeFlowAction(
  client: FlowApiClient,
  action: {
    flowType: string;
    flowId: string;
    rpcName: string;
    input: unknown;
    attributeSnapshot: Record<string, unknown>;
  },
  options: DefinitionBound,
): Promise<void> {
  await sendFlowApiJson(client, "/api/v2/actions", "POST", action, options);
}

/** A Flow start: the stable Flow and request IDs, the start target it was offered for, and its input. */
export interface FlowStart {
  flowType: string;
  flowId: string;
  requestId: string;
  targetRevision?: string;
  /** The input as JSON text, spliced in raw so 64-bit integers are never rounded through a JavaScript number. */
  inputJson: string;
}

export function startFlow(
  client: FlowApiClient,
  request: FlowStart,
  options: DefinitionBound,
): Promise<{ flowId: string }> {
  return postStart(client, "/api/v2/start", request, options);
}

/** The Flow an uncertain start of the same request admitted; START_NOT_FOUND when it admitted none. */
export function recoverFlowStart(
  client: FlowApiClient,
  request: FlowStart,
  options: DefinitionBound,
): Promise<{ flowId: string }> {
  return postStart(client, "/api/v2/start/recover", request, options);
}

async function postStart(
  client: FlowApiClient,
  path: string,
  { inputJson, ...fields }: FlowStart,
  { definitionRevision, signal }: DefinitionBound,
): Promise<{ flowId: string }> {
  const head = JSON.stringify(fields).slice(0, -1); // the fields object without its closing brace
  const response = await client.fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...definitionRevisionHeaders(definitionRevision) },
    body: `${head},"input":${inputJson}}`,
    signal,
  });
  return readFlowApiJson<{ flowId: string }>(response);
}
