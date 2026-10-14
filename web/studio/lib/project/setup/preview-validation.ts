// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { PreviewStartRecord__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewStartRecord";
import type { PreviewValidation__Output } from "@/lib/generated/protobuf/superverse/v2/PreviewValidation";
import { formatMonthDayTime } from "@/lib/shared/format";
import { timestampToISO } from "@/lib/shared/protobuf";

/** A skip reason has at most this many code points, as the server counts them. */
export const SKIP_REASON_LIMIT = 500;

/**
 * How a version stands against the publishing rule before it is built: the newest READY Preview start of
 * its commit, or none among the Preview's bounded record. A start the record dropped is not "never ran",
 * so a version without one reads "not in the Preview record".
 */
export type VersionValidation = { validated: true; endedAt: string } | { validated: false };

/** The validation of `commit` from the Preview's recent starts, newest last. */
export function versionValidation(
  starts: readonly PreviewStartRecord__Output[] | undefined,
  commit: string,
): VersionValidation {
  const start = (starts ?? []).findLast(
    (entry) => entry.sourceCommit === commit && entry.outcome === "START_OUTCOME_READY",
  );
  const endedAt = start && timestampToISO(start.endedAt);
  return endedAt ? { validated: true, endedAt } : { validated: false };
}

/** What the wizard says of a version's validation. */
export function validationText(validation: VersionValidation): string {
  return validation.validated
    ? `ran on Preview at ${formatMonthDayTime(validation.endedAt)}`
    : "not in the Preview record";
}

/** A skip reason as the server receives it: trimmed. */
export function trimmedSkipReason(text: string): string {
  return text.trim();
}

/** What is wrong with a skip reason, or "" when it can be sent: present and at most the limit in code points. */
export function skipReasonProblem(text: string): string {
  const reason = trimmedSkipReason(text);
  if (!reason) return "Say why this version is published without running on Preview.";
  const length = [...reason].length;
  return length > SKIP_REASON_LIMIT ? `Shorten the reason to ${SKIP_REASON_LIMIT} characters (now ${length}).` : "";
}

/** How a build met the publishing rule, as the version history shows it. */
export function buildValidationText(validation: PreviewValidation__Output | null | undefined): string {
  switch (validation?.kind) {
    case "KIND_VALIDATED": {
      const at = timestampToISO(validation.startedAt);
      return at ? `Ran on Preview at ${formatMonthDayTime(at)}` : "Ran on Preview";
    }
    case "KIND_SKIPPED":
      return `Published without running on Preview: ${validation.skipReason}`;
    case "KIND_LIVE_CODE":
      return "Configuration only: the code Live already ran";
    case "KIND_UNSPECIFIED":
    case undefined:
      return "Preview validation not recorded";
  }
}
