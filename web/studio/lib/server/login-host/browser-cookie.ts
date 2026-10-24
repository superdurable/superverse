// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { createCipheriv, createDecipheriv, createHmac, randomBytes, timingSafeEqual } from "node:crypto";
import { httpError } from "@/lib/server/request-body";

type CookieKind = "product" | "login" | "delivery";

function maxAge(kind: CookieKind): number {
  return kind === "delivery" ? 60 : 300;
}

interface KeyRing {
  current: string;
  previous?: string;
  previous_valid_until?: string;
}

function keys(kind: CookieKind): Buffer[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(process.env.SUPERVERSE_LOGIN_BROWSER_KEYS ?? "");
  } catch {
    throw httpError(503, "Login browser keys are unavailable");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed))
    throw httpError(503, "Login browser keys are unavailable");
  const ring = (parsed as Record<string, unknown>)[kind === "delivery" ? "login" : kind] as KeyRing | undefined;
  if (!ring || typeof ring !== "object" || typeof ring.current !== "string")
    throw httpError(503, "Login browser keys are unavailable");
  const decode = (raw: string): Buffer => {
    if (!/^[A-Za-z0-9_-]{43}$/.test(raw)) throw httpError(503, "Login browser keys are unavailable");
    const key = Buffer.from(raw, "base64url");
    if (key.length !== 32 || key.toString("base64url") !== raw)
      throw httpError(503, "Login browser keys are unavailable");
    return key;
  };
  const accepted = [decode(ring.current)];
  if (ring.previous !== undefined) {
    const until = Date.parse(ring.previous_valid_until ?? "");
    if (!Number.isFinite(until) || until > Date.now() + 24 * 3600_000)
      throw httpError(503, "Login browser keys are unavailable");
    if (until > Date.now()) accepted.push(decode(ring.previous));
  }
  return accepted;
}

function cookieName(kind: CookieKind, secure: boolean): string {
  if (kind === "delivery") return `${secure ? "__Secure-" : ""}sv2_login_delivery`;
  return `${secure ? "__Host-" : ""}sv2_${kind === "product" ? "handoff" : "login_context"}`;
}

function cookie(kind: CookieKind, value: string, secure: boolean, age: number): string {
  const path = kind === "delivery" ? "/api/auth/continue" : "/";
  const sameSite = kind === "delivery" ? "Strict" : "Lax";
  return `${cookieName(kind, secure)}=${value}; Path=${path}; HttpOnly; SameSite=${sameSite}; Max-Age=${age}${secure ? "; Secure" : ""}`;
}

export function sealCookie(kind: CookieKind, payload: Record<string, unknown>, secure: boolean): string {
  const plaintext = Buffer.from(JSON.stringify({ ...payload, issuedAt: Date.now() }));
  if (plaintext.length > 2048) throw httpError(400, "Login request is too large");
  const nonce = randomBytes(12);
  const cipher = createCipheriv("aes-256-gcm", keys(kind)[0], nonce);
  cipher.setAAD(Buffer.from(`sv2-login-${kind}-v1`));
  const encrypted = Buffer.concat([cipher.update(plaintext), cipher.final()]);
  const value = `v1.${Buffer.concat([nonce, encrypted, cipher.getAuthTag()]).toString("base64url")}`;
  return cookie(kind, value, secure, maxAge(kind));
}

export function openCookie(kind: CookieKind, request: Request, secure: boolean): Record<string, unknown> {
  return openCookieHeader(kind, request.headers.get("cookie"), secure);
}

export function openCookieHeader(
  kind: CookieKind,
  cookieHeader: string | null,
  secure: boolean,
): Record<string, unknown> {
  const name = cookieName(kind, secure);
  const values = (cookieHeader ?? "")
    .split(";")
    .map((part) => part.trim())
    .filter((part) => part.startsWith(`${name}=`));
  if (values.length !== 1) throw httpError(400, "Login handoff is missing or expired");
  const value = values[0].slice(name.length + 1);
  if (!/^v1\.[A-Za-z0-9_-]+$/.test(value) || value.length > 4096)
    throw httpError(400, "Login handoff is missing or expired");
  const bytes = Buffer.from(value.slice(3), "base64url");
  if (bytes.length < 29) throw httpError(400, "Login handoff is missing or expired");
  const nonce = bytes.subarray(0, 12);
  const body = bytes.subarray(12, -16);
  const tag = bytes.subarray(-16);
  for (const key of keys(kind)) {
    try {
      const decipher = createDecipheriv("aes-256-gcm", key, nonce);
      decipher.setAAD(Buffer.from(`sv2-login-${kind}-v1`));
      decipher.setAuthTag(tag);
      const payload: unknown = JSON.parse(Buffer.concat([decipher.update(body), decipher.final()]).toString("utf8"));
      if (!payload || typeof payload !== "object" || Array.isArray(payload)) break;
      const issuedAt = (payload as Record<string, unknown>).issuedAt;
      if (
        typeof issuedAt !== "number" ||
        !Number.isSafeInteger(issuedAt) ||
        issuedAt > Date.now() ||
        Date.now() - issuedAt >= maxAge(kind) * 1000
      )
        break;
      return payload as Record<string, unknown>;
    } catch {
      /* Try the one previous key during its explicit overlap. */
    }
  }
  throw httpError(400, "Login handoff is missing or expired");
}

export function clearCookie(kind: CookieKind, secure: boolean): string {
  return cookie(kind, "", secure, 0);
}

/** An independent synchronizer token for logout, valid for the session lifetime. */
export function sessionCSRF(sessionToken: string): string {
  return createHmac("sha256", keys("login")[0])
    .update("sv2-login-logout-v1\0")
    .update(sessionToken)
    .digest("base64url");
}

export function validSessionCSRF(sessionToken: string, submitted: unknown): boolean {
  if (typeof submitted !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(submitted)) return false;
  const actual = Buffer.from(submitted);
  return keys("login").some((key) => {
    const expected = createHmac("sha256", key).update("sv2-login-logout-v1\0").update(sessionToken).digest("base64url");
    return timingSafeEqual(actual, Buffer.from(expected));
  });
}
