// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { httpError } from "@/lib/server/request-body";

function currentSecret(raw: string | undefined): string {
  let value: unknown;
  try {
    value = JSON.parse(raw ?? "");
  } catch {
    throw httpError(503, "Login credential is unavailable");
  }
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw httpError(503, "Login credential is unavailable");
  const current = (value as Record<string, unknown>).current;
  if (
    typeof current !== "string" ||
    !/^[A-Za-z0-9_-]{43}$/.test(current) ||
    Buffer.from(current, "base64url").length !== 32 ||
    Buffer.from(current, "base64url").toString("base64url") !== current
  )
    throw httpError(503, "Login credential is unavailable");
  return current;
}

export function issuerSecret(): string {
  return currentSecret(process.env.SUPERVERSE_LOGIN_HOST_ISSUER_SECRET_RING);
}

export function productSecret(): string {
  let value: unknown;
  try {
    value = JSON.parse(process.env.SUPERVERSE_LOGIN_HOST_CLIENT_SECRET_RINGS ?? "");
  } catch {
    throw httpError(503, "Login credential is unavailable");
  }
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw httpError(503, "Login credential is unavailable");
  return currentSecret(JSON.stringify((value as Record<string, unknown>).studio));
}
