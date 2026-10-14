// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/flowdesign"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
)

// The outline of the approval design that design-revised confirms and
// summary-approval builds: its main story in reading order and the lost model
// response as its one failure-handling Step.
const approvalOutline = "TextSummaryFlow: PrepareSummary, GenerateSummary, AwaitSummaryApproval [Approve summary], SaveSummaryResult, RecordSummaryFailure; +1 failure-handling Step"

// flowDesign reads the design of slot at the digest of reference and fails
// the test unless the read returns it.
func (s *mockSession) flowDesign(ctx context.Context, slot flowdesign.Slot, reference *flowdesign.Reference) flowdesign.Design {
	s.t.Helper()
	if reference == nil {
		s.t.Fatalf("the %s requirements carry no design reference", slot)
	}
	design, err := s.client.GetFlowDesign(ctx, s.id, slot, reference.Digest)
	if err != nil {
		s.t.Fatalf("reading the %s design at %s: %v", slot, reference.Digest, err)
	}
	if design.Digest() != reference.Digest || design.Summary != reference.Summary {
		s.t.Fatalf("the %s design does not match its reference %+v", slot, reference)
	}
	return design
}

// requireDesignReadError fails unless reading slot at digest fails with want.
func (s *mockSession) requireDesignReadError(ctx context.Context, slot flowdesign.Slot, digest string, want error) {
	s.t.Helper()
	if _, err := s.client.GetFlowDesign(ctx, s.id, slot, digest); !errors.Is(err, want) {
		s.t.Fatalf("reading the %s design at %s returned %v, want %v", slot, digest, err, want)
	}
}

// awaitWait is the wait of the design's AwaitSummaryApproval Step.
func awaitWait(t *testing.T, design flowdesign.Design) flowdesign.Wait {
	t.Helper()
	for _, step := range design.Flows[0].Steps {
		if step.StepType == "AwaitSummaryApproval" {
			return step.WaitsFor
		}
	}
	t.Fatalf("the design has no AwaitSummaryApproval Step: %+v", design)
	return flowdesign.Wait{}
}

// A card with a Flow design stores its body as the pending design, which
// GetFlowDesign returns at the reference's digest; another digest is stale and
// the confirmed slot is empty. A request for changes confirms nothing, and the
// revised card replaces the pending design. Its confirmation copies the design
// to the confirmed slot in the commit that stores the confirmed requirements,
// which carry its reference; the design is not built yet. A compaction keeps
// the confirmed design's outline in the new segment, and a blocked checkpoint
// leaves the design unbuilt.
func TestSessionMockDesignRevisedDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "design-revised", 1).Content)

	agent := session.requirementsCard(ctx, 0)
	requireNoFailure(t, agent.History)
	first := *agent.Description.PendingUserInput
	review := session.flowDesign(ctx, flowdesign.SlotPending, first.Requirements.Design)
	if wait := awaitWait(t, review); wait.Kind != flowdesign.WaitKindChannel || wait.Name != "SummaryReviews" {
		t.Fatalf("the first design does not wait on the review Channel: %+v", wait)
	}
	if outline := first.Requirements.Design.Outline; !strings.Contains(outline, "AwaitSummaryApproval [SummaryReviews]") || !strings.HasSuffix(outline, "; +1 failure-handling Step") {
		t.Fatalf("the first outline does not list the main story with one failure-handling Step: %q", outline)
	}
	session.requireDesignReadError(ctx, flowdesign.SlotPending, "sha256:stale", sessionagent.ErrFlowDesignRevisionChanged)
	session.requireDesignReadError(ctx, flowdesign.SlotConfirmed, first.Requirements.Design.Digest, sessionagent.ErrFlowDesignNotFound)

	// The revised card replaces the pending design, and nothing is confirmed.
	answered := session.decide(ctx, first, agentloop.RequirementsDecision{RequestedChanges: "Make the review an approval in Studio."}, agent.Description.LastSequence)
	agent = session.requirementsCard(ctx, answered.Sequence)
	revised := *agent.Description.PendingUserInput
	if agent.Description.ConfirmedRequirements != nil || revised.Requirements.Design == nil || revised.Requirements.Design.Digest == first.Requirements.Design.Digest {
		t.Fatalf("the revised card does not carry a new design while nothing is confirmed: %+v", agent.Description)
	}
	session.requireDesignReadError(ctx, flowdesign.SlotPending, first.Requirements.Design.Digest, sessionagent.ErrFlowDesignRevisionChanged)
	approval := session.flowDesign(ctx, flowdesign.SlotPending, revised.Requirements.Design)
	if wait := awaitWait(t, approval); wait.Kind != flowdesign.WaitKindAction || wait.Name != "ApproveSummary" {
		t.Fatalf("the revised design does not wait for the Approve summary Action: %+v", wait)
	}
	if revised.Requirements.Design.Outline != approvalOutline {
		t.Fatalf("the revised outline is %q, want %q", revised.Requirements.Design.Outline, approvalOutline)
	}

	// The confirmation stores the design as confirmed, unbuilt, and the turn's
	// compaction keeps its outline: the last reply answers only when the new
	// segment carries it.
	agent = session.confirmed(ctx, revised, answered.Sequence, 1)
	if confirmed := session.flowDesign(ctx, flowdesign.SlotConfirmed, agent.Description.ConfirmedRequirements.Spec.Design); confirmed.Digest() != approval.Digest() {
		t.Fatalf("the confirmed design is not the revised one: %+v", confirmed)
	}
	if agent.Description.ConfirmedDesignBuilt {
		t.Fatal("a confirmed design reads as built before any push")
	}
	if final := agent.History.Messages[len(agent.History.Messages)-1].Message.Content; !strings.Contains(final, "survived the compaction") {
		t.Fatalf("the segment after the compaction does not carry the confirmed design's outline: %q", final)
	}

	// A blocked checkpoint pushes, but the build is not complete.
	before := agent.Description.LastSequence
	session.send(ctx, mockInput(t, "design-revised", 4).Content)
	agent = session.waitForSnapshot(ctx, "the turn that pushes a checkpoint", func(agent agentloop.AgentSnapshot) bool {
		return endsWithReplyAfter(agent, before) && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	requireNoFailure(t, agent.History)
	_, messages := session.history(ctx)
	after := slices.DeleteFunc(slices.Clone(messages), func(message conversation.SequencedMessage) bool { return message.Sequence <= before })
	if pushed := toolResult(t, after, conversation.ToolCommitAndPush, 0); pushed.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("the blocked checkpoint was not pushed: %+v", pushed)
	}
	if agent.Description.ConfirmedDesignBuilt {
		t.Fatal("a blocked checkpoint marked the confirmed design built")
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "design-revised", Input: 4})
}

