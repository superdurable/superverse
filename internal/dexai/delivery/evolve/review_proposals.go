// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package evolve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve/goals"
)

// The values below are persisted in the Proposals AttributeMap and in the
// RPC payloads of the proposal lifecycle; their field names are their JSON
// encoding.

// ProposalKind is what a proposal does: change the process, try two
// versions side by side, or undo a live proposal.
type ProposalKind string

const (
	ProposalChange ProposalKind = "change"
	ProposalTrial  ProposalKind = "trial"
	ProposalUndo   ProposalKind = "undo"
)

// ProposalStatus is a proposal's lifecycle. A person approves or declines an
// offered one; the agent applies an approved one to Preview or discards a
// declined, superseded or stale one; Publish makes an applied one live and a
// rollback, or a live undo, makes a live one undone.
type ProposalStatus string

const (
	ProposalOffered    ProposalStatus = "offered"
	ProposalApproved   ProposalStatus = "approved"
	ProposalDeclined   ProposalStatus = "declined"
	ProposalApplied    ProposalStatus = "applied"
	ProposalDiscarded  ProposalStatus = "discarded"
	ProposalLive       ProposalStatus = "live"
	ProposalUndone     ProposalStatus = "undone"
	ProposalSuperseded ProposalStatus = "superseded"
	ProposalStale      ProposalStatus = "stale"
)

// ProposalDecision is a person's answer to an offered proposal.
type ProposalDecision string

const (
	DecisionApprove ProposalDecision = "approve"
	DecisionDecline ProposalDecision = "decline"
)

// status is the status a decision gives the proposal.
func (d ProposalDecision) status() ProposalStatus {
	switch d {
	case DecisionApprove:
		return ProposalApproved
	case DecisionDecline:
		return ProposalDeclined
	}
	panic("evolve: undeclared decision " + string(d))
}

// ProposalRecord is the proposal of one review, the Proposals entry under the
// review's key r-<n>.
type ProposalRecord struct {
	ReviewID string
	Kind     ProposalKind
	// Undoes is the review of the live proposal an undo proposal reverts.
	Undoes  string `json:",omitempty"`
	Title   string
	Summary string
	// AgentID is the change subagent whose result holds the change.
	AgentID string
	// Backtest is the summary of the change's backtest, bound to this review's export, at most maximumBacktestBytes as JSON.
	Backtest *goals.BacktestSummary `json:",omitempty"`
	// Object is the stored proposal document.
	Object   objectstore.ObjectRef
	Status   ProposalStatus
	StatusAt time.Time
	// Revision grows with every change; a decision names the revision it saw.
	Revision  uint64
	OfferedAt time.Time
	// NotifyFailed is set when the decision could not reach the Session.
	NotifyFailed  bool                    `json:",omitempty"`
	Decision      *ProposalDecisionRecord `json:",omitempty"`
	AppliedCommit string                  `json:",omitempty"`
	AppliedAt     time.Time               `json:",omitzero"`
	// LiveRevision is the Live revision whose build first held the applied commit.
	LiveRevision uint64    `json:",omitempty"`
	LiveAt       time.Time `json:",omitzero"`
	// PreviousLiveReleaseID is the Live release that ran just before LiveRevision, which Undo rolls Live back to;
	// empty when it was not recorded.
	PreviousLiveReleaseID string `json:",omitempty"`
}

// ProposalDecisionRecord is who decided, when, why, and on which revision.
type ProposalDecisionRecord struct {
	Decision ProposalDecision
	Actor    string
	At       time.Time
	Reason   string `json:",omitempty"`
	Revision uint64
}

// ModelUsage is the platform's model calls of one model during a review's
// agent turn and its subagents, the review's billable usage.
type ModelUsage struct {
	Model                            string
	Calls, InputTokens, OutputTokens int64
}

// ReportPhase is what the Session reports on a review.
type ReportPhase string

