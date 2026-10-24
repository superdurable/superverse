// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { FlowApiContext__Output } from "@/lib/generated/protobuf/superverse/v2/FlowApiContext";
import type { FlowApiDocument__Output } from "@/lib/generated/protobuf/superverse/v2/FlowApiDocument";
import { callerFromRequest } from "@/lib/server/caller-envelope";
import { errorResponse, rpcErrorInfo } from "@/lib/server/grpc-client";
import { httpError, parseUint64, readBoundedBody } from "@/lib/server/request-body";
import { requireSurface } from "@/lib/server/request-origin";
import { connectionRefusalInfo } from "@/lib/shared/problem/refusals";
import { dispatchFlowApiRequest } from "./flow-api-operations";
import { FlowApiJson } from "./flow-api-json";
import { admitFlowApiRequest, checkCSRF, csrfToken, flowApiTarget, type FlowApiPurpose } from "./flow-api-security";

/**
 * The exact target and purpose a /dex/projects route selected: the Project's Preview, a Live Release, or Live's
 * next-publish layer, checked against a Release's source or, without one, the Preview's.
 */
export interface FlowApiSelection {
  projectId: string;
  preview?: boolean;
  releaseId?: string;
  nextPublish?: boolean;
  purpose: FlowApiPurpose;
  path?: string[];
}

// Reads whose response hands the browser the CSRF token for later writes.
const CSRF_READS = ["api/v2/catalog", "api/v2/connector-connections", "api/v2/application-environment"];

/**
 * Puts the browser's CSRF token where the Flow API handler placed its own
 * internal capability (top level and in startFlow), so that capability never
 * reaches the browser.
 */
function withBrowserCSRF(body: string, token: string, expose: boolean): string {
  if (!body.trimStart().startsWith("{")) return body;
  const fields = new Map(new FlowApiJson(body).fields);
  const start = fields.get("startFlow");
  if (!expose && !fields.has("csrfToken") && (!start || start === "null")) return body;
  if (expose || fields.has("csrfToken")) fields.set("csrfToken", JSON.stringify(token));
  if (start && start !== "null") fields.set("startFlow", new FlowApiJson(start).withString("csrfToken", token));
  return `{${[...fields].map(([key, value]) => `${JSON.stringify(key)}:${value}`).join(",")}}`;
}

function documentResponse(
  result: FlowApiDocument__Output,
  method: string,
  token?: string,
  exposeCSRF = false,
): Response {
  const status = result.httpStatus || 200;
  if (status < 200 || status > 599 || result.json.byteLength > 16 * 1024 * 1024)
    throw httpError(502, "Invalid Flow API response");
  const headers = new Headers({
    "cache-control": "no-store",
    "content-type": "application/json",
    "x-content-type-options": "nosniff",
    "referrer-policy": "no-referrer",
  });
  if (status === 429 && result.retryAfterSeconds > 0) headers.set("retry-after", String(result.retryAfterSeconds));
  let body = result.json.toString("utf8");
  if (token) {
    headers.set("x-superverse-csrf-token", token);
    body = withBrowserCSRF(body, token, exposeCSRF);
  }
  return new Response(method === "HEAD" || status === 204 || status === 304 ? null : body, { status, headers });
}

/** /dex/projects/{project}/...: Runs and Configuration for one exact target. */
export async function handleFlowApiRequest(request: Request, selection: FlowApiSelection): Promise<Response> {
  try {
    requireSurface(request, "studio");
    const { projectId, preview = false, releaseId = "", nextPublish = false, purpose, path = [] } = selection;
    admitFlowApiRequest(request, path);
    const caller = callerFromRequest(request);
    const target = flowApiTarget(request, projectId, preview, releaseId, purpose, nextPublish);
    const token = csrfToken(caller, target, purpose);
    const mutation = !["GET", "HEAD"].includes(request.method);
    if (mutation) checkCSRF(request, token);
    const body = mutation
      ? new TextDecoder("utf-8", { fatal: true }).decode(await readBoundedBody(request, 2 * 1024 * 1024))
      : "";
    const context: FlowApiContext__Output = {
      target,
      definitionRevision: request.headers.get("x-dex-flow-definition-revision") ?? "",
      appManifestRevision: parseUint64(request.headers.get("x-dex-app-manifest-revision"), "App manifest revision"),
      configurationRevision: parseUint64(request.headers.get("x-dex-configuration-revision"), "configuration revision"),
      credentialRevision: parseUint64(request.headers.get("x-dex-credential-revision"), "credential revision"),
    };
    const result = await dispatchFlowApiRequest(request, path, purpose, context, caller, body);
    return documentResponse(
      result,
      request.method,
      token,
      request.method === "GET" && CSRF_READS.includes(path.join("/")),
    );
  } catch (error) {
    return errorResponse(error, connectionRefusalInfo(rpcErrorInfo(error)));
  }
}
