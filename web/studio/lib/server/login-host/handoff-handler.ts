// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { createHash, randomUUID } from "node:crypto";
import { callerEnvelope, callerFromRequest, sessionCookie } from "@/lib/server/caller-envelope";
import { callAccountRpc } from "@/lib/server/grpc-client";
import { httpError, readBoundedBody } from "@/lib/server/request-body";
import { requireSurface } from "@/lib/server/request-origin";
import { authConfig, loginProductByRedirectURI, originHost } from "@/lib/server/auth-config";
import { clearCookie, openCookie, sealCookie } from "./browser-cookie";
import { postExchange } from "./exchange-transport";
import { issuerSecret, productSecret } from "./secrets";
import {
  callbackPath,
  challengeFor,
  parseLoginQuery,
  randomPart,
  readLoginContext,
  readProductHandoff,
  redirect303,
  requireCSRF,
  safeNext,
  samePart,
  validPart,
} from "./protocol";
import type { AccountLogin__Output } from "@/lib/generated/protobuf/superverse/v2/AccountLogin";
import { isRecord } from "@/lib/shared/json";
import { timestampToISO } from "@/lib/shared/protobuf";

function feature(on: boolean): void {
  if (!on) throw httpError(404, "Login handoff is disabled");
}
function secure(origin: string): boolean {
  return origin.startsWith("https:");
}
function badMethod(method: string): Response {
  return new Response(null, { status: 405, headers: { allow: method, "cache-control": "no-store" } });
}

/** Product GET: establish one browser-bound state/verifier before visiting login. */
export function startHandoff(request: Request): Response {
  if (request.method !== "GET") return badMethod("GET");
  const config = authConfig();
  feature(config.handoffEnabled);
  requireSurface(request, "studio");
  const query = new URL(request.url).searchParams;
  if ([...query.keys()].some((key) => key !== "next") || query.getAll("next").length > 1)
    throw httpError(400, "Invalid return destination");
  const next = safeNext(query.get("next") ?? "/", config.productOrigin);
  const state = randomPart(),
    verifier = randomPart();
  const login = new URL("/login", config.loginOrigin);
  login.searchParams.set("return_to", config.productOrigin);
  login.searchParams.set("state", state);
  login.searchParams.set("code_challenge", challengeFor(verifier));
  login.searchParams.set("code_challenge_method", "S256");
  const handoff = sealCookie(
    "product",
    { state, verifier, origin: config.productOrigin, next },
    secure(config.productOrigin),
  );
  return redirect303(login.toString(), [handoff]);
}

/** Login GET: validate the full request, or bootstrap a product-owned state. */
export function prepareLogin(request: Request): Response {
  if (request.method !== "GET") return badMethod("GET");
  const config = authConfig();
  feature(config.handoffEnabled);
  requireSurface(request, "login");
  const parsed = parseLoginQuery(new URL(request.url).searchParams);
  if (!parsed) return redirect303("/login");
  if (!parsed.state || !parsed.challenge) return redirect303(`${parsed.returnTo}/api/auth/start`);
  const csrf = randomPart();
  const context = sealCookie(
    "login",
    { returnTo: parsed.returnTo, state: parsed.state, challenge: parsed.challenge, csrf },
    secure(config.loginOrigin),
  );
  return redirect303("/login", [context]);
}

/** Login POST: the logged-in browser explicitly grants its selected product. */
export async function authorizeLogin(request: Request): Promise<Response> {
  if (request.method !== "POST") return badMethod("POST");
  const config = authConfig();
  feature(config.handoffEnabled);
  requireSurface(request, "login");
  const context = readLoginContext(openCookie("login", request, secure(config.loginOrigin)));
  const bytes = await readBoundedBody(request, 4096);
  if (!request.headers.get("content-type")?.startsWith("application/x-www-form-urlencoded"))
    throw httpError(400, "Invalid login request");
  const fields = new URLSearchParams(Buffer.from(bytes).toString("utf8"));
  if ([...fields.keys()].some((key) => key !== "csrf_token") || fields.getAll("csrf_token").length !== 1)
    throw httpError(400, "Invalid login request");
  requireCSRF(request, context.csrf, fields.get("csrf_token"), config.loginOrigin);
  const caller = callerFromRequest(request, "login");
  const issued = await callAccountRpc(
    "CreateLoginAuthorizationCode",
    {
      productOrigin: context.returnTo,
      redirectUri: context.returnTo + callbackPath,
      codeChallenge: context.challenge,
    },
    caller,
    "login",
    issuerSecret(),
    request.signal,
  );
  const code = issued.authorizationCode?.code;
  if (typeof code !== "string" || !/^lh1_[A-Za-z0-9_-]{43}$/.test(code))
    throw httpError(503, "Login handoff unavailable");
  const delivery = sealCookie(
    "delivery",
    {
      code,
      state: context.state,
      redirectUri: context.returnTo + callbackPath,
      sessionDigest: createHash("sha256").update(caller.sessionToken).digest("base64url"),
    },
    secure(config.loginOrigin),
  );
  return new Response(null, {
    status: 204,
    headers: {
      "cache-control": "no-store",
      "referrer-policy": "no-referrer",
      "set-cookie": delivery,
    },
  });
}

