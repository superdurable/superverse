// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

/** Whether `reason` is an abort: of a fetch, an AbortSignal or abortError. */
export function isAbortError(reason: unknown): boolean {
  return reason instanceof Error && reason.name === "AbortError";
}

/** Whether `reason` is the end of an `AbortSignal.timeout` bound. */
export function isTimeoutError(reason: unknown): boolean {
  return reason instanceof Error && reason.name === "TimeoutError";
}

/** The error an owner rejects with when it cancels work that its caller did not abort. */
export function abortError(message: string): DOMException {
  return new DOMException(message, "AbortError");
}

/** Waits `milliseconds`, or less when `signal` aborts first; the caller checks the signal afterwards. */
export function delay(milliseconds: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (signal?.aborted) return resolve();
    const finish = () => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", finish);
      resolve();
    };
    const timer = setTimeout(finish, milliseconds);
    signal?.addEventListener("abort", finish, { once: true });
  });
}
