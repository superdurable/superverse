// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

const (
	// evolveAgentTimeout bounds the agent's review: its turns, an
	// investigate and a change subagent that replays two trees in the
	// Sandbox, and, after the approval, the push through the commit gate.
	evolveAgentTimeout = 60 * time.Minute
	// shortTexts is how many of reviewTexts are under the 20 bytes the
	// evolve-review scenario's change refuses.
	shortTexts = 3
)

// reviewTexts are the Live runs an agent review measures: the Live build of
// the summary-approval example accepts every one, and the evolve-review
// scenario's change refuses the first shortTexts, which are under 20 bytes.
var reviewTexts = []string{
	"Too short.", "Hi there.", "Ok, noted.",
	"Dex keeps every Flow's state durable.",
	"The connector shipped on time, and the release notes went out the same afternoon.",
	"A summary waits for a person to approve it in Studio before it is saved.",
	"Goal measurement reads the runs of the Live build and evaluates its goals.",
	"The review exports the Live runs, places them for the agent and backtests a change over them.",
	"Short texts reach the model like any other request today.",
}

// TestEvolveReviewDexIntegration runs an agent review from its notification
// to Preview on a Project created from the summary-approval example, whose
// Live and Preview runs reach the model through the lane's deterministic
// summary endpoint, with the lane's server Worker, Live engine, Sandbox and
// coding image, the local Git host, storage and the mock model's
// evolve-review scenario:
//   - the scenario's goals, pushed and announced as the Live build's, and
//     Live runs with texts on both sides of 20 bytes; Review now measures them
//     and waits for the agent with its runs digest and goal definition
//     digests;
//   - the Session takes the review's evolve notification while idle and
//     opens its own turn: place_evolve_export places the review's Live
//     export, read-only, with its runs digest, and refuses a review without
//     a stored export (stale_review); an investigate subagent given the
//     folder as its input measures it with the image's superverse-evolve,
//     which refuses export, --runs, another review's folder, a manifest whose
//     source is not live and edited runs;
//   - its backtest over another review's folder is refused export_mismatch,
//     a report of a review that does not wait is stale_review, and an applied
//     report before any approval is stale_proposal;
//   - a change subagent raises the rule to 20 bytes, replays the Live build's
//     rule (from the goals commit) and its own over the runs, compares them
//     and checks the open runs; the review offers its proposal, bound to the
//     review's export, with the short runs changed exactly and the rest the
//     same, under the 8 KiB summary cap; the change subagent's record is
//     pinned for the review, and the review keeps the platform model usage
//     of its turns and subagents; GetEvolveProposal serves the document;
//   - Approve through DecideEvolveProposal sends the decision's notification,
//     whose turn merges the change, has the commit gate refuse a force-added
//     file of the export, pushes the change, which the Preview serves, and
//     reports it applied, which unpins the record; announcing the applied
//     commit as Live moves the proposal live.
//
// Decline and Discard need a second review of the Project, which Review now
// admits once a day; the proposal API's Dex test covers them.
func TestEvolveReviewDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), evolveTestTimeout)
	defer cancel()
	project := agents.NewEvolveProjectForTest(t, ctx, "summary-approval")
	connection, caller := projecttest.LoginForTest(ctx, t, project.Account)
	api := pb.NewEvolveServiceClient(connection)
	first := awaitEvolve(ctx, t, project, "the first Live announcement", func(s evolve.Snapshot) bool { return !s.Goals.ReadAt.IsZero() })
	goalsCommit := project.PushGoalsForTest(ctx, mockInput(t, "evolve-review", 1).Content)
	announce(ctx, t, project, first.Live.Revision+1, goalsCommit)
	measured := awaitEvolve(ctx, t, project, "the review's goals", func(s evolve.Snapshot) bool { return s.Goals.Commit == goalsCommit })
	if measured.Goals.Status != evolve.GoalsValid {
		t.Fatalf("the goals of %s are not valid: %+v", goalsCommit, measured.Goals)
	}
	startLiveRuns(ctx, t, project.LiveProjectForTest, reviewTexts, summaryApprovalStep)

	answer := startReview(ctx, t, api, caller, project.ProjectID, uuid.NewString())
	reviewID := answer.GetReviewId()
	if reviewID == "" {
		t.Fatalf("Review now was not admitted: %v", answer)
	}
	waiting := awaitReview(ctx, t, project, reviewID)
	if waiting.Status != evolve.ReviewWaitingAgent || len(waiting.Export.RunsSHA256) != 64 || waiting.Export.Records != len(reviewTexts) || waiting.Metrics == nil {
		t.Fatalf("review %s does not wait for the agent with the digest of its %d runs: %+v", reviewID, len(reviewTexts), waiting)
	}
	for _, goal := range waiting.Metrics.Goals {
		if goal.Digest == "" {
			t.Fatalf("goal %s of review %s has no definition digest: %+v", goal.ID, reviewID, waiting.Metrics.Goals)
		}
	}
	agentWait, stop := context.WithTimeout(ctx, evolveAgentTimeout)
	offered := awaitEvolve(agentWait, t, project, "the agent's proposal", func(s evolve.Snapshot) bool {
		return reviewStatus(s, reviewID) == evolve.ReviewProposal && proposalOf(s, reviewID) != nil
	})
	proposal := requireOfferedProposal(t, offered, waiting, goalsCommit)
	// The Session writes the pin and the later refusals after the review recorded the proposal.
	project.AwaitReplyForTest(agentWait, "The review proposes to skip texts too short to summarize")
	stop()
	requireReviewTurns(ctx, t, project, waiting)
	requirePinnedFor(t, project.SubagentsForTest(ctx), proposal.AgentID, reviewID)
	document, err := api.GetEvolveProposal(ctx, &pb.GetEvolveProposalRequest{ProjectId: project.ProjectID, ReviewId: reviewID, Caller: caller})
	if err != nil || !strings.Contains(document.GetMarkdown(), "# Skip texts too short to summarize") {
		t.Fatalf("reading the proposal of review %s answered %v (%T)", reviewID, document, err)
	}

	decided, err := api.DecideEvolveProposal(ctx, &pb.DecideEvolveProposalRequest{ProjectId: project.ProjectID, ReviewId: reviewID, Revision: proposal.Revision,
		Decision: pb.EvolveProposalDecision_EVOLVE_PROPOSAL_DECISION_APPROVE, Caller: caller})
	if err != nil || decided.GetRefusal() != pb.EvolveProposalRefusal_EVOLVE_PROPOSAL_REFUSAL_UNSPECIFIED ||
		decided.GetProposal().GetStatus() != pb.EvolveProposalStatus_EVOLVE_PROPOSAL_STATUS_APPROVED {
		t.Fatalf("approving the proposal of review %s answered %v (%T)", reviewID, decided, err)
	}
	agentWait, stop = context.WithTimeout(ctx, evolveAgentTimeout)
	applied := awaitEvolve(agentWait, t, project, "the applied proposal", func(s evolve.Snapshot) bool {
		p := proposalOf(s, reviewID)
		return p != nil && p.Status == evolve.ProposalApplied
	})
	appliedCommit := proposalOf(applied, reviewID).AppliedCommit
	if len(appliedCommit) != 40 || appliedCommit == goalsCommit {
		t.Fatalf("the applied proposal of review %s names commit %q", reviewID, appliedCommit)
	}
	// The Session unpins after the review recorded the applied report.
	project.AwaitReplyForTest(agentWait, "The approved change is on Preview")
	stop()
	for _, record := range project.SubagentsForTest(ctx) {
		if record.PinnedFor != "" {
			t.Fatalf("the applied review %s left subagent %s pinned for %s", reviewID, record.AgentID, record.PinnedFor)
		}
	}
	if !project.AwaitPreviewForTest(ctx, appliedCommit) {
		t.Fatalf("the Preview does not serve the applied commit %s of review %s", appliedCommit, reviewID)
	}
	announce(ctx, t, project, first.Live.Revision+2, appliedCommit)
	live := awaitEvolve(ctx, t, project, "the live proposal", func(s evolve.Snapshot) bool {
		p := proposalOf(s, reviewID)
		return p != nil && p.Status == evolve.ProposalLive
	})
	t.Logf("FlowID=%s review %s offered %q from %s, applied as %s and live at revision %d", flowid.EvolveFlowID(project.ProjectID), reviewID, proposal.Title,
		proposal.AgentID, appliedCommit, proposalOf(live, reviewID).LiveRevision)
}

