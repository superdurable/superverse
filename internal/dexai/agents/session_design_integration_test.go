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

	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/flowdesign"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// The outline of the approval design that design-revised records and
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

// A card with a Flow design records version 1 with its body as the current
// design, which GetFlowDesign returns at the reference's digest, while another
// digest is stale; the turn goes on to a durable timer. A change sent and
// steered as Studio's change box does interrupts it and revises the design:
// version 2 replaces it in the commit that records the card, so the first
// digest is stale too, and no design is built yet. A compaction in that turn
// keeps the outline of version 2. The next push traces version 2 in the same
// Session: a trace of version 1 is refused, and the blocked checkpoint that
// names version 2 pushes but leaves the design unbuilt.
func TestSessionMockDesignRevisedDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "design-revised", 1).Content)

	agent := session.waitForSnapshot(ctx, "version 1 while the Agent waits on its timer", func(agent agentloop.AgentSnapshot) bool {
		stored := agent.Description.ConfirmedRequirements
		return stored != nil && stored.Revision == 1 && agent.Description.PendingTimer != nil && agent.Description.Status == agentloop.AgentStatusWaitingForTimer
	})
	requireNoFailure(t, agent.History)
	first := agent.Description.ConfirmedRequirements.Spec
	review := session.flowDesign(ctx, flowdesign.SlotConfirmed, first.Design)
	if wait := awaitWait(t, review); wait.Kind != flowdesign.WaitKindChannel || wait.Name != "SummaryReviews" {
		t.Fatalf("the first design does not wait on the review Channel: %+v", wait)
	}
	if outline := first.Design.Outline; !strings.Contains(outline, "AwaitSummaryApproval [SummaryReviews]") || !strings.HasSuffix(outline, "; +1 failure-handling Step") {
		t.Fatalf("the first outline does not list the main story with one failure-handling Step: %q", outline)
	}
	session.requireDesignReadError(ctx, flowdesign.SlotConfirmed, "sha256:stale", sessionagent.ErrFlowDesignRevisionChanged)
	if agent.Description.ConfirmedDesignBuilt {
		t.Fatal("a recorded design reads as built before any push")
	}

	// The steered change records version 2 with the approval design, and the
	// turn's compaction keeps its outline: the last reply answers only when the
	// new segment carries it.
	before := agent.Description.LastSequence
	session.steer(ctx, mockInput(t, "design-revised", 2).Content)
	agent = session.recordedRequirements(ctx, before, 2)
	revised := agent.Description.ConfirmedRequirements.Spec
	if revised.Design == nil || revised.Design.Digest == first.Design.Digest {
		t.Fatalf("version 2 does not carry a new design: %+v", revised.Design)
	}
	session.requireDesignReadError(ctx, flowdesign.SlotConfirmed, first.Design.Digest, sessionagent.ErrFlowDesignRevisionChanged)
	approval := session.flowDesign(ctx, flowdesign.SlotConfirmed, revised.Design)
	if wait := awaitWait(t, approval); wait.Kind != flowdesign.WaitKindAction || wait.Name != "ApproveSummary" {
		t.Fatalf("the revised design does not wait for the Approve summary Action: %+v", wait)
	}
	if revised.Design.Outline != approvalOutline {
		t.Fatalf("the revised outline is %q, want %q", revised.Design.Outline, approvalOutline)
	}
	if agent.Description.ConfirmedDesignBuilt {
		t.Fatal("a recorded design reads as built before any push")
	}
	if final := agent.History.Messages[len(agent.History.Messages)-1].Message.Content; !strings.Contains(final, "survived the compaction") {
		t.Fatalf("the segment after the compaction does not carry the recorded design's outline: %q", final)
	}

	// The checkpoint's trace of version 1 is refused; the trace of version 2
	// pushes, but the build is not complete.
	before = agent.Description.LastSequence
	session.send(ctx, mockInput(t, "design-revised", 3).Content)
	agent = session.waitForSnapshot(ctx, "the turn that pushes a checkpoint", func(agent agentloop.AgentSnapshot) bool {
		return endsWithReplyAfter(agent, before) && agent.Description.Status == agentloop.AgentStatusWaitingForMessage
	})
	requireNoFailure(t, agent.History)
	_, messages := session.history(ctx)
	after := slices.DeleteFunc(slices.Clone(messages), func(message conversation.SequencedMessage) bool { return message.Sequence <= before })
	if stale := toolResult(t, after, conversation.ToolCommitAndPush, 0); stale.Outcome != conversation.ToolOutcomeKnownFailure || stale.ResultCode != toolfailure.InvalidArguments.String() {
		t.Fatalf("the trace of version 1 was not refused: %+v", stale)
	}
	if pushed := toolResult(t, after, conversation.ToolCommitAndPush, 1); pushed.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("the blocked checkpoint that traces version 2 was not pushed: %+v", pushed)
	}
	if agent.Description.ConfirmedDesignBuilt || agent.Description.ConfirmedRequirements.Revision != 2 {
		t.Fatalf("a blocked checkpoint marked the design built or changed the version: %+v", agent.Description.ConfirmedRequirements)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "design-revised", Input: 3})
}

