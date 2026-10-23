// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
)

// EvolveNotice is what EvolveFlow tells the Project's Session: a review that
// waits for the agent's report, or, with Decision, a person's decision on the
// review's proposal. Text is what the agent reads. Its notification ID is
// derived from the review and the decision's revision, so every retry of a
// delivery repeats it.
type EvolveNotice struct {
	ReviewID   string
	Decision   *DecisionNotice `json:",omitempty"`
	OccurredAt time.Time
	Text       string
}

// NotificationID is the ID of the notice's notification for the Session of
// project projectID: evolve/<projectID>/<reviewID>, or
// evolve-decision/<projectID>/<reviewID>/<revision> for a decision.
func (n EvolveNotice) NotificationID(projectID string) string {
	if n.Decision != nil {
		return fmt.Sprintf("evolve-decision/%s/%s/%d", projectID, n.ReviewID, n.Decision.Revision)
	}
	return fmt.Sprintf("evolve/%s/%s", projectID, n.ReviewID)
}

// maximumNoticeTextBytes bounds a notice's text, so that the notification,
// its JSON and preamble included, stays within the Session's 4 KiB.
const maximumNoticeTextBytes = 3 << 10

// reviewNotice is the notice of review record, which waits for the agent: its
// window and runs, the goals commit, each goal's value and its change since
// the previous measured review of the same definition, the newest three
// proposals with their status, decline reason and agent, and the procedure.
// The proposal history is cut first, then the goals, to fit the bound.
func reviewNotice(record ReviewRecord, reviews []ReviewRecord, proposals []ProposalRecord, at time.Time) EvolveNotice {
	head := fmt.Sprintf("Review %s of the goals of the Live build %s waits for your report. It read %d runs of the Live process from %s to %s",
		record.ID, shortCommit(record.GoalsCommit), record.Export.Runs, record.Window.From.Format(time.DateOnly), record.Window.To.Format(time.DateOnly))
	if record.Export.Truncated {
		head += ", cut at the export's limit"
	}
	tail := fmt.Sprintf("Follow the review procedure of references/evolve.md (dex-app-builder): place_evolve_export with review_id %s, an investigate "+
		"subagent and at most one change subagent with inputs [.local/superverse-evolve/%s], then report_evolve_review. Only this review's Live runs and "+
		"the goals.md it places count; the workspace's .superverse/goals.md is a draft.", record.ID, record.ID)
	goals, history := goalLines(record, reviews), proposalLines(proposals)
	for {
		text := strings.Join(append(append(append([]string{head + "."}, goals...), history...), tail), "\n")
		switch {
		case len(text) <= maximumNoticeTextBytes:
			return EvolveNotice{ReviewID: record.ID, OccurredAt: at, Text: text}
		case len(history) > 0:
			history = history[:len(history)-1]
		case len(goals) > 0:
			goals = goals[:len(goals)-1]
		default:
			return EvolveNotice{ReviewID: record.ID, OccurredAt: at, Text: comm.TruncateUTF8(text, maximumNoticeTextBytes)}
		}
	}
}

// goalLines are a review's goal values with their change since the newest
// earlier review that measured the same definition.
func goalLines(record ReviewRecord, reviews []ReviewRecord) []string {
	if record.Metrics == nil {
		return nil
	}
	reviews = slices.Clone(reviews)
	newestFirst(reviews)
	lines := []string{"Goals now:"}
	for _, goal := range record.Metrics.Goals {
		line := fmt.Sprintf("- %s: %s over %d runs", goal.ID, describeValue(goal.Value), goal.Runs)
		if previous, at := previousValue(goal, record.ID, reviews); previous != nil && goal.Value != nil {
			line += fmt.Sprintf(" (%+.4g since %s)", *goal.Value-*previous, at)
		}
		if goal.Holds != nil {
			line += map[bool]string{true: ", guardrail holds", false: ", guardrail broken"}[*goal.Holds]
		}
		if goal.FewRuns {
			line += ", few runs"
		}
		lines = append(lines, line)
	}
	return lines
}

// previousValue is the value of goal in the newest review before current that
// measured the same definition, with that review.
func previousValue(goal GoalSummary, current string, newestFirst []ReviewRecord) (*float64, string) {
	for _, review := range newestFirst {
		if review.Metrics == nil || reviewNumber(review.ID) >= reviewNumber(current) {
			continue
		}
		for _, earlier := range review.Metrics.Goals {
			if earlier.ID == goal.ID && earlier.Digest == goal.Digest {
				return earlier.Value, review.ID
			}
		}
	}
	return nil, ""
}

// proposalLines are the newest three proposals: their status, title, agent
// and a decline reason.
func proposalLines(proposals []ProposalRecord) []string {
	proposals = slices.Clone(proposals)
	newestProposalsFirst(proposals)
	if len(proposals) == 0 {
		return []string{"No proposal so far."}
	}
	lines := []string{"Recent proposals:"}
	for _, p := range proposals[:min(3, len(proposals))] {
		line := fmt.Sprintf("- %s %s: %q (change subagent %s)", p.ReviewID, p.Status, p.Title, p.AgentID)
		if d := p.Decision; d != nil && d.Decision == DecisionDecline && d.Reason != "" {
			line += fmt.Sprintf("; declined because %q", comm.TruncateUTF8(d.Reason, 300))
		}
		lines = append(lines, line)
	}
	return lines
}

// decisionNotice is the notice of a person's decision on a proposal, with
// what the agent does next.
func decisionNotice(decision DecisionNotice, at time.Time) EvolveNotice {
	text := fmt.Sprintf("A person approved the proposal of review %s (revision %d). Apply the result of the change subagent %s with "+
		"merge_agent_changes apply, resolve conflicts with the work done since, push it with commit_and_push, then report_evolve_review "+
		"with phase applied and the pushed commit.", decision.ReviewID, decision.Revision, decision.AgentID)
	if decision.Decision == DecisionDecline {
		text = fmt.Sprintf("A person declined the proposal of review %s (revision %d)", decision.ReviewID, decision.Revision)
		if decision.Reason != "" {
			text += fmt.Sprintf(" because %q", comm.TruncateUTF8(decision.Reason, 600))
		}
		text += fmt.Sprintf(". Discard the result of the change subagent %s with merge_agent_changes discard, then report_evolve_review with phase "+
			"discarded. Later reviews do not propose the same change unless the evidence changed.", decision.AgentID)
	}
	return EvolveNotice{ReviewID: decision.ReviewID, Decision: &decision, OccurredAt: at, Text: text}
}

func describeValue(value *float64) string {
	if value == nil {
		return "no value"
	}
	return fmt.Sprintf("%.4g", *value)
}

func shortCommit(commit string) string { return commit[:min(12, len(commit))] }

// deliverNotice classifies a notice's delivery: delivered, skipped with the
// reason the Session gave, or neither when its queue is full and the
// delivery is asked again; any other error is returned.
func deliverNotice(err error) (bool, ReviewReason, error) {
	var skip *SessionSkip
	switch {
	case err == nil:
		return true, "", nil
	case errors.As(err, &skip):
		return false, skip.Reason, nil
	case errors.Is(err, ErrSessionBusy):
		return false, "", nil
	}
	return false, "", fmt.Errorf("tell the Session: %w", err)
}
