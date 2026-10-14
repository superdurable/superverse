// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { callerFromRequest } from "@/lib/server/caller-envelope";
import { errorResponse, rpcErrorInfo, type RpcBackend } from "@/lib/server/grpc-client";
import { rejectTrustedBrowserHeaders } from "@/lib/server/flow/flow-api-security";
import { httpError, readJSONObject } from "@/lib/server/request-body";
import { assertSameOrigin, publicOrigin } from "@/lib/server/request-origin";
import { decodeOwnRouteRequest } from "@/lib/server/rpc/rpc-handler";
import { connectionRefusalInfo } from "@/lib/shared/problem/refusals";
import { timestampToISO } from "@/lib/shared/protobuf";
import type { RpcInput } from "@/lib/shared/rpc-methods";
import {
  clearConnectorOAuthCookie,
  createConnectorOAuthCookie,
  oauthState,
  readConnectorOAuthRoute,
  studioReturnPath,
  type ConnectorOAuthRoute,
} from "./connector-oauth-cookie";

const NO_STORE = { "cache-control": "no-store", "referrer-policy": "no-referrer" };

/**
 * POST /api/connector-oauth/start: StartTeamSpaceConnectionOAuth, whose body is its request plus
 * `returnPath`, the Studio page the callback returns to. The answer is the provider page to open,
 * and the route cookie binds the callback to this caller, login, state and page.
 */
export async function startConnectorOAuth(request: Request, backend: RpcBackend): Promise<Response> {
  try {
    if (request.method !== "POST")
      return new Response(null, { status: 405, headers: { allow: "POST", "cache-control": "no-store" } });
    assertSameOrigin(request);
    const caller = callerFromRequest(request);
    const { returnPath, ...start } = await readJSONObject(request);
    const destination = studioReturnPath(returnPath, publicOrigin(request));
    const input = decodeOwnRouteRequest(
      "StartTeamSpaceConnectionOAuth",
      start,
    ) as RpcInput<"StartTeamSpaceConnectionOAuth">;
    const started = await backend.unary("StartTeamSpaceConnectionOAuth", input, caller, request.signal);
    const expiresAt = timestampToISO(started.expiresAt) ?? "";
    const cookie = createConnectorOAuthCookie(
      request,
      caller,
      { teamSpaceId: input.teamSpaceId ?? "", connectionId: input.connectionId ?? "", returnPath: destination },
      { authorizationUrl: started.authorizationUrl, expiresAt },
    );
    const response = Response.json({ authorizationUrl: started.authorizationUrl, expiresAt }, { headers: NO_STORE });
    response.headers.append("set-cookie", cookie);
    return response;
  } catch (error) {
    return errorResponse(error, connectionRefusalInfo(rpcErrorInfo(error)));
  }
}

/** The page the authorization returns to, with its outcome in the query. */
function returnTo(route: ConnectorOAuthRoute, outcome: Record<string, string>): Response {
  const location = new URL(route.returnPath, route.origin);
  for (const [name, value] of Object.entries(outcome)) location.searchParams.set(name, value);
  const response = new Response(null, {
    status: 303,
    headers: { location: location.pathname + location.search, ...NO_STORE },
  });
  response.headers.append("set-cookie", clearConnectorOAuthCookie(route));
  return response;
}

/**
 * GET /api/connector-oauth/callback: the provider's redirect, which CompleteTeamSpaceConnectionOAuth
 * completes for the caller whose route cookie it carries. Any answer but a transient failure ends the
 * attempt: the person returns to the page that started it, with `oauth=success` and the connection,
 * or `oauth=failed` and the refusal's reason. A transient failure keeps the cookie, so a reload retries.
 */
export async function completeConnectorOAuth(request: Request, backend: RpcBackend): Promise<Response> {
  let route: ConnectorOAuthRoute | undefined;
  try {
    if (request.method !== "GET")
      return new Response(null, { status: 405, headers: { allow: "GET", "cache-control": "no-store" } });
    rejectTrustedBrowserHeaders(request);
    const caller = callerFromRequest(request);
    route = readConnectorOAuthRoute(request, caller);
    const query = new URL(request.url).searchParams;
    if (
      query.toString().length > 16 * 1024 ||
      query.getAll("code").length > 1 ||
      query.getAll("error").length > 1 ||
      Boolean(query.get("code")) === Boolean(query.get("error"))
    )
      throw httpError(400, "Invalid connector callback");
    const completed = await backend.unary(
      "CompleteTeamSpaceConnectionOAuth",
      { state: oauthState(query), code: query.get("code") ?? "", providerError: query.get("error") ?? "" },
      caller,
      request.signal,
    );
    return returnTo(route, { oauth: "success", connection: completed.connection?.connectionId ?? route.connectionId });
  } catch (error) {
    const info = connectionRefusalInfo(rpcErrorInfo(error));
    const failed = errorResponse(error, info);
    if (route && failed.status >= 400 && failed.status < 500 && failed.status !== 499)
      return returnTo(route, { oauth: "failed", ...(info ? { reason: info.reason } : {}) });
    failed.headers.set("referrer-policy", "no-referrer");
    return failed;
  }
}
