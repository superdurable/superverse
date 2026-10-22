// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"hash/fnv"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
)

// scheduleEpoch is a Monday at 00:00 UTC, where every review cycle starts.
var scheduleEpoch = time.Date(1970, time.January, 5, 0, 0, 0, 0, time.UTC)

// anchorHour is the Project's hour of the week for its scheduled reviews, a
// hash of its ID, so the reviews of all Projects spread over the week.
func anchorHour(projectID string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(projectID))
	return int(hash.Sum32() % weekHours)
}

// nextAnchor is the first review time of period after after: the anchor hour
// of a cycle of the period's length that starts at the epoch.
func nextAnchor(period goals.Period, anchor int, after time.Time) time.Time {
	length := period.Length()
	at := scheduleEpoch.Add(time.Duration(anchor) * time.Hour)
	if after.Before(at) {
		return at
	}
	cycles := int64(after.Sub(at) / length)
	at = at.Add(length * time.Duration(cycles))
	for !at.After(after) {
		at = at.Add(length)
	}
	return at
}

// nextSchedule is the schedule that current goals set. A schedule that keeps
// its period keeps its next review time, also one already due, so a goals
// change never drops a review the timer just started.
func nextSchedule(previous Schedule, current Goals, now time.Time) Schedule {
	schedule := Schedule{AnchorHour: previous.AnchorHour}
	if current.Status == GoalsValid {
		schedule.Period = current.spec().Review
		schedule.Active = schedule.Period.Length() > 0
	}
	switch {
	case !schedule.Active:
	case previous.Active && previous.Period == schedule.Period && !previous.NextAt.IsZero():
		schedule.NextAt = previous.NextAt
	default:
		schedule.NextAt = nextAnchor(schedule.Period, schedule.AnchorHour, now)
	}
	return schedule
}

// manualRefusal is why a Review now cannot start, empty when it can: the
// goals must be valid, no review may run and the last one must be a day old.
func manualRefusal(current Goals, active string, manual ManualRequests, now time.Time) ReviewRefusal {
	switch {
	case current.Status == GoalsNone:
		return RefusalNoGoals
	case current.Status == GoalsInvalid:
		return RefusalGoalsInvalid
	case active != "":
		return RefusalReviewRunning
	case !manual.LastAt.IsZero() && now.Sub(manual.LastAt) < manualReviewEvery:
		return RefusalRateLimited
	}
	return ""
}

// prunedReviews are the reviews beyond the newest keptReviews; a review whose
// export is still stored stays until a later review deletes the export.
func prunedReviews(records []ReviewRecord) []string {
	newestFirst(records)
	var pruned []string
	for _, record := range records[min(len(records), keptReviews):] {
		if record.ended() && (record.Status == ReviewSkipped || record.Export.Deleted) {
			pruned = append(pruned, record.ID)
		}
	}
	return pruned
}

// recoveredFailure is the failure of a review Step whose retries ran out or
// that passed its wait limit, by the Step that failed.
func recoveredFailure(step string, timedOut bool, detail string) reviewFailure {
	code := ReasonLiveUnreachable
	switch {
	case timedOut:
		code = ReasonTimedOut
	case step == dex.GetFinalStepType[reviewInput](FinishExport{}):
		code = ReasonExportFailed
	case step == dex.GetFinalStepType[reviewInput](ComputeMetrics{}):
		code = ReasonMetricsFailed
	}
	return reviewFailure{Code: code, Detail: detail}
}