const (
	// ReportReported is the review's verdict, with its proposal.
	ReportReported ReportPhase = "reported"
	// ReportApplied is an approved proposal pushed to Preview.
	ReportApplied ReportPhase = "applied"
	// ReportDiscarded is a proposal the agent dropped after it was declined, superseded or stale.
	ReportDiscarded ReportPhase = "discarded"
	// ReportSkipped is a waiting review the Session can no longer take, such
	// as after its switch to a local agent; it ends skipped with Reason.
	ReportSkipped ReportPhase = "skipped"
)

// ReviewVerdict is the agent's verdict, the status that ends the review.
type ReviewVerdict string

const (
	VerdictProposal = ReviewVerdict(ReviewProposal)
	VerdictNoChange = ReviewVerdict(ReviewNoChange)
)

// ReviewReport is the Session's report on review ReviewID.
type ReviewReport struct {
	ReviewID string
	Phase    ReportPhase
	// Verdict, Proposal and Usage belong to a reported review.
	Verdict  ReviewVerdict  `json:",omitempty"`
	Proposal *ProposalOffer `json:",omitempty"`
	Usage    []ModelUsage   `json:",omitempty"`
	// AppliedCommit is the commit an applied proposal was pushed as.
	AppliedCommit string `json:",omitempty"`
	// Defect is why the proposal's files cannot back it, which the review
	// answers after its own stale check.
	Defect *ReportDefect `json:",omitempty"`
	// Reason is why a skipped review's Session cannot take it.
	Reason ReviewReason `json:",omitempty"`
}

// ProposalOffer is a proposal as the agent reports it; the client decodes
// its backtest and open-run check from the change's files, stores its
// document and names the stored Object.
type ProposalOffer struct {
	Kind                    ProposalKind
	Undoes                  string `json:",omitempty"`
	Title, Summary, AgentID string
	Backtest                *goals.BacktestSummary `json:",omitempty"`
	OpenRuns                *goals.OpenRunsSummary `json:",omitempty"`
	Object                  objectstore.ObjectRef
}

// ReviewFiles are the files of the change subagent's result that a reported
// proposal rests on: proposal.md, backtest.json and open-runs.json, as the
// Session read them. Unreadable says why it could not read them, such as a
// missing file or one over its limit.
type ReviewFiles struct {
	Proposal, Backtest, OpenRuns []byte
	Unreadable                   *ReportDefect
}

// ReportDefect is a refusal the review answers for a report whose files
// cannot back a proposal, with its detail.
type ReportDefect struct {
	Refusal ReportRefusal
	Detail  string
}

// ReportRefusal is why ReportReview recorded nothing.
type ReportRefusal string

const (
	// ReportStaleReview is a review that is not waiting for its report.
	ReportStaleReview ReportRefusal = "stale_review"
	// ReportStaleProposal is a proposal in a status the report cannot follow.
	ReportStaleProposal = ReportRefusal(DecisionStaleProposal)
	// ReportExportMismatch is a backtest or open-run check that is not bound
	// to the review's own Live export, goals commit and goal definitions.
	ReportExportMismatch ReportRefusal = "export_mismatch"
	// ReportInvalidBacktest is a backtest or open-run check that cannot be
	// read, or one a gate refuses; the detail names the gate.
	ReportInvalidBacktest ReportRefusal = "invalid_backtest"
	// ReportTooLarge is a proposal file over its limit.
	ReportTooLarge ReportRefusal = "too_large"
)

// ReportAnswer answers ReportReview: the refusal with its detail, or the
// recorded proposal and, for a reported review, Open: the reviews whose
// proposals are offered or approved once it is recorded, whose change
// subagents the Session keeps pinned.
type ReportAnswer struct {
	Refusal  ReportRefusal   `json:",omitempty"`
	Detail   string          `json:",omitempty"`
	Proposal *ProposalRecord `json:",omitempty"`
	Open     []string        `json:",omitempty"`
}

// DecisionRequest is a person's decision on the proposal of ReviewID at the
// Revision they saw.
type DecisionRequest struct {
	ReviewID string
	Revision uint64
	Decision ProposalDecision
	Reason   string `json:",omitempty"`
	Actor    string
}

// DecisionRefusal is why DecideProposal recorded no decision.
type DecisionRefusal string

