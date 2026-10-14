// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/** A decoded Watch<R>Response: its oneof `event` names the member that is set. */
export interface WatchFrame {
  event?: "snapshot" | "progress";
  resumeCursor: string;
}

// A queue may exceed this byte budget by one encoded frame; gRPC separately bounds each message.
const QUEUE_BYTES = 256 * 1024;
// An SSE comment this often keeps an idle Watch inside the ALB's 60 s idle timeout.
const KEEPALIVE_MS = 20_000;

/**
 * Relays one Watch as Server-Sent Events: the single SSE writer of the BFF, for gRPC and the
 * mock alike. The first frame must be a snapshot. Successful upstream completion closes and
 * drains the queue. An upstream error, a request abort, an invalid first frame or an encoding
 * failure errors the queue at once, discarding unread frames rather than delivering them after
 * the caller's access may have been revoked; every exit aborts the upstream once.
 *
 * `open` receives the upstream's abort signal and `upstreamFailed`, through which a backend
 * reports a failure the moment it happens, while frames still wait for the browser. A failure
 * that only an iterator read reveals is seen when the relay next reads.
 */
export function eventStream(
  request: Request,
  open: (signal: AbortSignal, upstreamFailed: () => void) => AsyncIterable<WatchFrame>,
): Response {
  const upstream = new AbortController();
  const encoder = new TextEncoder();
  let closed = false;
  let keepalive: ReturnType<typeof setInterval> | undefined;
  let wake: (() => void) | undefined;
  const abort = () => fail("Resource Watch canceled");
  const upstreamFailed = () => fail("Resource Watch failed");
  let controller: ReadableStreamDefaultController<Uint8Array>;

  function finish() {
    closed = true;
    clearInterval(keepalive);
    request.signal.removeEventListener("abort", abort);
    upstream.abort();
    wake?.();
  }

  function fail(message: string) {
    if (closed) return;
    finish();
    controller.error(new Error(message));
  }

  function encode(frame: WatchFrame): Uint8Array {
    const id = frame.resumeCursor && !/[\r\n]/.test(frame.resumeCursor) ? `id: ${frame.resumeCursor}\n` : "";
    return encoder.encode(`${id}event: ${frame.event}\ndata: ${JSON.stringify(frame)}\n\n`);
  }

  // One frame is always read ahead, so a read that fails while the HTTP queue is full is seen at
  // once; the queue resumes the upstream only when it has room again. A failure that arrives
  // after the read-ahead frame reaches the relay through `upstreamFailed`.
  async function relay() {
    // The generator turns a backend that throws while opening into a failed first read.
    const frames = (async function* () {
      yield* open(upstream.signal, upstreamFailed);
    })();
    let sawSnapshot = false;
    let next = frames.next();
    for (;;) {
      let result: IteratorResult<WatchFrame>;
      try {
        result = await next;
      } catch {
        return upstreamFailed();
      }
      if (closed) return;
      if (result.done) {
        finish();
        return controller.close();
      }
      if (!sawSnapshot && result.value.event !== "snapshot") return fail("Watch did not begin with a snapshot");
      sawSnapshot = true;
      try {
        if (!result.value.event) throw new Error("Watch frame has no event");
        controller.enqueue(encode(result.value));
      } catch {
        return fail("Invalid resource Watch response");
      }
      next = frames.next();
      next.catch(upstreamFailed);
      if ((controller.desiredSize ?? 0) <= 0) await new Promise<void>((resolve) => (wake = resolve));
      wake = undefined;
    }
  }

  const body = new ReadableStream<Uint8Array>(
    {
      start(streamController) {
        controller = streamController;
        request.signal.addEventListener("abort", abort, { once: true });
        if (request.signal.aborted) return abort();
        keepalive = setInterval(() => {
          if (!closed) controller.enqueue(encoder.encode(": keepalive\n\n"));
        }, KEEPALIVE_MS);
        void relay();
      },
      pull() {
        wake?.();
      },
      cancel() {
        if (!closed) finish();
      },
    },
    { highWaterMark: QUEUE_BYTES, size: (chunk) => chunk.byteLength },
  );
  return new Response(body, {
    headers: {
      "content-type": "text/event-stream",
      "cache-control": "no-store, no-transform",
      "x-accel-buffering": "no",
    },
  });
}