// requirePinnedFor fails unless the Session keeps the record of the change
// subagent taskID, as a proposal names it (agent-<ID>), pinned for review
// reviewID.
func requirePinnedFor(t *testing.T, records []agenttasks.SubagentRecord, taskID, reviewID string) {
	t.Helper()
	for _, record := range records {
		if record.TaskID() != taskID {
			continue
		}
		if record.PinnedFor != reviewID {
			t.Fatalf("the change subagent %s of review %s is pinned for %q", taskID, reviewID, record.PinnedFor)
		}
		return
	}
	t.Fatalf("the Session keeps no record of the change subagent %s of review %s: %+v", taskID, reviewID, records)
}

// proposalOf is the proposal of review reviewID, nil when the snapshot keeps none.
func proposalOf(s evolve.Snapshot, reviewID string) *evolve.ProposalRecord {
	for i := range s.Proposals {
		if s.Proposals[i].ReviewID == reviewID {
			return &s.Proposals[i]
		}
	}
	return nil
}

// requireOfferedProposal checks the proposal the agent offered for review,
// which measured the Live build of goalsCommit: a change bound to the
// review's export and before side, whose backtest changed the short runs
// exactly and left the others the same, calibrated in full, with no gate,
// within the summary cap, and the review's usage of the platform model.
func requireOfferedProposal(t *testing.T, snapshot evolve.Snapshot, review evolve.ReviewRecord, goalsCommit string) evolve.ProposalRecord {
	t.Helper()
	proposal := *proposalOf(snapshot, review.ID)
	backtest := proposal.Backtest
	if proposal.Status != evolve.ProposalOffered || proposal.Kind != evolve.ProposalChange || proposal.Title != "Skip texts too short to summarize" || backtest == nil {
		t.Fatalf("review %s offered %+v", review.ID, proposal)
	}
	if export := backtest.Export; export.ReviewID != review.ID || export.GoalsCommit != goalsCommit || export.BeforeCommit != goalsCommit ||
		export.RunsSHA256 != review.Export.RunsSHA256 || export.Records != review.Export.Records || export.BaseCommit == "" {
		t.Fatalf("the backtest of review %s is bound to %+v, not its export %+v", review.ID, export, review.Export)
	}
	if runs := backtest.Runs; runs.ChangedExact != shortTexts || runs.Same != len(reviewTexts)-shortTexts || runs.Unknown != 0 || runs.ChangedEstimated != 0 ||
		backtest.Calibration.Share == nil || *backtest.Calibration.Share != 1 || len(backtest.Gates) != 0 {
		t.Fatalf("the backtest of review %s does not change the %d short runs exactly: %+v", review.ID, shortTexts, backtest)
	}
	for _, goal := range backtest.Goals {
		if goal.ID == "summarized-share" && (goal.After == nil || *goal.After < 0.5 || goal.Known != len(reviewTexts)) {
			t.Fatalf("the backtest of review %s does not keep the summarized share's guardrail: %+v", review.ID, goal)
		}
	}
	if size := len(mustJSON(t, backtest)); size > 8<<10 {
		t.Fatalf("the proposal of review %s keeps a backtest summary of %d bytes", review.ID, size)
	}
	reviewed := review
	for _, candidate := range snapshot.Reviews {
		if candidate.ID == review.ID {
			reviewed = candidate
		}
	}
	calls := int64(0)
	for _, usage := range reviewed.Usage {
		calls += usage.Calls
	}
	if len(reviewed.Usage) == 0 || calls < 10 {
		t.Fatalf("review %s keeps no model usage of its turns and subagents: %+v", review.ID, reviewed.Usage)
	}
	return proposal
}