// A design that names an undeclared feature, Step or Channel, leaves a Step
// unreachable or passes its size bound is refused as invalid user input whose
// message names the field and the rule, and records nothing; the corrected
// card is version 1, with its design as the current one.
func TestSessionMockDesignRefusedDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "design-refused", 1).Content)

	agent := session.recordedRequirements(ctx, 0, 1)
	_, messages := session.history(ctx)
	for index := range 5 {
		if refused := toolResult(t, messages, conversation.ToolConfirmRequirements, index); refused.Outcome != conversation.ToolOutcomeKnownFailure || refused.ResultCode != toolfailure.InvalidUserInput.String() {
			t.Fatalf("design %d was not refused as invalid user input: %+v", index+1, refused)
		}
	}
	card := agent.Description.ConfirmedRequirements.Spec
	design := session.flowDesign(ctx, flowdesign.SlotConfirmed, card.Design)
	if len(design.Flows) != 1 || len(design.Flows[0].Steps) != 6 || awaitWait(t, design).Kind != flowdesign.WaitKindAction {
		t.Fatalf("the current design is not the corrected one: %+v", design)
	}
	if card.Design.Outline != approvalOutline {
		t.Fatalf("the corrected outline is %q, want %q", card.Design.Outline, approvalOutline)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "design-refused", Input: 1})
}

// summary-approval records a card whose design is the approval it then
// builds in the same turn, and pushes it through the commit gate without
// blocked: that push completes the build, so the current design reads as
// built, and the final reply says it was built as recorded.
func TestSessionMockSummaryApprovalDesignBuiltDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	session, agent := startRecorded(t, ctx, mockInput(t, "summary-approval", 1))
	confirmed := agent.Description.ConfirmedRequirements
	if confirmed.Spec.Design == nil || confirmed.Spec.Design.Outline != approvalOutline {
		t.Fatalf("the current requirements do not carry the approval design: %+v", confirmed.Spec.Design)
	}
	if pushed := toolResult(t, agent.History.Messages, conversation.ToolCommitAndPush, 0); pushed.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the approval was not pushed through the commit gate: %+v", pushed)
	}
	if !agent.Description.ConfirmedDesignBuilt {
		t.Fatal("the pushed, unblocked commit did not mark the current design built")
	}
	if head := session.summary(ctx).Identity.HeadCommit; agent.Description.ConfirmedDesignBuiltCommit != head {
		t.Fatalf("the built design names commit %q, not the pushed head %s", agent.Description.ConfirmedDesignBuiltCommit, head)
	}
	session.flowDesign(ctx, flowdesign.SlotConfirmed, confirmed.Spec.Design)
	if final := agent.History.Messages[len(agent.History.Messages)-1].Message.Content; !strings.Contains(final, "built the design as recorded") {
		t.Fatalf("the final reply does not report the build against the design: %q", final)
	}
	requireProgress(t, agent, conversation.MockProgress{Scenario: "summary-approval", Input: 1})
}
