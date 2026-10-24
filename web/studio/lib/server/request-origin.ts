// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { httpError } from "./request-body";
import { authConfig, originHost } from "./auth-config";

/**
 * Studio's public origin. Next standalone may build request.url from its
 * internal listening address, so the configured origin wins; without one,
 * Host is the browser's actual HTTP authority. X-Forwarded-Host is never trusted.
 */
export function publicOrigin(request: Request, configured = process.env.SUPERVERSE_STUDIO_PUBLIC_ORIGIN): string {
  const auth = authConfig();
  if (auth.handoffEnabled || auth.exchangeEnabled) {
    const host = request.headers.get("host");
    if (host === originHost(auth.productOrigin)) return auth.productOrigin;
    if (host === originHost(auth.loginOrigin)) return auth.loginOrigin;
    throw httpError(421, "Unrecognized request authority");
  }
  if (configured) {
    const expected = new URL(configured);
    if (!["http:", "https:"].includes(expected.protocol) || expected.origin !== configured)
      throw httpError(503, "Invalid Studio public origin configuration");
    return expected.origin;
  }
  const internal = new URL(request.url);
  const host = request.headers.get("host");
  if (!host) return internal.origin;
  const forwarded = request.headers.get("x-forwarded-proto");
  const protocol = forwarded === "http" || forwarded === "https" ? `${forwarded}:` : internal.protocol;
  const external = new URL(`${protocol}//${host}`);
  if (
    external.host !== host ||
    external.username ||
    external.password ||
    external.pathname !== "/" ||
    external.search ||
    external.hash
  )
    throw httpError(400, "Invalid request authority");
  return external.origin;
}

/** The handler repeats the proxy's host selection before any business effect. */
export function requireSurface(request: Request, surface: "studio" | "login"): string {
  const auth = authConfig();
  const origin = publicOrigin(request);
  if (origin !== (surface === "studio" ? auth.productOrigin : auth.loginOrigin))
    throw httpError(403, "This endpoint is unavailable on this host");
  return origin;
}

/** Mutations must come from a Studio page on the public origin. */
export function assertSameOrigin(request: Request): void {
  if (["GET", "HEAD", "OPTIONS"].includes(request.method)) return;
  if (
    request.headers.get("origin") !== publicOrigin(request) ||
    request.headers.get("sec-fetch-site") === "cross-site"
  ) {
    throw httpError(403, "Cross-origin mutation is not allowed");
  }
}