/** Login GET: consume the browser-only delivery cookie and navigate to its fixed callback. */
export function continueLogin(request: Request): Response {
  if (request.method !== "GET") return badMethod("GET");
  const config = authConfig();
  feature(config.handoffEnabled);
  requireSurface(request, "login");
  const clear = clearCookie("delivery", secure(config.loginOrigin));
  try {
    if (new URL(request.url).search) throw httpError(400, "Invalid login delivery");
    const delivered = openCookie("delivery", request, secure(config.loginOrigin));
    const caller = callerFromRequest(request, "login");
    const expectedDigest = createHash("sha256").update(caller.sessionToken).digest("base64url");
    if (
      typeof delivered.code !== "string" ||
      !/^lh1_[A-Za-z0-9_-]{43}$/.test(delivered.code) ||
      !validPart(delivered.state) ||
      typeof delivered.redirectUri !== "string" ||
      !loginProductByRedirectURI(delivered.redirectUri) ||
      !validPart(delivered.sessionDigest) ||
      !samePart(delivered.sessionDigest, expectedDigest)
    )
      throw httpError(400, "Invalid login delivery");
    const callback = new URL(delivered.redirectUri);
    callback.searchParams.set("code", delivered.code);
    callback.searchParams.set("state", delivered.state);
    return redirect303(callback.toString(), [clear]);
  } catch {
    return redirect303("/login", [clear]);
  }
}

function basicCredential(request: Request): { id: string; secret: string } {
  const value = request.headers.get("authorization") ?? "";
  if (!/^Basic [A-Za-z0-9+/=]{1,256}$/.test(value)) throw httpError(401, "invalid_client");
  const encoded = value.slice(6);
  const decoded = Buffer.from(encoded, "base64");
  if (decoded.toString("base64") !== encoded) throw httpError(401, "invalid_client");
  const text = decoded.toString("utf8");
  const divider = text.indexOf(":");
  if (divider < 1 || divider !== text.lastIndexOf(":")) throw httpError(401, "invalid_client");
  const id = text.slice(0, divider),
    secret = text.slice(divider + 1);
  if (!/^[a-z][a-z0-9-]{0,29}$/.test(id) || !validPart(secret)) throw httpError(401, "invalid_client");
  return { id, secret };
}

function exchangeFailure(status: number, requestID: string, reason?: string): Response {
  const code =
    status === 401
      ? "invalid_client"
      : status === 403
        ? "browser_exchange_forbidden"
        : status === 429
          ? "rate_limited"
          : status === 503 || status >= 500
            ? "exchange_unavailable"
            : reason === "invalid_request"
              ? "invalid_request"
              : "invalid_grant";
  const result = Response.json(
    { error: code, request_id: requestID },
    {
      status: status === 504 ? 503 : status,
      headers: { "cache-control": "no-store", "referrer-policy": "no-referrer", "x-request-id": requestID },
    },
  );
  if (status === 429) result.headers.set("retry-after", "1");
  return result;
}

