// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/runexport"
)

// The values below are persisted in EvolveFlow Attributes, Step inputs and
// RPC payloads; their field names are their JSON encoding.

// LiveRelease is a committed Live deployment as ProjectDeployFlow announces it.
// Revision is the deployment's Project lease generation, which grows with every
// Live deployment, rollbacks included; ReleaseID is the build commit.
// ReplacedRevision is the Revision of the Live deployment it replaced, zero
// for none and in announcements from before it.
type LiveRelease struct {
	Revision               uint64
	ReleaseID, BuildCommit string
	CommittedAt            time.Time
	ReplacedRevision       uint64 `json:",omitempty"`
}

// GoalsStatus is what the goals file of the Live build holds.
type GoalsStatus string

const (
	GoalsNone    GoalsStatus = "none"
	GoalsValid   GoalsStatus = "valid"
	GoalsInvalid GoalsStatus = "invalid"
)

// Goals is the goals block of the Live build at Commit, a copy the file owns.
type Goals struct {
	Commit string
	Status GoalsStatus
	// Spec is the parsed block as JSON, at most maximumSpecBytes, when valid.
	Spec string `json:",omitempty"`
	// Errors are the parser's first problems, when invalid.
	Errors []string `json:",omitempty"`
	ReadAt time.Time
}

// Parsed is the stored goals block, empty without valid goals.
func (g Goals) Parsed() goals.Spec {
	if g.Spec == "" {
		return goals.Spec{}
	}
	return g.spec()
}

// spec decodes the stored block; a stored Spec always decodes.
func (g Goals) spec() goals.Spec {
	var spec goals.Spec
	comm.MustDo(json.Unmarshal([]byte(g.Spec), &spec))
	return spec
}

// Schedule is when the next scheduled review runs: at AnchorHour of the
// period's cycle, an hour of the week that a hash of the Project ID spreads.
type Schedule struct {
	Period     goals.Period
	AnchorHour int
	NextAt     time.Time
	Active     bool
}

// ManualRequests are the newest admitted Review now requests and the time of
// the last one, which the once-a-day limit measures from.
type ManualRequests struct {
	RequestIDs []string
	LastAt     time.Time
}

// ReviewTrigger is what started a review.
type ReviewTrigger string

const (
	TriggerSchedule ReviewTrigger = "schedule"
	TriggerManual   ReviewTrigger = "manual"
)

// ReviewStatus is a review's lifecycle: exporting, then measured, or waiting
// for the agent's report, which ends it proposal or no_change, or expired.
type ReviewStatus string

const (
	ReviewExporting    ReviewStatus = "exporting"
	ReviewMeasured     ReviewStatus = "measured"
	ReviewWaitingAgent ReviewStatus = "waiting_agent"
	ReviewProposal     ReviewStatus = "proposal"
	ReviewNoChange     ReviewStatus = "no_change"
	ReviewExpired      ReviewStatus = "expired"
	ReviewSkipped      ReviewStatus = "skipped"
	ReviewFailed       ReviewStatus = "failed"
)

// ReviewReason is why a review was skipped or failed.
type ReviewReason string

// The reasons of a skipped review.
const (
	ReasonNoLive                ReviewReason = "no_live"
	ReasonNoGoals               ReviewReason = "no_goals"
	ReasonGoalsInvalid          ReviewReason = "goals_invalid"
	ReasonPreviousReviewRunning ReviewReason = "previous_review_running"
	// The reasons the Project's Session gives when it cannot take an agent review.
	ReasonSessionUnavailable ReviewReason = "session_unavailable"
	ReasonRemoteSession      ReviewReason = "remote_session"
	ReasonNoModelKey         ReviewReason = "no_model_key"
)

// The failure codes of a failed review.
const (
	ReasonLiveUnreachable ReviewReason = "live_unreachable"
	ReasonExportTooLarge  ReviewReason = "export_too_large"
	ReasonExportFailed    ReviewReason = "export_failed"
	ReasonTimedOut        ReviewReason = "timed_out"
	ReasonMetricsFailed   ReviewReason = "metrics_failed"
)

