// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { NextResponse, type NextRequest } from "next/server";
import { authConfig, originHost } from "./lib/server/auth-config";

const loginPaths = new Set([
  "/",
  "/login",
  "/register",
  "/api/auth/login",
  "/api/auth/register",
  "/api/auth/session",
  "/api/auth/logout",
  "/api/auth/authorize",
  "/api/auth/continue",
  "/api/auth/exchange",
  "/favicon.ico",
  "/icon.svg",
  "/api/health",
]);

function loginSecurity(request: NextRequest): NextResponse {
  const nonce = crypto.randomUUID().replaceAll("-", "");
  const script = `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'${process.env.NODE_ENV === "development" ? " 'unsafe-eval'" : ""}`;
  const csp = [
    "default-src 'self'",
    script,
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data:",
    "font-src 'self'",
    "connect-src 'self'",
    "object-src 'none'",
    "base-uri 'none'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join("; ");
  const headers = new Headers(request.headers);
  headers.set("content-security-policy", csp);
  headers.set("x-nonce", nonce);
  let next: NextResponse;
  if (request.nextUrl.pathname === "/") {
    next = NextResponse.redirect(new URL("/login", request.url), 303);
  } else if (request.nextUrl.pathname === "/login" && request.nextUrl.search) {
    const target = new URL("/api/auth/authorize" + request.nextUrl.search, request.url);
    next = NextResponse.redirect(target, 303);
  } else {
    next = NextResponse.next({ request: { headers } });
  }
  next.headers.set("content-security-policy", csp);
  next.headers.set("x-frame-options", "DENY");
  next.headers.set("referrer-policy", "no-referrer");
  next.headers.set("cache-control", "no-store");
  return next;
}

export function proxy(request: NextRequest) {
  const config = authConfig();
  if (!config.handoffEnabled && !config.exchangeEnabled) return NextResponse.next();
  const host = request.headers.get("host");
  const pathname = request.nextUrl.pathname;
  if (host === originHost(config.loginOrigin)) {
    if (!loginPaths.has(pathname) && !pathname.startsWith("/_next/"))
      return new NextResponse(null, { status: 404, headers: { "cache-control": "no-store" } });
    return loginSecurity(request);
  }
  if (host === originHost(config.productOrigin)) {
    if (pathname === "/api/auth/authorize" || pathname === "/api/auth/continue" || pathname === "/api/auth/exchange")
      return new NextResponse(null, { status: 404, headers: { "cache-control": "no-store" } });
    return NextResponse.next();
  }
  const legacy = `localhost:${new URL(config.productOrigin).port}`;
  if (host === legacy && (request.method === "GET" || request.method === "HEAD")) {
    const destination = new URL(pathname + request.nextUrl.search, config.productOrigin);
    return NextResponse.redirect(destination, 303);
  }
  return new NextResponse(null, { status: 421, headers: { "cache-control": "no-store" } });
}
