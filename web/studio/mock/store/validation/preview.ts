// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Preview evidence admission for mock release builds.
import type { PreviewValidation } from "@/lib/generated/protobuf/superverse/v2/PreviewValidation";
import { rpcFailure } from "@/lib/server/grpc-client";
import { isoToTimestamp } from "@/lib/shared/protobuf";

/** A skip reason has at most this many code points, as Go counts them. */
const SKIP_REASON_LIMIT = 500;

/**
 * How a new build meets the publishing rule, as the Release admission decides it: the code Live runs,
 * else the newest READY Preview start of the commit, else the written skip reason; none of them refuses
 * PREVIEW_VALIDATION_REQUIRED with its declared remedy.
 */
export function admittedValidation(
  commit: string,
  skipReason: string,
  liveReleaseId: string | null,
  starts: readonly { sourceCommit: string; outcome: string; commandId: string; endedAt: string }[],
): PreviewValidation {
  if (liveReleaseId === commit) return { kind: "KIND_LIVE_CODE" };
  const start = starts.findLast((entry) => entry.sourceCommit === commit && entry.outcome === "READY");
  if (start)
    return { kind: "KIND_VALIDATED", startCommandId: start.commandId, startedAt: isoToTimestamp(start.endedAt) };
  const reason = skipReason.trim();
  if (reason && [...reason].length <= SKIP_REASON_LIMIT) return { kind: "KIND_SKIPPED", skipReason: reason };
  throw rpcFailure(
    409,
    "This version has not run on Preview. Run it on Preview, or give a reason to publish it without.",
    {
      reason: "PREVIEW_VALIDATION_REQUIRED",
      domain: "superverse.v2",
      metadata: { remedy: "FAILURE_REMEDY_APPLY_AGAIN" },
    },
  );
}
