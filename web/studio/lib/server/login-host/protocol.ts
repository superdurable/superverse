// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { createHash, randomBytes, timingSafeEqual } from "node:crypto";
import { httpError } from "@/lib/server/request-body";
import { authConfig, loginProductByOrigin } from "@/lib/server/auth-config";

export interface LoginContext {
  returnTo: string;
  state: string;
  challenge: string;
  csrf: string;
}

export interface ProductHandoff {
  state: string;
  verifier: string;
  origin: string;
  next: string;
}

export const callbackPath = "/api/auth/callback";
const forbiddenNext = new Set([
  "/login",
  "/register",
  "/api/auth/start",
  callbackPath,
  "/api/auth/authorize",
  "/api/auth/continue",
  "/api/auth/exchange",
]);

export function randomPart(): string {
  return randomBytes(32).toString("base64url");
}

export function challengeFor(verifier: string): string {
  return createHash("sha256").update(verifier, "ascii").digest("base64url");
}

export function validPart(value: unknown): value is string {
  if (typeof value !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(value)) return false;
  const decoded = Buffer.from(value, "base64url");
  return decoded.length === 32 && decoded.toString("base64url") === value;
}

export function samePart(left: string, right: string): boolean {
  const a = Buffer.from(left),
    b = Buffer.from(right);
  return a.length === b.length && timingSafeEqual(a, b);
}

export function safeNext(value: unknown, origin: string): string {
  if (
    typeof value !== "string" ||
    value.length > 1024 ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    /[\\\u0000-\u001f\u007f]/.test(value)
  )
    throw httpError(400, "Invalid return destination");
  const parsed = new URL(value, origin);
  if (
    parsed.origin !== origin ||
    parsed.hash ||
    forbiddenNext.has(parsed.pathname) ||
    parsed.pathname.startsWith("/api/auth/")
  )
    throw httpError(400, "Invalid return destination");
  return parsed.pathname + parsed.search;
}

export function parseLoginQuery(
  query: URLSearchParams,
): { returnTo: string; state?: string; challenge?: string } | null {
  if (
    ![...query.keys()].every((key) =>
      ["return_to", "state", "code_challenge", "code_challenge_method"].includes(key),
    ) ||
    [...query.keys()].some((key) => query.getAll(key).length !== 1)
  )
    throw httpError(400, "Invalid login request");
  const target = query.get("return_to");
  if (target === null) {
    if (query.size !== 0) throw httpError(400, "Invalid login request");
    return null;
  }
  if (!loginProductByOrigin(target)) throw httpError(400, "Invalid login request");
  const state = query.get("state"),
    challenge = query.get("code_challenge"),
    method = query.get("code_challenge_method");
  if (state === null && challenge === null && method === null) return { returnTo: target };
  if (!validPart(state) || !validPart(challenge) || method !== "S256") throw httpError(400, "Invalid login request");
  return { returnTo: target, state, challenge };
}

export function readLoginContext(raw: Record<string, unknown>): LoginContext {
  if (
    typeof raw.returnTo !== "string" ||
    !loginProductByOrigin(raw.returnTo) ||
    !validPart(raw.state) ||
    !validPart(raw.challenge) ||
    !validPart(raw.csrf)
  )
    throw httpError(400, "Login handoff is missing or expired");
  return { returnTo: raw.returnTo, state: raw.state, challenge: raw.challenge, csrf: raw.csrf };
}

export function readProductHandoff(raw: Record<string, unknown>): ProductHandoff {
  const config = authConfig();
  if (!validPart(raw.state) || !validPart(raw.verifier) || raw.origin !== config.productOrigin)
    throw httpError(400, "Login handoff is missing or expired");
  return {
    state: raw.state,
    verifier: raw.verifier,
    origin: raw.origin,
    next: safeNext(raw.next, config.productOrigin),
  };
}

export function requireCSRF(request: Request, expected: string, submitted: unknown, origin: string): void {
  if (
    request.headers.get("origin") !== origin ||
    request.headers.get("host") !== new URL(origin).host ||
    request.headers.get("sec-fetch-site") === "cross-site" ||
    !validPart(submitted) ||
    !samePart(submitted, expected)
  )
    throw httpError(403, "Login request could not be verified");
}

export function redirect303(destination: string, cookies: string[] = []): Response {
  const response = new Response(null, {
    status: 303,
    headers: {
      location: destination,
      "cache-control": "no-store",
      "referrer-policy": "no-referrer",
      "x-frame-options": "DENY",
    },
  });
  for (const value of cookies) response.headers.append("set-cookie", value);
  return response;
}
