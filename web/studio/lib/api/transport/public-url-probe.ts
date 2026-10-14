// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/**
 * Confirms that this browser can reach a public application URL.
 *
 * Cross-origin application responses are intentionally opaque. A resolved request proves
 * browser DNS, TLS, and transport reachability; the Control Plane owns HTTP status checks.
 */
export async function probePublicUrl(url: string, signal: AbortSignal): Promise<void> {
  const parsed = new URL(url);
  if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
    throw new TypeError("Public application URL must use HTTP or HTTPS");
  }
  await fetch(parsed, {
    method: "GET",
    mode: "no-cors",
    credentials: "omit",
    cache: "no-store",
    redirect: "follow",
    referrerPolicy: "no-referrer",
    signal,
  });
}