// requireReviewTurns checks the Session's history of the review: it took the
// review's evolve notification, placed the export with the review's runs
// digest, which the stored result names, and was refused a review without a
// stored export (stale_review); the report of the backtest over another
// review's folder was refused export_mismatch, the report of a review that
// does not wait stale_review and an applied report before any approval
// stale_proposal. The history's public messages carry each result's outcome
// and failure code, never its content.
func requireReviewTurns(ctx context.Context, t *testing.T, project agents.EvolveProjectForTest, review evolve.ReviewRecord) {
	t.Helper()
	var notified, placed, unplaced bool
	refusals := map[string]bool{}
	for _, message := range project.HistoryForTest(ctx) {
		if n := message.Message.Notification; n != nil && n.IsEvolve() && n.Evolve.ReviewID == review.ID {
			notified = true
		}
		name, execution := message.Message.ToolName, message.Message.ToolExecution
		if message.Message.Role != conversation.MessageRoleTool || name == nil || execution == nil {
			continue
		}
		switch {
		case *name == conversation.ToolPlaceEvolveExport && execution.Outcome == conversation.ToolOutcomeSucceeded:
			placed = placed || strings.Contains(project.ToolResultForTest(ctx, message.Sequence), review.Export.RunsSHA256)
		case *name == conversation.ToolPlaceEvolveExport:
			unplaced = unplaced || execution.ResultCode == toolfailure.EvolveReviewStale.String()
		case *name == conversation.ToolReportEvolveReview && execution.Outcome != conversation.ToolOutcomeSucceeded:
			refusals[execution.ResultCode] = true
		}
	}
	if !notified || !placed || !unplaced || !refusals[toolfailure.ExportMismatch.String()] || !refusals[toolfailure.EvolveReviewStale.String()] ||
		!refusals[toolfailure.EvolveProposalStale.String()] {
		t.Fatalf("the Session did not take review %s's notification (%t), place its export with digest %s (%t), get the placement's refusal (%t) or the "+
			"report refusals: %v", review.ID, notified, review.Export.RunsSHA256, placed, unplaced, refusals)
	}
}
