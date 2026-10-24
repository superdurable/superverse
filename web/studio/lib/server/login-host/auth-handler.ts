// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomUUID } from "node:crypto";
import { timestampToISO } from "@/lib/shared/protobuf";
import { callerEnvelope, callerFromRequest, clearedSessionCookie, sessionCookie } from "@/lib/server/caller-envelope";
import { callAccountRpc, errorResponse, type RpcBackend } from "@/lib/server/grpc-client";
import { httpError, readJSONObject } from "@/lib/server/request-body";
import { assertSameOrigin, publicOrigin, requireSurface } from "@/lib/server/request-origin";
import { authConfig } from "@/lib/server/auth-config";
import { openCookie, validSessionCSRF } from "./browser-cookie";
import { readLoginContext, requireCSRF } from "./protocol";
import { issuerSecret } from "./secrets";

const NO_STORE = { "cache-control": "no-store" };

/**
 * /api/auth/{register,login,logout,session}: the only caller of AccountService and the only
 * writer of the login cookie. The cookie carries the AccountLogin the backend issued.
 */
export async function handleAuthRequest(request: Request, action: string, backend: RpcBackend): Promise<Response> {
  try {
    const signsIn = action === "register" || action === "login";
    if (
      !(signsIn || action === "logout" || action === "session") ||
      request.method !== (action === "session" ? "GET" : "POST")
    )
      return new Response(null, { status: 404 });
    const config = authConfig();
    const origin = publicOrigin(request);
    const onLoginHost = origin === config.loginOrigin && (config.handoffEnabled || config.exchangeEnabled);
    requireSurface(request, onLoginHost ? "login" : "studio");
    if (signsIn && !onLoginHost && config.handoffEnabled) throw httpError(403, "Password sign-in is on the login host");
    if (signsIn && onLoginHost && !config.handoffEnabled) throw httpError(503, "Login handoff is disabled");
    assertSameOrigin(request);
    const secure = new URL(origin).protocol === "https:";
    if (signsIn) {
      const body = await readJSONObject(request);
      // Empty passwords are valid; the request body limit bounds their size.
      if (typeof body.email !== "string" || !body.email.trim() || typeof body.password !== "string")
        throw httpError(400, "Email and password must be strings, and email is required");
      const credentials = { email: body.email, password: body.password };
      if (onLoginHost) {
        const context = readLoginContext(openCookie("login", request, secure));
        requireCSRF(request, context.csrf, body.csrfToken, origin);
      }
      const { login } =
        action === "register"
          ? onLoginHost
            ? await callAccountRpc(
                "RegisterAccount",
                { ...credentials, requestId: request.headers.get("idempotency-key") ?? randomUUID() },
                null,
                "login",
                issuerSecret(),
                request.signal,
              )
            : await backend.unary(
                "RegisterAccount",
                { ...credentials, requestId: request.headers.get("idempotency-key") ?? randomUUID() },
                null,
                request.signal,
              )
          : onLoginHost
            ? await callAccountRpc("Login", credentials, null, "login", issuerSecret(), request.signal)
            : await backend.unary("Login", credentials, null, request.signal);
      if (!login) throw httpError(503, "Invalid account session response");
      return Response.json(
        { account: login.account },
        {
          headers: {
            ...NO_STORE,
            "set-cookie": sessionCookie(
              callerEnvelope(login),
              secure,
              timestampToISO(login.expiresAt),
              onLoginHost ? "login" : "studio",
            ),
          },
        },
      );
    }
    if (action === "logout") {
      if (onLoginHost) {
        const caller = callerFromRequest(request, "login");
        const body = await readJSONObject(request);
        if (
          request.headers.get("origin") !== origin ||
          request.headers.get("host") !== new URL(origin).host ||
          !validSessionCSRF(caller.sessionToken, body.csrfToken)
        )
          throw httpError(403, "Login request could not be verified");
        await callAccountRpc("Logout", {}, caller, "login", issuerSecret(), request.signal);
      } else {
        await backend.unary("Logout", {}, callerFromRequest(request), request.signal);
      }
      return Response.json(
        {},
        { headers: { ...NO_STORE, "set-cookie": clearedSessionCookie(secure, onLoginHost ? "login" : "studio") } },
      );
    }
    const { account } = onLoginHost
      ? await callAccountRpc(
          "GetCurrentAccount",
          {},
          callerFromRequest(request, "login"),
          "login",
          issuerSecret(),
          request.signal,
        )
      : await backend.unary("GetCurrentAccount", {}, callerFromRequest(request), request.signal);
    return Response.json(account, { headers: NO_STORE });
  } catch (error) {
    return errorResponse(error);
  }
}
