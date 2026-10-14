// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { ErrorInfo__Output } from "@/lib/generated/protobuf/google/rpc/ErrorInfo";
import { asRecord } from "@/lib/shared/json";
import { isRevisionChanged } from "@/lib/shared/revision-conflict";

/**
 * A non-2xx response with an error body: a Studio route's (the BFF's sanitized
 * message and the command admission detail it lets through) or the Flow API's
 * (the server's message, safe error code and gRPC code). A gRPC code the body
 * omitted is absent, never 0, so one unreadable run can be told apart from an
 * unreachable server.
 */
export class ApiError extends Error {
  readonly errorInfo?: ErrorInfo__Output;
  readonly code?: string;
  readonly grpcCode?: number;

  constructor(
    readonly status: number,
    message: string,
    detail: { errorInfo?: ErrorInfo__Output; code?: string; grpcCode?: number } = {},
  ) {
    super(message);
    this.name = "ApiError";
    this.errorInfo = detail.errorInfo;
    this.code = detail.code;
    this.grpcCode = detail.grpcCode;
  }
}

/** A write the server refused because what it was fenced on changed since the page read it. */
export function isRevisionConflict(reason: unknown): boolean {
  return reason instanceof ApiError && isRevisionChanged(reason.errorInfo);
}

/**
 * The ApiError of a failed response, read from the BFF's `{error, errorInfo}`
 * body. The BFF is the one gate for ErrorInfo, so it is read as sent.
 */
export async function responseError(response: Response): Promise<ApiError> {
  const problem = asRecord(await response.json().catch(() => null));
  const message = problem.error;
  return new ApiError(
    response.status,
    typeof message === "string" && message ? message : `Request returned HTTP ${response.status}`,
    { errorInfo: problem.errorInfo as ErrorInfo__Output | undefined },
  );
}

/**
 * The JSON body of a Flow API response. A failed request throws an ApiError with the server's
 * message and codes; an empty or non-JSON body throws a plain Error describing what came back,
 * because nothing says whether the server acted on the request.
 */
export async function readFlowApiJson<T>(response: Response): Promise<T> {
  const data = await parseFlowApiJson<T & { error?: string; grpcCode?: number; code?: string }>(response);
  if (!response.ok) {
    throw new ApiError(response.status, data.error?.trim() || describe(response, "an error"), {
      code: data.code,
      grpcCode: typeof data.grpcCode === "number" ? data.grpcCode : undefined,
    });
  }
  return data;
}

const transientGatewayStatusCodes = new Set([502, 503, 504]);

export function isTransientGatewayResponse(response: Response): boolean {
  return transientGatewayStatusCodes.has(response.status);
}

async function parseFlowApiJson<T>(response: Response): Promise<T> {
  const body = await response.text();
  if (!body.trim()) {
    throw new Error(`${describe(response, "an empty response")}. The Dex server may be unreachable.`);
  }
  try {
    return JSON.parse(body) as T;
  } catch {
    const snippet = bodySnippet(body);
    const description = describe(response, "a non-JSON response");
    throw new Error(snippet ? `${description}: ${snippet}` : `${description}.`);
  }
}

function describe(response: Response, kind: string): string {
  return `Dex API returned ${kind} (HTTP ${response.status})`;
}

/** A short single-line excerpt of a text body; an HTML page says nothing useful, so it is dropped. */
function bodySnippet(body: string): string {
  const trimmed = body.replace(/\s+/g, " ").trim();
  if (!trimmed || trimmed.startsWith("<")) return "";
  return trimmed.length > 160 ? `${trimmed.slice(0, 157)}...` : trimmed;
}

export function newRequestId(): string {
  return crypto.randomUUID();
}

/** What to tell the user about a failure: an Error's own message, or `fallback` for anything else. */
export function errorMessage(reason: unknown, fallback: string): string {
  return reason instanceof Error && reason.message ? reason.message : fallback;
}

/**
 * Whether the server definitely refused a request: the statuses the BFF gives a
 * business rejection (invalid input, sign-in, access, absence, conflict).
 * Anything else, including a network failure or an abort, is uncertain.
 * 404 counts for every command, Preview included: an absent Session, Release
 * or Project cannot have admitted the request, and a 404 after an uncertain
 * attempt still keeps that attempt's ID (see forgetsRequest).
 */
function isDefinitiveRejection(reason: unknown): boolean {
  return reason instanceof ApiError && [400, 401, 403, 404, 409].includes(reason.status);
}

/**
 * The one retry rule for idempotent commands. A command keeps its request ID
 * across retries until it is accepted, or until the server definitely rejects
 * an attempt of a command that never had an uncertain attempt. Once an attempt
 * was uncertain, that first request may have been admitted, so a later
 * rejection keeps the ID: the next retry repeats the same request instead of
 * starting a second one.
 */
function forgetsRequest(reason: unknown, wasUncertain: boolean): boolean {
  return !wasUncertain && isDefinitiveRejection(reason);
}

/** An idempotent command not yet accepted: its exact input, request ID included, and whether an attempt was uncertain. */
export type PendingCommand<Input> = { input: Input; uncertain: boolean };

/**
 * A pending command after a failed attempt, by the rule above: null when the
 * next click starts a new request, otherwise the same input, now uncertain,
 * whose retry repeats its request ID.
 */
export function retainedAfterFailure<Input>(
  command: PendingCommand<Input>,
  reason: unknown,
): PendingCommand<Input> | null {
  return forgetsRequest(reason, command.uncertain) ? null : { ...command, uncertain: true };
}
