// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

"use client";

import { useState } from "react";
import type { Evolve__Output } from "@/lib/generated/protobuf/superverse/v2/Evolve";
import { useReviewNow } from "@/lib/project/goals/use-goal-measurement";
import { formatMonthDayTime } from "@/lib/shared/format";
import { timestampToISO } from "@/lib/shared/protobuf";
import { HistoryList, HistoryRow } from "@/components/publishing/HistoryRows";
import { RequestProblem } from "@/components/publishing/RequestProblem";
import { Button } from "@/components/ui/Button";
import { reviewFacts, reviewNowBlocked, reviewStatus } from "./review-words";

/**
 * The reviews that measured Live's runs against the goals, newest first: when, what started each one, the runs it
 * read and their window, and how it ended. Those who build start one with Review now, which a running review, an
 * invalid goals file or the once-a-day limit holds back.
 */
export function GoalReviews({
  projectId,
  evolve,
  canStart,
  onChange,
}: {
  projectId: string;
  evolve: Evolve__Output;
  canStart: boolean;
  /** Reads the measurement again, after Review now answered. */
  onChange: () => void;
}) {
  // Ages are read when the list first renders.
  const [now] = useState(() => Date.now());
  const reviewNow = useReviewNow(projectId, onChange);
  const blocked = reviewNowBlocked(evolve, reviewNow.refusal);
  // A running review and an invalid goals file already show on the page; only the button's title repeats them.
  const shownElsewhere =
    !reviewNow.refusal && (Boolean(evolve.activeReviewId) || evolve.goals?.status === "EVOLVE_GOALS_STATUS_INVALID");
  const next = evolve.schedule?.active ? timestampToISO(evolve.schedule.nextAt) : null;
  const caption =
    canStart && blocked && !shownElsewhere ? blocked : next ? `Next review ${formatMonthDayTime(next)}` : null;
  return (
    <HistoryList
      label="Reviews"
      heading="Reviews"
      testId="goal-reviews"
      action={
        <span className="flex flex-wrap items-center justify-end gap-x-2 gap-y-1 text-right">
          {caption && (
            <span className="text-caption text-ink-faint" data-testid="review-now-note">
              {caption}
            </span>
          )}
          {canStart && (
            <Button
              size="sm"
              disabled={reviewNow.starting || blocked !== null}
              title={blocked ?? "Measure Live's runs against the goals now"}
              onClick={reviewNow.start}
            >
              {reviewNow.starting ? "Starting…" : "Review now"}
            </Button>
          )}
        </span>
      }
      footer={
        reviewNow.failure && (
          <div className="mt-2">
            <RequestProblem operation="Starting a review" reason={reviewNow.failure.reason} />
          </div>
        )
      }
    >
      {evolve.reviews.length === 0 && <p className="px-3 py-2.5 text-[13px] text-ink-faint">No reviews yet.</p>}
      <ol className="divide-y divide-line-faint">
        {evolve.reviews.map((review) => {
          const status = reviewStatus(review);
          const truncated = review.export?.truncated;
          const detail =
            review.status === "EVOLVE_REVIEW_STATUS_FAILED" && review.detail
              ? review.detail
              : truncated
                ? "The export stopped at its limit, so the values rest on the runs it read."
                : undefined;
          return (
            <li key={review.reviewId} data-testid="goal-review" data-status={review.status}>
              <HistoryRow at={timestampToISO(review.requestedAt)} now={now} detail={detail}>
                <span className={status.tone}>{status.words}</span>
                <span className="text-ink-dim"> · {reviewFacts(review)}</span>
              </HistoryRow>
            </li>
          );
        })}
      </ol>
    </HistoryList>
  );
}
