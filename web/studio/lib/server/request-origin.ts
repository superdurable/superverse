// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { httpError } from "./request-body";

/**
 * Studio's public origin. Next standalone may build request.url from its
 * internal listening address, so the configured origin wins; without one,
 * Host is the browser's actual HTTP authority. X-Forwarded-Host is never trusted.
 */
export function publicOrigin(request: Request, configured = process.env.SUPERVERSE_STUDIO_PUBLIC_ORIGIN): string {
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