/** Login POST: narrow server-only transport adapter to the typed account RPC. */
export async function exchangeLogin(request: Request): Promise<Response> {
  const requestID = randomUUID();
  try {
    if (request.method !== "POST") return badMethod("POST");
    const config = authConfig();
    feature(config.exchangeEnabled);
    requireSurface(request, "login");
    if (request.headers.get("origin") || request.headers.get("cookie"))
      throw httpError(403, "browser_exchange_forbidden");
    if (request.headers.get("content-type") !== "application/json") throw httpError(400, "invalid_request");
    const { id, secret } = basicCredential(request);
    const body = await readBoundedBody(request, 4096);
    let value: unknown;
    try {
      value = JSON.parse(Buffer.from(body).toString("utf8"));
    } catch {
      throw httpError(400, "invalid_request");
    }
    if (
      !isRecord(value) ||
      Object.keys(value).sort().join(",") !== "code,code_verifier,redirect_uri" ||
      typeof value.code !== "string" ||
      !/^lh1_[A-Za-z0-9_-]{43}$/.test(value.code) ||
      !validPart(value.code_verifier) ||
      typeof value.redirect_uri !== "string" ||
      loginProductByRedirectURI(value.redirect_uri)?.id !== id
    )
      throw httpError(400, "invalid_request");
    const { login } = await callAccountRpc(
      "ExchangeLoginAuthorizationCode",
      {
        code: value.code,
        codeVerifier: value.code_verifier,
        redirectUri: value.redirect_uri,
      },
      null,
      id,
      secret,
      request.signal,
    );
    if (!login) throw httpError(503, "exchange_unavailable");
    return Response.json(
      { login },
      { headers: { "cache-control": "no-store", "referrer-policy": "no-referrer", "x-request-id": requestID } },
    );
  } catch (error) {
    const status = error instanceof Error && "status" in error && typeof error.status === "number" ? error.status : 503;
    const reason = error instanceof Error && error.message === "invalid_request" ? "invalid_request" : undefined;
    return exchangeFailure(status === 413 ? 400 : status === 421 ? 403 : status, requestID, reason);
  }
}

function decodeLogin(value: unknown): AccountLogin__Output {
  if (
    !isRecord(value) ||
    !isRecord(value.account) ||
    !isRecord(value.expiresAt) ||
    typeof value.account.accountId !== "string" ||
    typeof value.account.email !== "string" ||
    typeof value.account.isAdmin !== "boolean" ||
    typeof value.sessionToken !== "string" ||
    typeof value.sessionId !== "string" ||
    !/^login-[A-Za-z0-9_-]{24}$/.test(value.sessionId) ||
    typeof value.expiresAt.seconds !== "string" ||
    !/^[0-9]+$/.test(value.expiresAt.seconds) ||
    typeof value.expiresAt.nanos !== "number" ||
    !Number.isSafeInteger(value.expiresAt.nanos)
  )
    throw httpError(503, "Login exchange response is invalid");
  return {
    account: { accountId: value.account.accountId, email: value.account.email, isAdmin: value.account.isAdmin },
    sessionToken: value.sessionToken,
    sessionId: value.sessionId,
    expiresAt: { seconds: value.expiresAt.seconds, nanos: value.expiresAt.nanos },
  };
}

/** Product GET: validate browser state before one non-retried server exchange. */
export async function completeHandoff(request: Request): Promise<Response> {
  if (request.method !== "GET") return badMethod("GET");
  const config = authConfig();
  feature(config.exchangeEnabled);
  requireSurface(request, "studio");
  const clear = clearCookie("product", secure(config.productOrigin));
  try {
    const query = new URL(request.url).searchParams;
    if (
      [...query.keys()].some((key) => key !== "code" && key !== "state") ||
      query.getAll("code").length !== 1 ||
      query.getAll("state").length !== 1
    )
      throw httpError(400, "Invalid login callback");
    const code = query.get("code"),
      state = query.get("state");
    if (!code || !/^lh1_[A-Za-z0-9_-]{43}$/.test(code) || !validPart(state))
      throw httpError(400, "Invalid login callback");
    const handoff = readProductHandoff(openCookie("product", request, secure(config.productOrigin)));
    if (!samePart(state, handoff.state)) throw httpError(400, "Invalid login callback");
    const basic = Buffer.from(`studio:${productSecret()}`).toString("base64");
    const response = await postExchange(
      config.exchangeURL,
      originHost(config.loginOrigin),
      `Basic ${basic}`,
      JSON.stringify({
        code,
        code_verifier: handoff.verifier,
        redirect_uri: config.productOrigin + callbackPath,
      }),
      request.signal,
    );
    if (response.status !== 200) throw httpError(503, "Login exchange was not completed");
    const value: unknown = JSON.parse(response.body.toString("utf8"));
    if (!isRecord(value)) throw httpError(503, "Login exchange response is invalid");
    const login = decodeLogin(value.login);
    const expiresAt = timestampToISO(login.expiresAt);
    if (!expiresAt) throw httpError(503, "Login exchange response is invalid");
    const session = sessionCookie(callerEnvelope(login), secure(config.productOrigin), expiresAt, "studio");
    return redirect303(handoff.next, [session, clear]);
  } catch {
    return redirect303("/login/restart", [clear]);
  }
}
