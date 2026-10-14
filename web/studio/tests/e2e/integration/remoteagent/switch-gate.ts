// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { expect, type Page, type Request, type Route } from "@playwright/test";
import { object } from "@/tests/e2e/support/json-assert";
import { rpcPosted } from "@/tests/e2e/support/studio-api";

async function bounded(work: Promise<void>, label: string) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([
      work,
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error(`${label} timed out`)), 10_000);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
}

/** Delay one actual browser command before dispatch. Never fabricates a response or holds a Watch. */
export async function installSwitchGate(page: Page, projectId: string, basisEpoch: string) {
  const pattern = "**/api/rpc/SetSessionExecutor";
  let request: Request | null = null;
  let calls = 0;
  let armed = false;
  let cancelled = false;
  let failedOtherwise = false;
  let expired = false;
  let disposing = false;
  let disposed = false;
  let release!: () => void;
  let finished!: () => void;
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  const completed = new Promise<void>((resolve) => {
    finished = resolve;
  });
  const onFailure = (candidate: Request) => {
    if (candidate !== request || expired || disposing || !armed) return;
    const error = candidate.failure()?.errorText ?? "";
    cancelled = /ERR_ABORTED|NS_BINDING_ABORTED|cancel(?:l)?ed/i.test(error);
    failedOtherwise = !cancelled;
  };
  const handler = async (route: Route) => {
    const candidate = route.request();
    if (!rpcPosted("SetSessionExecutor")(candidate)) {
      await route.continue();
      return;
    }
    const body = object(candidate.postDataJSON(), "Switch request");
    if (body.projectId !== projectId || body.basisEpoch !== basisEpoch) {
      await route.continue();
      return;
    }
    ++calls;
    if (request) {
      await route.abort("aborted");
      return;
    }
    request = candidate;
    await held;
    try {
      // Forward only after actual browser cancellation was observed. An expired
      // or failed case is aborted during disposal, never dispatched as cleanup.
      if (cancelled && !expired && !disposing) await route.continue();
      else await route.abort("aborted");
    } catch {
      if (!candidate.failure()) throw new Error("Owned switch route could not be disposed");
    } finally {
      finished();
    }
  };
  page.on("requestfailed", onFailure);
  await page.route(pattern, handler);
  const deadline = setTimeout(() => {
    expired = true;
    release();
  }, 90_000);
  return {
    async pending() {
      await expect.poll(() => request !== null, { timeout: 15_000 }).toBe(true);
      if (expired || request?.failure()) throw new Error("Switch was not pending before its cancellation trigger");
      expect(calls).toBe(1);
      armed = true;
    },
    async assertCancelled() {
      await expect
        .poll(
          () => {
            if (expired || failedOtherwise) throw new Error("Switch deadline or non-cancellation network failure");
            return cancelled;
          },
          { timeout: 15_000 },
        )
        .toBe(true);
      expect(calls).toBe(1);
      release();
      await bounded(completed, "Cancelled switch disposal");
    },
    async dispose() {
      if (disposed) return;
      disposed = true;
      disposing = true;
      clearTimeout(deadline);
      release();
      try {
        if (request) await bounded(completed, "Owned switch route cleanup");
      } finally {
        await page.unroute(pattern, handler);
        page.off("requestfailed", onFailure);
      }
    },
  };
}
