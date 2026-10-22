// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import type { Evolve__Output } from "@/lib/generated/protobuf/superverse/v2/Evolve";
import type { EvolveGoalKind__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveGoalKind";
import type { EvolveReview__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveReview";
import type { EvolveReviewReason__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveReviewReason";
import type { EvolveReviewRefusal__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveReviewRefusal";
import type { EvolveReviewStatus__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveReviewStatus";
import type { EvolveReviewTrigger__Output } from "@/lib/generated/protobuf/superverse/v2/EvolveReviewTrigger";
import { formatMonthDayTime } from "@/lib/shared/format";
import { timestampToISO } from "@/lib/shared/protobuf";

/** What a goal is about, as the quiet label beside it. */
export const KIND_WORDS = {
  EVOLVE_GOAL_KIND_UNSPECIFIED: "Goal",
  EVOLVE_GOAL_KIND_OUTCOME: "Outcome",
  EVOLVE_GOAL_KIND_EXPERIENCE: "Experience",
  EVOLVE_GOAL_KIND_COST: "Cost",
} as const satisfies Record<EvolveGoalKind__Output, string>;

const TRIGGER_WORDS = {
  EVOLVE_REVIEW_TRIGGER_UNSPECIFIED: "Review",
  EVOLVE_REVIEW_TRIGGER_SCHEDULE: "Scheduled",
  EVOLVE_REVIEW_TRIGGER_MANUAL: "Review now",
} as const satisfies Record<EvolveReviewTrigger__Output, string>;

/** Why a review was skipped or failed, after "Skipped:" or "Failed:". */
const REASON_WORDS = {
  EVOLVE_REVIEW_REASON_UNSPECIFIED: "",
  EVOLVE_REVIEW_REASON_NO_LIVE: "nothing was Live",
  EVOLVE_REVIEW_REASON_NO_GOALS: "the Live version has no goals",
  EVOLVE_REVIEW_REASON_GOALS_INVALID: "the goals file has problems",
  EVOLVE_REVIEW_REASON_PREVIOUS_REVIEW_RUNNING: "the previous review was still running",
  EVOLVE_REVIEW_REASON_LIVE_UNREACHABLE: "Live did not answer",
  EVOLVE_REVIEW_REASON_EXPORT_TOO_LARGE: "the runs were too large to save",
  EVOLVE_REVIEW_REASON_EXPORT_FAILED: "the runs could not be saved",
  EVOLVE_REVIEW_REASON_TIMED_OUT: "it took too long",
  EVOLVE_REVIEW_REASON_METRICS_FAILED: "the goals could not be computed",
} as const satisfies Record<EvolveReviewReason__Output, string>;

const STATUS = {
  EVOLVE_REVIEW_STATUS_UNSPECIFIED: { words: "", tone: "text-ink-dim" },
  EVOLVE_REVIEW_STATUS_EXPORTING: { words: "In progress", tone: "text-accent" },
  EVOLVE_REVIEW_STATUS_MEASURED: { words: "Measured", tone: "text-pass" },
  EVOLVE_REVIEW_STATUS_SKIPPED: { words: "Skipped", tone: "text-ink-dim" },
  EVOLVE_REVIEW_STATUS_FAILED: { words: "Failed", tone: "text-fail" },
} as const satisfies Record<EvolveReviewStatus__Output, { words: string; tone: string }>;

/** A review's status in words, with its reason when it was skipped or failed, and the colour that says how it went. */
export function reviewStatus(review: EvolveReview__Output): { words: string; tone: string } {
  const { words, tone } = STATUS[review.status];
  const reason = REASON_WORDS[review.reason];
  return { words: reason ? `${words}: ${reason}` : words, tone };
}

/** What started a review and what it read: "Scheduled · 312 runs · Sep 14 – Oct 14". */
export function reviewFacts(review: EvolveReview__Output): string {
  const from = monthDay(review.windowFrom);
  const to = monthDay(review.windowTo);
  const runs = review.export?.runs ?? 0;
  return [
    TRIGGER_WORDS[review.trigger],
    review.status !== "EVOLVE_REVIEW_STATUS_SKIPPED" &&
      runs > 0 &&
      `${runs.toLocaleString()} run${runs === 1 ? "" : "s"}`,
    from && to && `${from} – ${to}`,
  ]
    .filter(Boolean)
    .join(" · ");
}

function monthDay(value: Parameters<typeof timestampToISO>[0]): string {
  const iso = timestampToISO(value);
  return iso ? new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(new Date(iso)) : "";
}

const REFUSAL_WORDS = {
  EVOLVE_REVIEW_REFUSAL_UNSPECIFIED: "",
  EVOLVE_REVIEW_REFUSAL_NO_GOALS: "Live has no goals to review",
  EVOLVE_REVIEW_REFUSAL_GOALS_INVALID: "Fix the goals file and publish first",
  EVOLVE_REVIEW_REFUSAL_REVIEW_RUNNING: "A review is running",
  EVOLVE_REVIEW_REFUSAL_RATE_LIMITED: "Review now runs once a day",
} as const satisfies Record<EvolveReviewRefusal__Output, string>;

/**
 * Why Review now cannot start, or null when it can: the refusal of the last click, else what the measurement
 * already says. The once-a-day limit names when it admits one again.
 */
export function reviewNowBlocked(evolve: Evolve__Output, refusal: EvolveReviewRefusal__Output | null): string | null {
  const again = timestampToISO(evolve.nextManualReviewAt);
  const limited = again
    ? `Once a day; again ${formatMonthDayTime(again)}`
    : REFUSAL_WORDS.EVOLVE_REVIEW_REFUSAL_RATE_LIMITED;
  if (refusal === "EVOLVE_REVIEW_REFUSAL_RATE_LIMITED") return limited;
  if (refusal) return REFUSAL_WORDS[refusal] || null;
  if (evolve.activeReviewId) return REFUSAL_WORDS.EVOLVE_REVIEW_REFUSAL_REVIEW_RUNNING;
  if (evolve.goals?.status === "EVOLVE_GOALS_STATUS_INVALID") return REFUSAL_WORDS.EVOLVE_REVIEW_REFUSAL_GOALS_INVALID;
  if (again) return limited;
  return null;
}
