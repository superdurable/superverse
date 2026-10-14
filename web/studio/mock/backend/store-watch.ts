// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Development-only Watches over the in-memory mock store.
import { httpError } from "@/lib/server/request-body";
import { delay } from "@/lib/shared/async";

/**
 * Polls the store until the signal aborts or `next` returns null, yielding each tick's frames. A
 * missing resource on the first tick fails the Watch, as Go's NotFound does.
 */
export async function* pollStore<T>(signal: AbortSignal, next: () => readonly T[] | null): AsyncGenerator<T> {
  let first = true;
  while (!signal.aborted) {
    const frames = next();
    if (!frames) {
      if (first) throw httpError(404, "Not found");
      return;
    }
    first = false;
    yield* frames;
    await delay(250, signal);
  }
}

/** A resource Watch: the current snapshot first, then each changed one; `final` ends the Watch. */
export function watchSnapshots<S>(
  signal: AbortSignal,
  read: () => S | null,
  final: (snapshot: S) => boolean = () => false,
): AsyncGenerator<{ snapshot: S }> {
  let fingerprint = "";
  let ended = false;
  return pollStore(signal, () => {
    const snapshot = ended ? null : read();
    if (!snapshot) return null;
    ended = final(snapshot);
    const text = JSON.stringify(snapshot);
    if (text === fingerprint) return [];
    fingerprint = text;
    return [{ snapshot }];
  });
}