// A design that names an undeclared feature, Step or Channel, leaves a Step
// unreachable or passes its size bound is refused as invalid user input whose
// message names the field and the rule, and writes no pending input; the
// corrected card waits with its design as the pending one.
func TestSessionMockDesignRefusedDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "design-refused", 1).Content)

	agent := session.requirementsCard(ctx, 0)
	_, messages := session.history(ctx)
	for index := range 5 {
		if refused := toolResult(t, messages, conversation.ToolConfirmRequirements, index); refused.Outcome != conversation.ToolOutcomeKnownFailure || refused.ResultCode != toolfailure.InvalidUserInput.String() {
			t.Fatalf("design %d was not refused as invalid user input: %+v", index+1, refused)
		}
	}
	card := *agent.Description.PendingUserInput
	design := session.flowDesign(ctx, flowdesign.SlotPending, card.Requirements.Design)
	if len(design.Flows) != 1 || len(design.Flows[0].Steps) != 6 || awaitWait(t, design).Kind != flowdesign.WaitKindAction {
		t.Fatalf("the pending design is not the corrected one: %+v", design)
	}
	if card.Requirements.Design.Outline != approvalOutline {
		t.Fatalf("the corrected outline is %q, want %q", card.Requirements.Design.Outline, approvalOutline)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "design-refused", Input: 1})
}

// summary-approval confirms a card whose design is the approval it then
// builds, and pushes it through the commit gate without blocked: that push
// completes the build, so the confirmed design reads as built, and the final
// reply says it was built as confirmed.
func TestSessionMockSummaryApprovalDesignBuiltDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, agent := startConfirmed(t, ctx, mockInput(t, "summary-approval", 1), mockInput(t, "summary-approval", 2))
	confirmed := agent.Description.ConfirmedRequirements
	if confirmed.Spec.Design == nil || confirmed.Spec.Design.Outline != approvalOutline {
		t.Fatalf("the confirmed requirements do not carry the approval design: %+v", confirmed.Spec.Design)
	}
	if pushed := toolResult(t, agent.History.Messages, conversation.ToolCommitAndPush, 0); pushed.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the approval was not pushed through the commit gate: %+v", pushed)
	}
	if !agent.Description.ConfirmedDesignBuilt {
		t.Fatal("the pushed, unblocked commit did not mark the confirmed design built")
	}
	if head := session.summary(ctx).Identity.HeadCommit; agent.Description.ConfirmedDesignBuiltCommit != head {
		t.Fatalf("the built design names commit %q, not the pushed head %s", agent.Description.ConfirmedDesignBuiltCommit, head)
	}
	session.flowDesign(ctx, flowdesign.SlotConfirmed, confirmed.Spec.Design)
	if final := agent.History.Messages[len(agent.History.Messages)-1].Message.Content; !strings.Contains(final, "built the design as confirmed") {
		t.Fatalf("the final reply does not report the build against the design: %q", final)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "summary-approval", Input: 2})
}
