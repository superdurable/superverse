// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { useCallback, useState } from "react";
import { ApiError, errorMessage } from "@/lib/api/transport/api-error";
import { isOpenFlowStatusCode } from "@/lib/api/flow/flow-status";

/**
 * An empty answer, a failed refresh over a good answer, and never having asked are
 * three different things. Collapsing them sends a reader to the wrong place.
 */
export type Liveness = "loading" | "ok" | "stale" | "unreachable" | "stranded";

export interface Held<T> {
  readonly value: T | null;
  readonly liveness: Liveness;
  readonly reason: string | null;
}

export type ReadOutcome<T> =
  | { readonly state: "ok"; readonly value: T }
  | { readonly state: "unreachable"; readonly reason: string }
  | { readonly state: "stranded"; readonly reason: string };

export function nothingHeld<T>(): Held<T> {
  return { value: null, liveness: "loading", reason: null };
}

export function absorb<T>(prior: Held<T>, next: ReadOutcome<T>): Held<T> {
  if (next.state === "ok") return { value: next.value, liveness: "ok", reason: null };
  // A stranded run keeps its own liveness: nothing is coming back, so it is not stale.
  if (next.state === "stranded") {
    return { value: prior.value, liveness: "stranded", reason: next.reason };
  }
  // Showing a true-a-moment-ago answer late beats blanking it; reporting it as fresh is worse.
  if (prior.value !== null) return { value: prior.value, liveness: "stale", reason: next.reason };
  return { value: null, liveness: "unreachable", reason: next.reason };
}

const GRPC_FAILED_PRECONDITION = 9;

/**
 * Dex routes a Flow RPC to the worker that owns the run, so a worker that exited makes one run
 * permanently unreadable while the server stays healthy. Measured, not assumed: that surfaces as
 * FailedPrecondition. A closed run is excluded by status — its Display is equally unreadable, but
 * nothing is owed on it.
 */
function isStrandedRunFailure(error: unknown, flowStatusCode: number | undefined): boolean {
  return (
    error instanceof ApiError && error.grpcCode === GRPC_FAILED_PRECONDITION && isOpenFlowStatusCode(flowStatusCode)
  );
}

export function classifyReadFailure<T>(error: unknown, flowStatusCode: number | undefined): ReadOutcome<T> {
  const reason = errorMessage(error, String(error));
  return isStrandedRunFailure(error, flowStatusCode) ? { state: "stranded", reason } : { state: "unreachable", reason };
}

/**
 * Learned, never searched: a run reports itself unreachable only when somebody opens it.
 * Session-scoped because a worker could in principle come back.
 */
export function useStrandedRuns() {
  const [strandedFlowIDs, setStrandedFlowIDs] = useState<ReadonlySet<string>>(() => new Set());
  const rememberStranded = useCallback((strandedFlowID: string) => {
    setStrandedFlowIDs((prior) => (prior.has(strandedFlowID) ? prior : new Set([...prior, strandedFlowID])));
  }, []);
  return { strandedFlowIDs, rememberStranded };
}