// ReviewRefusal is why StartReview admitted no review; empty when it did.
type ReviewRefusal string

const (
	RefusalNoGoals                     = ReviewRefusal(ReasonNoGoals)
	RefusalGoalsInvalid                = ReviewRefusal(ReasonGoalsInvalid)
	RefusalReviewRunning ReviewRefusal = "review_running"
	RefusalRateLimited   ReviewRefusal = "rate_limited"
)

// ReviewRecord is one review, the Reviews entry r-<n>.
type ReviewRecord struct {
	ID      string
	Trigger ReviewTrigger
	// RequestID and Actor name the person's Review now request.
	RequestID, Actor string `json:",omitempty"`
	RequestedAt      time.Time
	StartedAt        time.Time `json:",omitzero"`
	FinishedAt       time.Time `json:",omitzero"`
	Window           runexport.Window
	// GoalsCommit and GoalsSpec are the goals BeginReview copied; the Spec copy is dropped when the review ends.
	GoalsCommit string `json:",omitempty"`
	GoalsSpec   string `json:",omitempty"`
	Status      ReviewStatus
	Reason      ReviewReason `json:",omitempty"`
	Detail      string       `json:",omitempty"`
	Export      ExportSummary
	Metrics     *MetricsSummary `json:",omitempty"`
	// Usage is the platform's model usage of the agent's review, the billable record the Session reports.
	Usage []ModelUsage `json:",omitempty"`
}

// ended reports whether the review reached its final status.
func (r ReviewRecord) ended() bool {
	return r.Status != ReviewExporting && r.Status != ReviewWaitingAgent
}

// ExportSummary is what an export stored: its parts and runs, whether it was
// cut at a limit, and whether a later review deleted it. RunsSHA256 and
// Records are the digest and the count of the runs its metrics evaluated, one
// per FlowID (runexport.WriteCanonical), which a placed runs.jsonl repeats;
// absent until the metrics are computed.
type ExportSummary struct {
	Parts, Runs int
	Bytes       int64
	Truncated   bool
	Deleted     bool
	RunsSHA256  string `json:",omitempty"`
	Records     int    `json:",omitempty"`
}

// exportDigest is the digest and the count of the runs a review's metrics
// evaluated.
type exportDigest struct {
	RunsSHA256 string `json:",omitempty"`
	Records    int    `json:",omitempty"`
}

// MetricsSummary is the review's goal values, at most maximumSummaryBytes;
// metrics.json beside the export holds the rest.
type MetricsSummary struct {
	Runs  int
	Goals []GoalSummary
}

// GoalSummary is one goal's value; Holds is set for a guardrail with a value.
// Digest names the goal's definition the value was measured under
// (the goals package's definition digest).
type GoalSummary struct {
	ID      string
	Digest  string   `json:",omitempty"`
	Value   *float64 `json:",omitempty"`
	Runs    int
	FewRuns bool
	Holds   *bool `json:",omitempty"`
}

// Snapshot is EvolveFlow as GetEvolve reads it, the reviews and proposals newest first.
type Snapshot struct {
	ProjectID string
	Live      LiveRelease
	Goals     Goals
	Schedule  Schedule
	ReviewSeq int64
	Active    string
	// LastManualAt is the time of the last admitted Review now.
	LastManualAt time.Time
	Reviews      []ReviewRecord
	Proposals    []ProposalRecord
}

// NextManualReviewAt is when the once-a-day limit admits Review now again,
// zero when it admits one now.
func (s Snapshot) NextManualReviewAt(now time.Time) time.Time {
	if next := s.LastManualAt.Add(manualReviewEvery); !s.LastManualAt.IsZero() && next.After(now) {
		return next
	}
	return time.Time{}
}

// ReviewRequest is a person's Review now; RequestID is Studio's.
type ReviewRequest struct {
	RequestID, Actor string
}

// ReviewAdmission answers StartReview: the review it started, or why none.
type ReviewAdmission struct {
	ReviewID string        `json:",omitempty"`
	Refusal  ReviewRefusal `json:",omitempty"`
}

