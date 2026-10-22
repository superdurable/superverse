// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Evolve__Output } from "@/lib/generated/protobuf/superverse/v2/Evolve";
import type { RpcInput, RpcOutput } from "@/lib/shared/rpc-methods";
import { ApiError } from "./transport/api-error";
import { readUnary } from "./transport/read-unary";
import { unary } from "./transport/unary";

// Browser client of EvolveService: a Project's goals, as its Live build's goals file states them, and the reviews
// that measured its Live runs against them.

/** The Project's goal measurement, or null before its first Live deployment, when nothing measures it yet. */
export async function getEvolve(
  input: Required<RpcInput<"GetEvolve">>,
  signal: AbortSignal,
): Promise<Evolve__Output | null> {
  try {
    return (await readUnary("GetEvolve", input, { signal })).evolve;
  } catch (reason) {
    if (reason instanceof ApiError && reason.status === 404) return null;
    throw reason;
  }
}

/**
 * Review now: starts a review of the Live runs at once, one at a time and at most one a day. The answer names the
 * review started, or why none started; a retry with the same request ID answers the review it started.
 */
export function startEvolveReview(
  input: Required<RpcInput<"StartEvolveReview">>,
  signal: AbortSignal,
): Promise<RpcOutput<"StartEvolveReview">> {
  return unary("StartEvolveReview", input, { signal });
}
