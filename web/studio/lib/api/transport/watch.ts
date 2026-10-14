// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { RpcInput, RpcOutput, WatchMethod } from "@/lib/shared/rpc-methods";

/**
 * A Watch connection ended without its owner closing it. It is retryable when
 * the stream dropped or ended; a refused request (a non-2xx status, such as a
 * revoked access) is not, and EventSource reports no status to tell more.
 */
export class WatchConnectionError extends Error {
  constructor(readonly retryable: boolean) {
    super(retryable ? "Watch disconnected" : "Watch was refused");
    this.name = "WatchConnectionError";
  }
}

/**
 * Opens one Watch through /api/rpc/<Method> as Server-Sent Events and hands
 * each frame to `onFrame`. Every connection begins with a snapshot. The promise
 * resolves when `signal` aborts and rejects when the connection ends
 * (WatchConnectionError), a frame is invalid or `onFrame` throws. EventSource
 * never reconnects on its own here: reconnecting with the last frame's
 * resumeCursor belongs to the caller, which re-reads what it may have missed.
 */
export function watch<M extends WatchMethod>(
  method: M,
  input: Omit<RpcInput<M>, "resumeCursor">,
  {
    resumeCursor,
    signal,
    onFrame,
  }: { resumeCursor: string; signal: AbortSignal; onFrame: (frame: RpcOutput<M>) => void },
): Promise<void> {
  if (signal.aborted) return Promise.resolve();
  const query = new URLSearchParams(Object.entries(input).map(([key, value]) => [key, String(value)]));
  if (resumeCursor) query.set("resumeCursor", resumeCursor);
  return new Promise((resolve, reject) => {
    const source = new EventSource(`/api/rpc/${method}?${query}`);
    let sawSnapshot = false;
    const finish = (error?: Error) => {
      source.close();
      signal.removeEventListener("abort", abort);
      if (error) reject(error);
      else resolve();
    };
    const abort = () => finish();
    const receive = (event: MessageEvent<string>) => {
      try {
        // Every Watch<R>Response names its member in the `event` oneof.
        const frame: RpcOutput<M> & { event?: string } = JSON.parse(event.data);
        if (!sawSnapshot && frame.event !== "snapshot") throw new Error("Watch did not begin with a snapshot");
        sawSnapshot = true;
        onFrame(frame);
      } catch (error) {
        finish(error instanceof Error ? error : new Error("Invalid Watch frame"));
      }
    };
    source.addEventListener("snapshot", receive);
    source.addEventListener("progress", receive);
    // EventSource would retry a dropped stream (CONNECTING) but gives up on a refused request (CLOSED).
    source.onerror = () => finish(new WatchConnectionError(source.readyState !== EventSource.CLOSED));
    signal.addEventListener("abort", abort, { once: true });
  });
}