// Limits of the state EvolveFlow keeps.
const (
	maximumSpecBytes    = 8 << 10
	maximumGoalsErrors  = 10
	maximumSummaryBytes = 2 << 10
	// maximumBacktestBytes bounds a proposal's backtest summary, which holds up to 8 goals.
	maximumBacktestBytes = 8 << 10
	keptReviews          = 26
	keptManualRequests   = 16
	manualReviewEvery    = 24 * time.Hour
	weekHours            = 7 * 24
)

// reviewKey is the Reviews instance and ID of review number seq, r-<seq>.
func reviewKey(seq int64) string { return "r-" + strconv.FormatInt(seq, 10) }

// reviewNumber is the number of a review key.
func reviewNumber(key string) int64 {
	n, err := strconv.ParseInt(strings.TrimPrefix(key, "r-"), 10, 64)
	if err != nil || !strings.HasPrefix(key, "r-") {
		panic("evolve: review key " + strconv.Quote(key) + " is not r-<n>")
	}
	return n
}

// readGoals turns a goals file read at commit into the Goals to store; found
// is false when the Live build has no goals file.
func readGoals(commit string, file []byte, found bool, at time.Time) Goals {
	read := Goals{Commit: commit, Status: GoalsNone, ReadAt: at}
	if !found {
		return read
	}
	spec, problems := goals.Parse(file)
	if len(problems) > 0 {
		read.Status = GoalsInvalid
		for _, problem := range problems[:min(len(problems), maximumGoalsErrors)] {
			read.Errors = append(read.Errors, comm.TruncateUTF8(problem.String(), 300))
		}
		return read
	}
	encoded := comm.Must(json.Marshal(spec))
	if len(encoded) > maximumSpecBytes {
		read.Status, read.Errors = GoalsInvalid, []string{fmt.Sprintf("the goals block takes %d bytes as JSON; at most %d are kept", len(encoded), maximumSpecBytes)}
		return read
	}
	read.Status, read.Spec = GoalsValid, string(encoded)
	return read
}

// goalsReadFailure is the Goals of a read whose retries ran out.
func goalsReadFailure(commit, detail string, at time.Time) Goals {
	return Goals{Commit: commit, Status: GoalsInvalid, Errors: []string{dexflow.FailureMessage("The goals file could not be read.", detail)}, ReadAt: at}
}

// summarize keeps each goal's headline numbers within maximumSummaryBytes.
func summarize(metrics goals.Metrics) MetricsSummary {
	summary := MetricsSummary{Runs: metrics.Runs}
	for _, goal := range metrics.Goals {
		entry := GoalSummary{ID: goal.ID, Digest: goal.Digest, Value: goal.Value, Runs: goal.Runs, FewRuns: goal.FewRuns}
		if goal.Guardrail != nil {
			entry.Holds = goal.Guardrail.Holds
		}
		summary.Goals = append(summary.Goals, entry)
	}
	for len(summary.Goals) > 0 && len(comm.Must(json.Marshal(summary))) > maximumSummaryBytes {
		summary.Goals = summary.Goals[:len(summary.Goals)-1]
	}
	return summary
}

// newestFirst orders review records by number, newest first.
func newestFirst(records []ReviewRecord) {
	slices.SortFunc(records, func(a, b ReviewRecord) int { return int(reviewNumber(b.ID) - reviewNumber(a.ID)) })
}

// exportPrefix is the object prefix of a review's export, deleted as a whole.
func exportPrefix(projectID, reviewID string) string {
	return projectID + "/evolve/exports/" + reviewID + "/"
}

// partKey names one part written by one attempt; Put never overwrites a key.
func partKey(projectID, reviewID string, part int, attempt int32) string {
	return fmt.Sprintf("%spart-%04d-%d.jsonl.gz", exportPrefix(projectID, reviewID), part, attempt)
}

