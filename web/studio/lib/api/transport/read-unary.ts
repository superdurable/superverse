// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { delay } from "@/lib/shared/async";
import type { RpcInput, RpcOutput, UnaryMethod } from "@/lib/shared/rpc-methods";
import { ApiError } from "./api-error";
import { unary } from "./unary";

/** Only the generated Get/List business RPCs may use automatic read retries. */
type ReadMethod = Extract<UnaryMethod, `Get${string}` | `List${string}`>;

const MAX_RETRIES = 3;
const BACKOFF_MILLISECONDS = 500;
const READ_BUDGET_MILLISECONDS = 60_000;

/** One read, with three transient retries inside a shared deadline, canceled by its caller. */
export async function readUnary<M extends ReadMethod>(
  method: M,
  input: RpcInput<M>,
  { signal }: { signal: AbortSignal },
): Promise<RpcOutput<M>> {
  const deadline = new AbortController();
  const timer = setTimeout(
    () => deadline.abort(new DOMException("Read request timed out", "TimeoutError")),
    READ_BUDGET_MILLISECONDS,
  );
  const requestSignal = AbortSignal.any([signal, deadline.signal]);
  let lastFailure: unknown;
  try {
    for (let attempt = 0; ; attempt++) {
      signal.throwIfAborted();
      if (deadline.signal.aborted) throw lastFailure ?? deadline.signal.reason;
      try {
        const result = await unary(method, input, { signal: requestSignal });
        requestSignal.throwIfAborted();
        return result;
      } catch (reason) {
        signal.throwIfAborted();
        if (deadline.signal.aborted) throw lastFailure ?? reason;
        if (!(reason instanceof ApiError) || ![503, 504].includes(reason.status) || attempt === MAX_RETRIES)
          throw reason;
        lastFailure = reason;
        // Jitter keeps mounted readers from retrying together after a shared outage.
        await delay(BACKOFF_MILLISECONDS * 2 ** attempt * (0.75 + Math.random() * 0.5), requestSignal);
      }
    }
  } finally {
    clearTimeout(timer);
  }
}