// DecisionStaleProposal is a proposal that is no longer offered at the revision the person saw.
const DecisionStaleProposal DecisionRefusal = "stale_proposal"

// DecisionAnswer answers DecideProposal: the refusal and the proposal as it is.
type DecisionAnswer struct {
	Refusal  DecisionRefusal `json:",omitempty"`
	Proposal *ProposalRecord `json:",omitempty"`
}

// DecisionNotice is a recorded decision as the Project's Session is told of
// it, the input of NotifyDecision and RecordNotifyFailure.
type DecisionNotice struct {
	ReviewID, AgentID string
	Revision          uint64
	Decision          ProposalDecision
	Reason            string `json:",omitempty"`
}

// Limits of the proposals EvolveFlow keeps and serves.
const (
	maximumTitleRunes    = 120
	MaximumReasonRunes   = 500
	maximumProposalBytes = 32 << 10
	maximumUsageModels   = 16
	maximumDetailBytes   = 1 << 10
	maximumAgentIDBytes  = 128
	approvedStaleAfter   = 7 * 24 * time.Hour
	appliedStaleAfter    = 30 * 24 * time.Hour
)

// ErrInvalidReport is a report its caller should not have sent, such as a
// title or summary over its bound.
var ErrInvalidReport = errors.New("invalid review report")

// ErrNoProposal reports a review without a kept proposal.
var ErrNoProposal = errors.New("the review has no proposal")

// ValidateReviewID refuses an ID that is not a review key r-<n>.
func ValidateReviewID(id string) error {
	number, found := strings.CutPrefix(id, "r-")
	if !found || number == "" || len(number) > 18 || strings.TrimLeft(number, "0123456789") != "" || number[0] == '0' {
		return fmt.Errorf("review ID %q is not r-<n>", id)
	}
	return nil
}

// validate refuses a report whose phase lacks or mixes its fields.
func (r ReviewReport) validate() error {
	if err := ValidateReviewID(r.ReviewID); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidReport, err)
	}
	reported := r.Phase == ReportReported
	switch {
	case !slices.Contains([]ReportPhase{ReportReported, ReportApplied, ReportDiscarded, ReportSkipped}, r.Phase):
		return fmt.Errorf("%w: phase %q", ErrInvalidReport, r.Phase)
	case (r.Phase == ReportSkipped) != (r.Reason != "") || (r.Reason != "" && !slices.Contains(sessionSkips, r.Reason)):
		return fmt.Errorf("%w: a skipped review, and only one, names why its Session cannot take it", ErrInvalidReport)
	case reported != (r.Verdict != "") || (!reported && (r.Proposal != nil || len(r.Usage) > 0)):
		return fmt.Errorf("%w: only a reported review has a verdict, a proposal and usage", ErrInvalidReport)
	case (r.Phase == ReportApplied) != (r.AppliedCommit != "") || (r.AppliedCommit != "" && !flowid.IsCommitSHA(r.AppliedCommit)):
		return fmt.Errorf("%w: an applied proposal, and only one, names its commit SHA", ErrInvalidReport)
	case reported && !slices.Contains([]ReviewVerdict{VerdictProposal, VerdictNoChange}, r.Verdict):
		return fmt.Errorf("%w: verdict %q", ErrInvalidReport, r.Verdict)
	case reported && (r.Verdict == VerdictProposal) != (r.Proposal != nil):
		return fmt.Errorf("%w: the verdict proposal, and only it, carries a proposal", ErrInvalidReport)
	case r.Defect != nil && (r.Proposal == nil || !slices.Contains([]ReportRefusal{ReportInvalidBacktest, ReportTooLarge}, r.Defect.Refusal)):
		return fmt.Errorf("%w: only a proposal's files are refused invalid_backtest or too_large", ErrInvalidReport)
	case len(r.Usage) > maximumUsageModels:
		return fmt.Errorf("%w: usage of %d models; at most %d are kept", ErrInvalidReport, len(r.Usage), maximumUsageModels)
	}
	for _, usage := range r.Usage {
		if usage.Model == "" || len(usage.Model) > maximumAgentIDBytes || usage.Calls < 0 || usage.InputTokens < 0 || usage.OutputTokens < 0 {
			return fmt.Errorf("%w: usage %+v", ErrInvalidReport, usage)
		}
	}
	if r.Proposal != nil {
		return r.Proposal.validate(r.Defect == nil)
	}
	return nil
}

