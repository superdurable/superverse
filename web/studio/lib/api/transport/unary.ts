// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { RpcInput, RpcOutput, UnaryMethod } from "@/lib/shared/rpc-methods";
import { responseError } from "./api-error";

/** Posts one JSON body to a same-origin Studio route; a non-2xx response throws ApiError. Nothing is retried here. */
export async function postJson<T>(
  path: string,
  body: object,
  { signal, headers }: { signal?: AbortSignal; headers?: Record<string, string> } = {},
): Promise<T> {
  const response = await fetch(path, {
    method: "POST",
    cache: "no-store",
    signal,
    headers: { ...headers, "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw await responseError(response);
  return response.json();
}

/** One business RPC through /api/rpc/<Method>. Method names are unique across services, so the name routes it. */
export function unary<M extends UnaryMethod>(
  method: M,
  input: RpcInput<M>,
  { signal }: { signal: AbortSignal },
): Promise<RpcOutput<M>> {
  return postJson(`/api/rpc/${method}`, input, { signal });
}
