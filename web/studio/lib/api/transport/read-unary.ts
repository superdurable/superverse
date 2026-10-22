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
import { READ_POLICY, readRetryDelay } from "./read-policy";

/** Only the generated Get/List business RPCs may use automatic read retries. */
type ReadMethod = Extract<UnaryMethod, `Get${string}` | `List${string}`>;

/** One read, with three transient retries inside a shared deadline, canceled by its caller. */
export async function readUnary<M extends ReadMethod>(
  method: M,
  input: RpcInput<M>,
  { signal }: { signal: AbortSignal },
): Promise<RpcOutput<M>> {
  const deadline = new AbortController();
  const timer = setTimeout(
    () => deadline.abort(new DOMException("Read request timed out", "TimeoutError")),
    READ_POLICY.budgetMilliseconds,
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
        if (
          !(reason instanceof ApiError) ||
          !READ_POLICY.retryStatuses.includes(reason.status) ||
          attempt === READ_POLICY.maxRetries
        )
          throw reason;
        lastFailure = reason;
        // Jitter keeps mounted readers from retrying together after a shared outage.
        await delay(readRetryDelay(attempt), requestSignal);
      }
    }
  } finally {
    clearTimeout(timer);
  }
}