// validate refuses an offer that the record could not keep or Studio not
// show; read is whether its files were read, which gives it a backtest, an
// open-run check and a stored document.
func (o ProposalOffer) validate(read bool) error {
	undo := o.Kind == ProposalUndo
	switch {
	case !slices.Contains([]ProposalKind{ProposalChange, ProposalTrial, ProposalUndo}, o.Kind):
		return fmt.Errorf("%w: proposal kind %q", ErrInvalidReport, o.Kind)
	case undo == (o.Undoes == "") || (undo && ValidateReviewID(o.Undoes) != nil):
		return fmt.Errorf("%w: an undo proposal, and only one, names the review it undoes", ErrInvalidReport)
	case strings.TrimSpace(o.Title) == "" || utf8.RuneCountInString(o.Title) > maximumTitleRunes || strings.ContainsAny(o.Title, "\r\n"):
		return fmt.Errorf("%w: the title is one line of 1 to %d characters", ErrInvalidReport, maximumTitleRunes)
	case len(o.Summary) > maximumSummaryBytes || !utf8.ValidString(o.Title+o.Summary):
		return fmt.Errorf("%w: the summary is UTF-8 of at most %d bytes", ErrInvalidReport, maximumSummaryBytes)
	case o.AgentID == "" || len(o.AgentID) > maximumAgentIDBytes:
		return fmt.Errorf("%w: the agent ID has 1 to %d bytes", ErrInvalidReport, maximumAgentIDBytes)
	case read != (o.Backtest != nil && o.OpenRuns != nil && o.Object.Key != ""):
		return fmt.Errorf("%w: a proposal whose files were read, and only one, has its backtest, its open-run check and its stored document", ErrInvalidReport)
	case o.Backtest != nil && len(comm.Must(json.Marshal(o.Backtest.Kept()))) > maximumBacktestBytes:
		return fmt.Errorf("%w: the backtest summary takes more than %d bytes", ErrInvalidReport, maximumBacktestBytes)
	}
	return nil
}

// validateDocument refuses a proposal document Studio cannot serve.
func validateDocument(document []byte) error {
	if len(document) == 0 || len(document) > maximumProposalBytes || !utf8.Valid(document) {
		return fmt.Errorf("%w: the proposal is UTF-8 of 1 to %d bytes", ErrInvalidReport, maximumProposalBytes)
	}
	return nil
}

// proposalKey is where a review's proposal document is stored, named by its
// content: a repeated report stores the same object, and a report the review
// refused never blocks a corrected one.
func proposalKey(projectID, reviewID string, document []byte) string {
	sum := sha256.Sum256(document)
	return projectID + "/evolve/proposals/" + reviewID + "-" + hex.EncodeToString(sum[:8]) + ".md"
}

// offered is the new offered proposal of review reviewID.
func (o ProposalOffer) offered(reviewID string, now time.Time) ProposalRecord {
	kept := o.Backtest.Kept()
	return ProposalRecord{
		ReviewID: reviewID, Kind: o.Kind, Undoes: o.Undoes, Title: o.Title, Summary: o.Summary, AgentID: o.AgentID, Backtest: &kept,
		Object: o.Object, Status: ProposalOffered, StatusAt: now, Revision: 1, OfferedAt: now,
	}
}

