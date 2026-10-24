// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { CallerContext__Output } from "@/lib/generated/protobuf/superverse/v2/CallerContext";
import type { AccountLogin__Output } from "@/lib/generated/protobuf/superverse/v2/AccountLogin";
import { isRecord } from "@/lib/shared/json";
import { httpError } from "./request-body";
import { authConfig } from "./auth-config";
import { publicOrigin } from "./request-origin";

/**
 * The login cookie carries the Go-issued identity fields as a base64url JSON
 * envelope. Go verifies every field against the signed session token on each
 * call; this module only shapes and bounds the envelope.
 */
export type Caller = CallerContext__Output;
type Surface = "studio" | "login";
const LEGACY_SESSION_COOKIE = "sv2_session";
const ENVELOPE_PREFIX = "sv2.";
const MAX_ENVELOPE_LENGTH = 4096;

function isCaller(value: unknown): value is Caller {
  if (!isRecord(value)) return false;
  const { accountId, canonicalEmail, sessionToken } = value;
  return (
    Object.keys(value).sort().join(",") === "accountId,canonicalEmail,sessionToken" &&
    typeof accountId === "string" &&
    accountId.length > 0 &&
    accountId.length <= 128 &&
    typeof canonicalEmail === "string" &&
    canonicalEmail.length <= 320 &&
    canonicalEmail.includes("@") &&
    typeof sessionToken === "string" &&
    /^[A-Za-z0-9._~-]{16,3072}$/.test(sessionToken)
  );
}

export function callerEnvelope(login: AccountLogin__Output): string {
  const caller = {
    accountId: login.account?.accountId,
    canonicalEmail: login.account?.email,
    sessionToken: login.sessionToken,
  };
  if (!isCaller(caller)) throw httpError(503, "Invalid account session response");
  const envelope = ENVELOPE_PREFIX + Buffer.from(JSON.stringify(caller)).toString("base64url");
  if (envelope.length > MAX_ENVELOPE_LENGTH) throw httpError(503, "Account session exceeds cookie budget");
  return envelope;
}

export function callerFromEnvelope(envelope: string | undefined): Caller | null {
  if (!envelope || envelope.length > MAX_ENVELOPE_LENGTH || !/^sv2\.[A-Za-z0-9_-]+$/.test(envelope)) return null;
  try {
    const value: unknown = JSON.parse(
      Buffer.from(envelope.slice(ENVELOPE_PREFIX.length), "base64url").toString("utf8"),
    );
    return isCaller(value) ? value : null;
  } catch {
    return null;
  }
}

/** The one session cookie value; a missing, repeated or malformed cookie reads as ''. */
function cookieName(surface: Surface): string {
  const config = authConfig();
  if (!config.handoffEnabled && !config.exchangeEnabled) return LEGACY_SESSION_COOKIE;
  const secure = (surface === "login" ? config.loginOrigin : config.productOrigin).startsWith("https:");
  return `${secure ? "__Host-" : ""}sv2_${surface === "login" ? "login" : "session"}`;
}

function parseSessionCookie(cookieHeader: string, surface: Surface): string {
  const name = cookieName(surface);
  const values = cookieHeader
    .split(";")
    .map((value) => value.trim())
    .filter((value) => value.startsWith(`${name}=`));
  if (values.length !== 1) return "";
  const token = values[0].slice(name.length + 1);
  return /^[A-Za-z0-9._~-]{16,4096}$/.test(token) ? token : "";
}

/** The caller a Cookie header signs in, or null when it carries no valid login. */
export function callerFromCookieHeader(cookieHeader: string | null, surface: Surface = "studio"): Caller | null {
  return callerFromEnvelope(parseSessionCookie(cookieHeader ?? "", surface));
}

export function callerFromRequest(request: Request, surface?: Surface): Caller {
  const selected = surface ?? (publicOrigin(request) === authConfig().loginOrigin ? "login" : "studio");
  const caller = callerFromCookieHeader(request.headers.get("cookie"), selected);
  if (!caller) throw httpError(401, "Sign in to continue");
  return caller;
}

export function sessionCookie(
  envelope: string,
  secure: boolean,
  expires: string | null,
  surface: Surface = "studio",
): string {
  return `${cookieName(surface)}=${envelope}; Path=/; HttpOnly; SameSite=Lax${secure ? "; Secure" : ""}${expires ? `; Expires=${new Date(expires).toUTCString()}` : ""}`;
}

export function clearedSessionCookie(secure: boolean, surface: Surface = "studio"): string {
  return `${cookieName(surface)}=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0${secure ? "; Secure" : ""}`;
}
