// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { randomUUID } from "node:crypto";
import { timestampToISO } from "@/lib/shared/protobuf";
import { callerEnvelope, callerFromRequest, clearedSessionCookie, sessionCookie } from "./caller-envelope";
import { errorResponse, type RpcBackend } from "./grpc-client";
import { httpError, readJSONObject } from "./request-body";
import { assertSameOrigin, publicOrigin } from "./request-origin";

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
    assertSameOrigin(request);
    const secure = new URL(publicOrigin(request)).protocol === "https:";
    if (signsIn) {
      const body = await readJSONObject(request);
      // Empty passwords are valid; the request body limit bounds their size.
      if (typeof body.email !== "string" || !body.email.trim() || typeof body.password !== "string")
        throw httpError(400, "Email and password must be strings, and email is required");
      const credentials = { email: body.email, password: body.password };
      const { login } =
        action === "register"
          ? await backend.unary(
              "RegisterAccount",
              { ...credentials, requestId: request.headers.get("idempotency-key") ?? randomUUID() },
              null,
              request.signal,
            )
          : await backend.unary("Login", credentials, null, request.signal);
      if (!login) throw httpError(503, "Invalid account session response");
      return Response.json(
        { account: login.account },
        {
          headers: {
            ...NO_STORE,
            "set-cookie": sessionCookie(callerEnvelope(login), secure, timestampToISO(login.expiresAt)),
          },
        },
      );
    }
    if (action === "logout") {
      await backend.unary("Logout", {}, callerFromRequest(request), request.signal);
      return Response.json({}, { headers: { ...NO_STORE, "set-cookie": clearedSessionCookie(secure) } });
    }
    const { account } = await backend.unary("GetCurrentAccount", {}, callerFromRequest(request), request.signal);
    return Response.json(account, { headers: NO_STORE });
  } catch (error) {
    return errorResponse(error);
  }
}