// refusal is why the review record refuses offer, empty when it admits it:
// its backtest and open-run check must be bound to the review's export and
// goals copy, and no gate may refuse them.
func (r ReviewRecord) refusal(offer ProposalOffer) (ReportRefusal, string) {
	values := map[string]*float64{}
	if r.Metrics != nil {
		for _, goal := range r.Metrics.Goals {
			values[goal.ID] = goal.Value
		}
	}
	facts := goals.ReviewFacts{ReviewID: r.ID, GoalsCommit: r.GoalsCommit, RunsSHA256: r.Export.RunsSHA256, Records: r.Export.Records, Values: values}
	err := goals.CheckReview(Goals{Spec: r.GoalsSpec}.spec(), facts, *offer.Backtest, *offer.OpenRuns, offer.Kind == ProposalTrial)
	switch {
	case err == nil:
		return "", ""
	case errors.Is(err, goals.ErrExportMismatch):
		return ReportExportMismatch, comm.TruncateUTF8(err.Error(), maximumDetailBytes)
	}
	return ReportInvalidBacktest, comm.TruncateUTF8(err.Error(), maximumDetailBytes)
}

// moved is the proposal in status, as of now, at its next revision.
func (p ProposalRecord) moved(status ProposalStatus, now time.Time) ProposalRecord {
	p.Status, p.StatusAt, p.Revision = status, now, p.Revision+1
	return p
}

// repeats reports whether a reported review's record already holds this report.
func repeats(record ReviewRecord, proposal *ProposalRecord, in ReviewReport) bool {
	if record.Status != ReviewStatus(in.Verdict) {
		return false
	}
	if in.Proposal == nil {
		return proposal == nil
	}
	return proposal != nil && proposal.Object == in.Proposal.Object
}

// decide records a decision on an offered proposal at the revision the
// person saw; the same decision repeated answers the record unchanged.
func (p ProposalRecord) decide(in DecisionRequest, now time.Time) (ProposalRecord, DecisionRefusal) {
	if d := p.Decision; d != nil && d.Revision == in.Revision && d.Decision == in.Decision && d.Actor == in.Actor && d.Reason == in.Reason {
		return p, ""
	}
	if p.Status != ProposalOffered || p.Revision != in.Revision {
		return p, DecisionStaleProposal
	}
	next := p.moved(in.Decision.status(), now)
	next.Decision = &ProposalDecisionRecord{Decision: in.Decision, Actor: in.Actor, At: now, Reason: in.Reason, Revision: in.Revision}
	return next, ""
}

// notice is the recorded decision as the Session is told of it.
func (p ProposalRecord) notice() DecisionNotice {
	return DecisionNotice{ReviewID: p.ReviewID, AgentID: p.AgentID, Revision: p.Revision, Decision: p.Decision.Decision, Reason: p.Decision.Reason}
}

// report follows the agent applying or discarding a decided proposal; a
// repeated report answers the record unchanged.
func (p ProposalRecord) report(in ReviewReport, now time.Time) (ProposalRecord, ReportRefusal) {
	switch {
	case in.Phase == ReportApplied && p.AppliedCommit == in.AppliedCommit, in.Phase == ReportDiscarded && p.Status == ProposalDiscarded:
		return p, ""
	case in.Phase == ReportApplied && p.Status == ProposalApproved:
		next := p.moved(ProposalApplied, now)
		next.AppliedCommit, next.AppliedAt = in.AppliedCommit, now
		return next, ""
	case in.Phase == ReportDiscarded && slices.Contains([]ProposalStatus{ProposalDeclined, ProposalSuperseded, ProposalStale}, p.Status):
		return p.moved(ProposalDiscarded, now), ""
	}
	return p, ReportStaleProposal
}

// sweptProposals are the proposals the expiry makes stale: approved and not
// applied within 7 days, or applied and not live within 30.
func sweptProposals(proposals []ProposalRecord, now time.Time) []ProposalRecord {
	var swept []ProposalRecord
	for _, p := range proposals {
		approvedLong := p.Status == ProposalApproved && now.Sub(p.Decision.At) >= approvedStaleAfter
		appliedLong := p.Status == ProposalApplied && now.Sub(p.AppliedAt) >= appliedStaleAfter
		if approvedLong || appliedLong {
			swept = append(swept, p.moved(ProposalStale, now))
		}
	}
	return swept
}