// reviewInput is the input of every Step of a review, which its failure
// route receives unchanged.
type reviewInput struct {
	ReviewID string
	// Skip is why BeginReview found the review cannot measure; DeletedExports
	// are the earlier reviews whose exports it deleted, and DeletedProposals
	// the pruned proposals whose stored proposal it deleted.
	Skip             ReviewReason `json:",omitempty"`
	DeletedExports   []string     `json:",omitempty"`
	DeletedProposals []string     `json:",omitempty"`
	// Live is the Project's current Live deployment that BeginReview read.
	Live     *LiveRelease `json:",omitempty"`
	Export   exportCursor
	Manifest *objectstore.ObjectRef `json:",omitempty"`
	Metrics  *MetricsSummary        `json:",omitempty"`
	// Failure is a decision that ends the review failed, or skipped for a Session that cannot take it.
	Failure *reviewFailure `json:",omitempty"`
	// ReviewsOn is whether agent reviews run for the Project, which ComputeMetrics read.
	ReviewsOn bool `json:",omitempty"`
}

// reviewFailure is a review's failure code and its detail.
type reviewFailure struct {
	Code   ReviewReason
	Detail string
}

// exportCursor is where an export continues: the next part, the goals' Flow
// type and its search page, and what the parts so far hold; Digest is set
// once the metrics evaluated them.
type exportCursor struct {
	Part, Flow  int
	PageToken   string `json:",omitempty"`
	Runs, Reads int
	Bytes       int64
	Truncated   bool
	Parts       []exportPart  `json:",omitempty"`
	Digest      *exportDigest `json:",omitempty"`
}

// summary is what the export stored, as its review keeps it.
func (cursor exportCursor) summary() ExportSummary {
	summary := ExportSummary{Parts: len(cursor.Parts), Runs: cursor.Runs, Bytes: cursor.Bytes, Truncated: cursor.Truncated}
	if cursor.Digest != nil {
		summary.RunsSHA256, summary.Records = cursor.Digest.RunsSHA256, cursor.Digest.Records
	}
	return summary
}

// exportPart is one written part with its exact object.
type exportPart struct {
	runexport.Part
	Ref objectstore.ObjectRef
}

// goalsReading is a goals file read for one Live revision, the input of
// ReadGoals and CommitGoals; Goals is nil until the read. Holds says, for
// each applied or live proposal by review ID, whether the build holds its
// applied commit. PreviousReleaseID and PreviousBuildCommit are the release
// Live ran just before this revision, which this deployment replaced; empty
// when it was the same release or none, or when the measurement missed the
// release Live ran just before. PreviousHolds says, for each applied proposal
// the build holds, whether that previous build held it too. ReviewID is the
// review that waits for these goals and begins again once they are stored.
// GoalsOnly marks a review's reading of a revision whose own reading is still
// pending: it checks no proposal and moves none, so the pending reading, which
// knows the release before, moves them.
type goalsReading struct {
	Revision            uint64
	Commit              string
	PreviousReleaseID   string          `json:",omitempty"`
	PreviousBuildCommit string          `json:",omitempty"`
	Goals               *Goals          `json:",omitempty"`
	Holds               map[string]bool `json:",omitempty"`
	PreviousHolds       map[string]bool `json:",omitempty"`
	ReviewID            string          `json:",omitempty"`
	GoalsOnly           bool            `json:",omitempty"`
}

// liveReading is the goals reading of Live deployment live after previous,
// which is the release before when live replaced it and is another release.
func liveReading(previous, live LiveRelease) goalsReading {
	reading := goalsReading{Revision: live.Revision, Commit: live.BuildCommit}
	if previous.ReleaseID != live.ReleaseID && previous.Revision == live.ReplacedRevision {
		reading.PreviousReleaseID, reading.PreviousBuildCommit = previous.ReleaseID, previous.BuildCommit
	}
	return reading
}

// measuredLive is the Live deployment whose goals a review measures with:
// the newer of the one EvolveFlow was told of and the current one, if read.
func measuredLive(announced LiveRelease, current *LiveRelease) LiveRelease {
	if current != nil && current.Revision > announced.Revision {
		return *current
	}
	return announced
}
