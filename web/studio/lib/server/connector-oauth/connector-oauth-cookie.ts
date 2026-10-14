// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { createHash, createHmac, timingSafeEqual } from "node:crypto";
import type { Caller } from "@/lib/server/caller-envelope";
import { browserSigningKey } from "@/lib/server/flow/flow-api-security";
import { httpError } from "@/lib/server/request-body";
import { publicOrigin } from "@/lib/server/request-origin";
import { FlowApiJson } from "@/lib/server/flow/flow-api-json";

/**
 * A signed, short-lived navigation receipt that brings the provider's OAuth redirect back to the
 * Studio page that started a TeamSpace connection's authorization. Go still authenticates the
 * caller and completes only the caller's own attempt, which its state names.
 */
const connectorOAuthCallbackPath = "/api/connector-oauth/callback";
const cookieName = "sv2_connector_oauth_route";
const MAX_AGE_SECONDS = 600;
const MAX_RETURN_PATH = 512;
// Version 7 names a TeamSpace connection and the page to return to; an earlier cookie restarts authorization.
const VERSION = 7;

/** What the callback needs of the authorization it completes. */
export interface ConnectorOAuthReturn {
  teamSpaceId: string;
  connectionId: string;
  /** A path of Studio's own origin, with its query, that the person returns to. */
  returnPath: string;
}

export type ConnectorOAuthRoute = ConnectorOAuthReturn & {
  version: typeof VERSION;
  actorId: string;
  sessionDigest: string;
  origin: string;
  stateDigest: string;
  issuedAt: number;
  expiresAt: number;
};

const ROUTE_FIELDS = [
  "version",
  "actorId",
  "sessionDigest",
  "origin",
  "stateDigest",
  "teamSpaceId",
  "connectionId",
  "returnPath",
  "issuedAt",
  "expiresAt",
];

function hash(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

function signature(value: string): Buffer {
  return createHmac("sha256", browserSigningKey())
    .update(`superverse-connector-oauth-route-v${VERSION}:${value}`)
    .digest();
}

export function oauthState(params: URLSearchParams): string {
  const values = params.getAll("state");
  if (values.length !== 1 || !values[0] || values[0].length > 1024 || /[\u0000-\u001f\u007f]/.test(values[0]))
    throw httpError(400, "Invalid OAuth state");
  return values[0];
}

/** The path, with its query, of a Studio page on origin; anything else is refused. */
export function studioReturnPath(value: unknown, origin: string): string {
  if (
    typeof value !== "string" ||
    value.length > MAX_RETURN_PATH ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    /[\\\u0000-\u001f\u007f]/.test(value)
  )
    throw httpError(400, "Invalid return path");
  const url = new URL(value, origin);
  if (url.origin !== origin || url.hash) throw httpError(400, "Invalid return path");
  return url.pathname + url.search;
}

function routeCookie(route: Pick<ConnectorOAuthRoute, "origin" | "expiresAt">, value: string, age: number): string {
  return `${cookieName}=${value}; Path=${connectorOAuthCallbackPath}; HttpOnly; SameSite=Lax; Max-Age=${age}; Expires=${new Date(age === 0 ? 0 : route.expiresAt * 1000).toUTCString()}${route.origin.startsWith("https:") ? "; Secure" : ""}`;
}

/**
 * The route cookie of an authorization Go started. Go owns the provider policy, so the BFF checks
 * only that the authorization URL is an absolute http or https URL without userinfo or fragment
 * that carries one state.
 */
export function createConnectorOAuthCookie(
  request: Request,
  caller: Caller,
  destination: ConnectorOAuthReturn,
  started: { authorizationUrl: string; expiresAt: string },
): string {
  const origin = publicOrigin(request);
  let authorization: URL;
  try {
    authorization = new URL(started.authorizationUrl);
  } catch {
    throw httpError(502, "Invalid provider authorization destination");
  }
  if (
    !["http:", "https:"].includes(authorization.protocol) ||
    authorization.username ||
    authorization.password ||
    authorization.hash
  )
    throw httpError(502, "Invalid provider authorization destination");
  const state = oauthState(authorization.searchParams);
  const issuedAt = Math.floor(Date.now() / 1000);
  const expiresAt = Math.min(issuedAt + MAX_AGE_SECONDS, Math.floor(Date.parse(started.expiresAt) / 1000));
  if (!Number.isSafeInteger(expiresAt) || expiresAt <= issuedAt)
    throw httpError(502, "Connector authorization expired");
  const route: ConnectorOAuthRoute = {
    version: VERSION,
    actorId: caller.accountId,
    sessionDigest: hash(caller.sessionToken),
    origin,
    stateDigest: hash(state),
    teamSpaceId: destination.teamSpaceId,
    connectionId: destination.connectionId,
    returnPath: studioReturnPath(destination.returnPath, origin),
    issuedAt,
    expiresAt,
  };
  const payload = Buffer.from(JSON.stringify(route)).toString("base64url");
  return routeCookie(route, `${payload}.${signature(payload).toString("base64url")}`, expiresAt - issuedAt);
}

/** The callback's route cookie, which must be the caller's, for this state, unexpired and signed. */
export function readConnectorOAuthRoute(request: Request, caller: Caller): ConnectorOAuthRoute {
  const invalidRoute = () => httpError(403, "Connector authorization is missing or expired; start authorization again");
  const matching = (request.headers.get("cookie") ?? "")
    .split(";")
    .map((item) => item.trim())
    .filter((item) => item.startsWith(`${cookieName}=`));
  if (matching.length !== 1) throw invalidRoute();
  const raw = matching[0].slice(cookieName.length + 1);
  if (raw.length > 4096 || !/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]{43}$/.test(raw)) throw invalidRoute();
  const [payload, mac] = raw.split(".");
  if (!timingSafeEqual(Buffer.from(mac, "base64url"), signature(payload))) throw invalidRoute();
  const value = new FlowApiJson(Buffer.from(payload, "base64url").toString("utf8"), ROUTE_FIELDS);
  if (value.integer("version") !== VERSION) throw invalidRoute();
  const route: ConnectorOAuthRoute = {
    version: VERSION,
    actorId: value.string("actorId"),
    sessionDigest: value.string("sessionDigest"),
    origin: value.string("origin"),
    stateDigest: value.string("stateDigest"),
    teamSpaceId: value.string("teamSpaceId"),
    connectionId: value.string("connectionId"),
    returnPath: value.string("returnPath"),
    issuedAt: value.integer("issuedAt"),
    expiresAt: value.integer("expiresAt"),
  };
  const now = Math.floor(Date.now() / 1000);
  if (
    route.actorId !== caller.accountId ||
    route.sessionDigest !== hash(caller.sessionToken) ||
    route.origin !== publicOrigin(request) ||
    route.stateDigest !== hash(oauthState(new URL(request.url).searchParams)) ||
    route.issuedAt > now ||
    route.expiresAt <= now ||
    route.expiresAt - route.issuedAt > MAX_AGE_SECONDS
  )
    throw invalidRoute();
  return route;
}

export function clearConnectorOAuthCookie(route: Pick<ConnectorOAuthRoute, "origin" | "expiresAt">): string {
  return routeCookie(route, "", 0);
}