// openProposals are the reviews whose proposals are offered or approved once
// changed is stored over proposals, in review order.
func openProposals(proposals []ProposalRecord, changed ...ProposalRecord) []string {
	statuses := map[string]ProposalStatus{}
	for _, p := range slices.Concat(proposals, changed) {
		statuses[p.ReviewID] = p.Status
	}
	var open []string
	for reviewID, status := range statuses {
		if status == ProposalOffered || status == ProposalApproved {
			open = append(open, reviewID)
		}
	}
	slices.Sort(open)
	return open
}

// supersededProposals are the offered proposals a newer offer of review newest supersedes.
func supersededProposals(proposals []ProposalRecord, newest string, now time.Time) []ProposalRecord {
	var superseded []ProposalRecord
	for _, p := range proposals {
		if p.Status == ProposalOffered && p.ReviewID != newest {
			superseded = append(superseded, p.moved(ProposalSuperseded, now))
		}
	}
	return superseded
}

// liveMoves are the proposals the Live build of in changes, by in.Holds,
// which says for each applied or live proposal whether the build holds its
// applied commit: an applied one it holds goes live, recording the release
// Live ran before when that release's build lacked its commit
// (in.PreviousHolds), a live one it lacks is undone (a rollback), and a live
// undo makes the proposal it undoes undone.
func liveMoves(proposals []ProposalRecord, in goalsReading, now time.Time) []ProposalRecord {
	byReview := map[string]ProposalRecord{}
	for _, p := range proposals {
		held, read := in.Holds[p.ReviewID]
		switch {
		case !read:
		case p.Status == ProposalApplied && held:
			next := p.moved(ProposalLive, now)
			next.LiveRevision, next.LiveAt = in.Revision, now
			if before, checked := in.PreviousHolds[p.ReviewID]; checked && !before {
				next.PreviousLiveReleaseID = in.PreviousReleaseID
			}
			byReview[p.ReviewID] = next
		case p.Status == ProposalLive && !held:
			byReview[p.ReviewID] = p.moved(ProposalUndone, now)
		}
	}
	for _, p := range proposals {
		moved, changed := byReview[p.ReviewID]
		if !changed || moved.Kind != ProposalUndo || moved.Status != ProposalLive {
			continue
		}
		for _, target := range proposals {
			if target.ReviewID == moved.Undoes && target.Status == ProposalLive {
				byReview[target.ReviewID] = target.moved(ProposalUndone, now)
			}
		}
	}
	moves := make([]ProposalRecord, 0, len(byReview))
	for _, p := range proposals {
		if moved, changed := byReview[p.ReviewID]; changed {
			moves = append(moves, moved)
		}
	}
	return moves
}

// ancestryChecked are the proposals whose applied commit a Live build is
// checked for: the applied ones and the live ones.
func ancestryChecked(proposals []ProposalRecord) []ProposalRecord {
	return slices.DeleteFunc(slices.Clone(proposals), func(p ProposalRecord) bool { return p.Status != ProposalApplied && p.Status != ProposalLive })
}

// goingLive are the applied proposals whose commit a Live build holds, by
// holds, which go live with it; the build Live ran before is checked for them.
func goingLive(proposals []ProposalRecord, holds map[string]bool) []ProposalRecord {
	return slices.DeleteFunc(slices.Clone(proposals), func(p ProposalRecord) bool { return p.Status != ProposalApplied || !holds[p.ReviewID] })
}

// prunedProposals are the proposals beyond the newest keptReviews whose
// lifecycle ended; an offered, approved or applied one is kept.
func prunedProposals(proposals []ProposalRecord) []ProposalRecord {
	proposals = slices.Clone(proposals)
	newestProposalsFirst(proposals)
	return slices.DeleteFunc(proposals[min(len(proposals), keptReviews):], func(p ProposalRecord) bool {
		return slices.Contains([]ProposalStatus{ProposalOffered, ProposalApproved, ProposalApplied}, p.Status)
	})
}

// newestProposalsFirst orders proposals by their review's number, newest first.
func newestProposalsFirst(proposals []ProposalRecord) {
	slices.SortFunc(proposals, func(a, b ProposalRecord) int { return int(reviewNumber(b.ReviewID) - reviewNumber(a.ReviewID)) })
}
