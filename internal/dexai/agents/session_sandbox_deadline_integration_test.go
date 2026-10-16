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
	"strconv"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
)

// These scenarios run on the Session of a throwaway READY Project and the
// actual Worker, deployed with the mock model and a short sandbox.hold that
// SUPERVERSE_TEST_SANDBOX_HOLD_SECONDS repeats. Dex, Kind or Docker, the
// Sandbox and its tools are real; each Session starts its own workspace
// through the real cold path, and at its deadline the Session has its
// SessionWorkspaceFlow reclaim it.

// TestSandboxDeadlineReclaimDexIntegration lets a turn leave an uncommitted
// note, which moves the Sandbox deadline to about one hold after the tool call.
// Idle past the deadline, the Session revokes its lease and sends one reclaim
// command, and its SessionWorkspaceFlow reclaims the instance and reports it
// reclaimed: the instance is gone while its boundary and volume remain, and
// the Session stays ACTIVE with its workspace REVOKED. Only these two
// cross-Flow calls take place: the workspace Flow runs exactly the command's
// Steps, the Session ran no Preview, so no PreviewFlow exists, and the
// completed provisioning records no event after the turn's tool calls
// extended the deadline. The next turn provisions a new instance on the same
// volume and finds the note unchanged.
func TestSandboxDeadlineReclaimDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout+hold+sandboxReclaimTimeout)
	defer cancel()
	note := "deadline-" + time.Now().UTC().Format("20060102150405")
	session := newMockSession(t, ctx, mockInput(t, "workspace-note", 1).Content+" "+mockmodel.ArgumentMarkerForTest("note", note))
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	if result := lastToolResult(t, agent.History, conversation.ToolWrite); result.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("the note was not written: %+v", result)
	}
	ready := session.workspace(ctx)
	if ready.State != agentloop.WorkspaceReady || ready.Instance != sessionworkspace.InstanceReady || ready.SandboxDeadline.IsZero() || time.Until(ready.SandboxDeadline) > hold {
		t.Fatalf("the READY workspace %s has no Sandbox deadline within one hold: %s", ready.RequestID, ready.SandboxDeadline)
	}
	first := ready.Resources
	provisioned, err := session.provisions.Get(ctx, session.identity.ProjectID, ready.InstanceID)
	if err != nil || provisioned.Outcome == nil || provisioned.Outcome.Status != provision.OutcomeReady || first.Volume.UID == "" || provisioned.Outcome.Resources.Instance.UID != first.Instance.UID {
		t.Fatalf("the READY request %s is not the provisioned instance (%T)", ready.InstanceID, err)
	}
	provisionFlowID := provision.FlowIDForTest(session.identity.ProjectID, ready.InstanceID)
	provisionEvents := session.historyEvents(ctx, provisionFlowID)
	workspaceMark := markHistory(t, ctx, session.reader, sessionworkspace.FlowIDForTest(session.identity.ProjectID))
	sessionMark := markHistory(t, ctx, session.reader, string(session.id))
	closed := session.awaitReclaim(ctx, ready)
	session.await(ctx, "the reclaimed report", func() bool {
		return slices.Contains(completedStepsSince(t, ctx, session.reader, workspaceMark), stepType(sessionworkspace.ReportToSession{}))
	})
	wantSteps := []string{stepType(sessionworkspace.AwaitWorkspaceCommand{}), stepType(sessionworkspace.ReclaimInstance{}), stepType(sessionworkspace.ReportToSession{})}
	if steps := completedStepsSince(t, ctx, session.reader, workspaceMark); !slices.Equal(steps, wantSteps) {
		t.Fatalf("the workspace Flow ran %v for the reclamation, not exactly %v", steps, wantSteps)
	}
	if steps := completedStepsSince(t, ctx, session.reader, sessionMark); count(steps, stepType(sessionagent.SendWorkspaceCommand{})) != 1 {
		t.Fatalf("the Session sent %d workspace commands at its deadline, not one: %v", count(steps, stepType(sessionagent.SendWorkspaceCommand{})), steps)
	}
	if state, readErr := session.workspaces.Get(ctx, session.identity.ProjectID); readErr != nil || state.Outcome == nil ||
		state.Outcome.Kind != sessionworkspace.OutcomeReclaimed || state.Outcome.Reported != sessionworkspace.ReportAdmittedForTest {
		t.Fatalf("the Session did not admit the reclaimed report of %s (%T)", ready.InstanceID, readErr)
	}
	if closed.Resources.Instance.UID != "" || closed.Resources.Boundary.UID != first.Boundary.UID || closed.Resources.Volume.UID != first.Volume.UID {
		t.Fatalf("the reclamation of %s did not keep exactly the boundary and the volume: %+v", ready.InstanceID, closed.Resources)
	}
	if closed.VolumeState != sandbox.WorkspaceDirty {
		t.Fatalf("the reclaimed workspace with an uncommitted note reads %s", closed.VolumeState)
	}
	session.requireNoPreview(ctx)
	if events := session.historyEvents(ctx, provisionFlowID); events != provisionEvents {
		t.Fatalf("the provisioning %s recorded %d events after it completed; the deadline and its reclamation must not reach it", provisionFlowID, events-provisionEvents)
	}
	if live, liveErr := session.client.GetLiveSessionSummary(ctx, session.id); liveErr != nil || live.Status != agentloop.SessionStatusActive {
		t.Fatalf("the Session did not stay a running ACTIVE Session after the reclamation (%T)", liveErr)
	}

	// The next use cold-starts a new instance on the same volume.
	check := conversation.PendingUserMessage{MessageID: conversation.MessageID("note-check-" + note),
		Value: conversation.UserMessage{Content: mockInput(t, "workspace-note-check", 1).Content + " " + mockmodel.ArgumentMarkerForTest("note", note)}}
	if err = session.client.SendPendingMessage(ctx, session.id, check); err != nil {
		t.Fatalf("the next message was not admitted (%T)", err)
	}
	agent = session.finishedTurnAfter(ctx, agent.Description.LastSequence)
	requireNoFailure(t, agent.History)
	if result := lastToolResult(t, agent.History, conversation.ToolBash); result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("the note is not on the volume of the next instance: %+v", result)
	}
	second := session.workspace(ctx)
	if second.InstanceID == ready.InstanceID || second.Instance != sessionworkspace.InstanceReady || second.Resources.Volume.UID != first.Volume.UID {
		t.Fatalf("the next turn did not run on a new instance of the same volume: %s %s", second.InstanceID, second.Instance)
	}
	t.Logf("FlowID=%s %s reclaimed at its deadline by the Session alone with volume %s kept; %s found the note on it", session.id, ready.InstanceID, first.Volume.UID, second.InstanceID)
}

// TestSandboxDeadlineWorkerRestartDexIntegration replaces the actual
// application Worker of the local lane while AwaitSandboxDeadline waits. The timer
// is the engine's, so the restart keeps the deadline, and the Session reclaims
// the instance once when the deadline passes.
func TestSandboxDeadlineWorkerRestartDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+hold+sandboxReclaimTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	// The deadline a turn's dispatches left; a timer armed for an earlier one
	// fires and waits again for it.
	ready := session.workspace(ctx)
	var firing int64
	session.await(ctx, "the timer of the current Sandbox deadline", func() bool {
		firing = session.deadlineTimer(ctx)
		return firing != 0 && firing >= ready.SandboxDeadline.Unix()
	})
	if firing > ready.SandboxDeadline.Unix()+2 {
		t.Fatalf("the deadline timer fires at %d, after the deadline %s", firing, ready.SandboxDeadline)
	}
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
		_, readErr := session.client.GetSessionSummary(ctx, session.id)
		return readErr
	})
	if !time.Now().Before(ready.SandboxDeadline) {
		t.Fatal("the Worker was replaced after the deadline; deploy with a longer sandbox.hold")
	}
	if kept := session.deadlineTimer(ctx); kept != firing {
		t.Fatalf("the Worker replacement moved the deadline timer from %d to %d", firing, kept)
	}
	closed := session.awaitReclaim(ctx, ready)
	if closed.Resources.Instance.UID != "" || closed.Resources.Volume.UID == "" {
		t.Fatalf("the reclamation of %s did not keep the volume: %+v", ready.InstanceID, closed.Resources)
	}
	if session.deadlineTimer(ctx) != 0 {
		t.Fatal("a Sandbox deadline still waits after the reclamation")
	}
	t.Logf("FlowID=%s deadline timer %d kept across the Worker replacement; %s reclaimed once", session.id, firing, ready.InstanceID)
}

// TestPreviewLendHoldsSandboxDexIntegration lends the Session's READY
// instance to a Preview command more than a minute after the turn that last
// moved its Sandbox deadline. The command runs on the instance, which holds
// the Sandbox for another hold as the main agent's workspace use does: the
// deadline moves to one hold after the lend, so a deadline that a turn left
// does not reclaim the instance under the command.
func TestPreviewLendHoldsSandboxDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	if hold < 2*time.Minute {
		t.Fatal("the lend comes a minute after the turn and before its deadline: SUPERVERSE_TEST_SANDBOX_HOLD_SECONDS must be at least 120")
	}
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+previewCommandTimeout+2*time.Minute)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	ready := session.workspace(ctx)
	// A deadline moves only by at least a minute.
	select {
	case <-ctx.Done():
		t.Fatalf("the wait before the lend did not end; reconcile %s", session.id)
	case <-time.After(time.Minute + 10*time.Second):
	}
	lent := time.Now()
	if !lent.Before(ready.SandboxDeadline) {
		t.Fatalf("the deadline %s of %s passed before the lend", ready.SandboxDeadline, ready.InstanceID)
	}
	prepare := preview.Command{ID: "prepare-lend-" + lent.UTC().Format("20060102150405"), Kind: preview.CommandPreparePreviewSource}
	if err := session.service().PreparePreviewSource(ctx, session.account, session.identity.ProjectID, prepare); err != nil {
		t.Fatalf("the source preparation was not admitted (%T)", err)
	}
	prepared := session.awaitPreview(ctx, prepare.ID)
	held := session.workspace(ctx)
	switch {
	case prepared.WorkspaceRequestID != ready.InstanceID || held.InstanceID != ready.InstanceID || held.Instance != sessionworkspace.InstanceReady:
		t.Fatalf("the command did not run on the lent %s: %q, instance %s %s", ready.InstanceID, prepared.WorkspaceRequestID, held.InstanceID, held.Instance)
	case held.SandboxDeadline.Before(lent.Add(hold - 2*time.Second)):
		t.Fatalf("the lend did not hold %s for another hold: the deadline %s precedes %s", ready.InstanceID, held.SandboxDeadline, lent.Add(hold))
	}
	t.Logf("FlowID=%s lent %s to %s, which moved its deadline from %s to %s", session.id, ready.InstanceID, prepare.ID, ready.SandboxDeadline, held.SandboxDeadline)
}

// TestSandboxDeadlineRenewDexIntegration keeps a Session's workspace running
// through the Session Service, as Studio's "Keep running for 7 days" does.
// Before the Session has a workspace the renewal answers false and changes
// nothing. On the READY instance it answers true and moves the Sandbox
// deadline to seven days from now, and a repeated renewal answers true without
// moving it. The deadline timer, armed for the shortened hold, fires at the
// earlier deadline and waits again for the renewed one instead of reclaiming.
func TestSandboxDeadlineRenewDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+hold+sandboxReclaimTimeout)
	defer cancel()
	session := newSession(t, ctx, conversation.ModelMock, "")
	service, project := session.service(), session.identity.ProjectID
	if renewed, err := service.RenewWorkspace(ctx, session.account, project); err != nil || renewed {
		t.Fatalf("the renewal of a Session without a workspace answered %t (%v)", renewed, err)
	}
	if ws := session.workspace(ctx); ws.RequestID != "" || !ws.SandboxDeadline.IsZero() {
		t.Fatalf("the renewal changed the absent workspace: %s %s", ws.RequestID, ws.SandboxDeadline)
	}
	session.send(ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	ready := session.workspace(ctx)
	var armed int64
	session.await(ctx, "the timer of the Sandbox deadline", func() bool {
		armed = session.deadlineTimer(ctx)
		return armed != 0 && armed >= ready.SandboxDeadline.Unix()
	})

	requested := time.Now()
	if renewed, err := service.RenewWorkspace(ctx, session.account, project); err != nil || !renewed {
		t.Fatalf("the renewal of the READY workspace %s answered %t (%v)", ready.InstanceID, renewed, err)
	}
	kept := session.workspace(ctx)
	if kept.InstanceID != ready.InstanceID || kept.SandboxDeadline.Before(requested.Add(agentloop.KeepRunningHold-5*time.Second)) ||
		kept.SandboxDeadline.After(time.Now().Add(agentloop.KeepRunningHold+5*time.Second)) {
		t.Fatalf("the renewal of %s moved the deadline from %s to %s, not seven days ahead", ready.InstanceID, ready.SandboxDeadline, kept.SandboxDeadline)
	}
	if renewed, err := service.RenewWorkspace(ctx, session.account, project); err != nil || !renewed {
		t.Fatalf("the repeated renewal of %s answered %t (%v)", ready.InstanceID, renewed, err)
	}
	if again := session.workspace(ctx); !again.SandboxDeadline.Equal(kept.SandboxDeadline) {
		t.Fatalf("a renewal within a minute moved the deadline from %s to %s", kept.SandboxDeadline, again.SandboxDeadline)
	}

	// At the earlier deadline the timer fires and waits for the renewed one.
	var rearmed int64
	session.await(ctx, "the deadline timer re-armed for the renewed deadline", func() bool {
		rearmed = session.deadlineTimer(ctx)
		return time.Now().After(ready.SandboxDeadline) && rearmed >= kept.SandboxDeadline.Unix()
	})
	if after := session.workspace(ctx); after.InstanceID != ready.InstanceID || after.State != agentloop.WorkspaceReady || after.Instance != sessionworkspace.InstanceReady {
		t.Fatalf("the workspace %s did not stay READY past its earlier deadline: %s %s", ready.InstanceID, after.State, after.Instance)
	}
	t.Logf("FlowID=%s: no renewal without a workspace; %s renewed to %s; the deadline timer moved from %d to %d", session.id, ready.InstanceID, kept.SandboxDeadline, armed, rearmed)
}

// deadlineTimer reads when the Session's AwaitSandboxDeadline timer fires,
// in Unix seconds, or 0 while none waits: a bounded diagnostic read of the
// engine state.
func (s *mockSession) deadlineTimer(ctx context.Context) int64 {
	s.t.Helper()
	state, err := s.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(s.id)})
	if err != nil {
		s.t.Fatalf("bounded engine state read failed (%T)", err)
	}
	for _, step := range state.GetActiveStepExecutions() {
		if conditions := step.GetWaitingCondition().GetTimerConditions(); step.GetStepType() == dex.GetFinalStepType(sessionagent.AwaitSandboxDeadline{}) && len(conditions) == 1 {
			return conditions[0].GetFiringUnixTimestampSeconds()
		}
	}
	return 0
}

// sandboxReclaimTimeout bounds a reclamation after the deadline: the Preview
// engine stop, the inspection and the instance's deletion.
const sandboxReclaimTimeout = 10 * time.Minute

// sandboxHold is the Sandbox hold of the deployed server, which these tests
// wait out, so it must be short.
func sandboxHold(t *testing.T) time.Duration {
	t.Helper()
	seconds, err := strconv.Atoi(dexflowtest.Setting(t, "SUPERVERSE_TEST_SANDBOX_HOLD_SECONDS"))
	if err != nil || seconds < 60 || seconds > 600 {
		t.Fatal("SUPERVERSE_TEST_SANDBOX_HOLD_SECONDS must repeat, in seconds, the deployed server's sandbox.hold, 60 to 600")
	}
	return time.Duration(seconds) * time.Second
}

// summary reads the retained lifecycle of the Session.
func (s *mockSession) summary(ctx context.Context) agentloop.SessionSummary {
	s.t.Helper()
	summary, err := s.client.GetSessionSummary(ctx, s.id)
	if err != nil {
		s.t.Fatalf("Session read failed (%T)", err)
	}
	return summary
}

// await polls done until it holds; past ctx the test fails.
func (s *mockSession) await(ctx context.Context, what string, done func() bool) {
	s.t.Helper()
	for !done() {
		select {
		case <-ctx.Done():
			s.t.Fatalf("%s did not converge; reconcile %s", what, s.id)
		case <-time.After(2 * time.Second):
		}
	}
}

// awaitReclaim waits until the workspace Flow reclaimed the instance of ready
// at the Session's deadline: the Session's workspace reads REVOKED without a
// deadline and the instance reclaimed, with no failure. It returns the
// workspace after it.
func (s *mockSession) awaitReclaim(ctx context.Context, ready workspaceView) workspaceView {
	s.t.Helper()
	var reclaimed workspaceView
	s.await(ctx, "the reclamation at the Sandbox deadline", func() bool {
		ws := s.workspace(ctx)
		if ws.InstanceID != ready.InstanceID {
			s.t.Fatalf("the instance %s was replaced by %s instead of reclaimed", ready.InstanceID, ws.InstanceID)
		}
		if ws.State == agentloop.WorkspaceRevoked && time.Now().Before(ready.SandboxDeadline) {
			s.t.Fatalf("the workspace %s was revoked before its deadline %s", ready.InstanceID, ready.SandboxDeadline)
		}
		reclaimed = ws
		return ws.Instance == sessionworkspace.InstanceReclaimed || ws.Instance == sessionworkspace.InstanceFailed
	})
	if reclaimed.Instance != sessionworkspace.InstanceReclaimed || reclaimed.State != agentloop.WorkspaceRevoked || !reclaimed.SandboxDeadline.IsZero() {
		s.t.Fatalf("the instance %s ended %s (%s): %+v", ready.InstanceID, reclaimed.Instance, reclaimed.State, reclaimed.InstanceFailure)
	}
	return reclaimed
}

// requireNoPreview fails unless the Project never ran a Preview command: no
// PreviewFlow exists for it.
func (s *mockSession) requireNoPreview(ctx context.Context) {
	s.t.Helper()
	_, err := s.previews.Get(ctx, s.identity.ProjectID)
	var missing *dex.FlowNotFoundError
	if !errors.As(err, &missing) {
		s.t.Fatalf("the Project's Preview exists or is unreadable (%T)", err)
	}
}

// historyEvents counts the events of the engine history of flowID, a
// diagnostic read of another Flow's activity.
func (s *mockSession) historyEvents(ctx context.Context, flowID string) int {
	s.t.Helper()
	return len(runHistory(s.t, ctx, s.reader, flowID, currentRun(s.t, ctx, s.reader, flowID)))
}

// finishedTurnAfter waits until a turn that began after sequence has ended
// with a final reply and the Session waits for the next message.
func (s *mockSession) finishedTurnAfter(ctx context.Context, sequence conversation.Sequence) agentloop.AgentSnapshot {
	s.t.Helper()
	var agent agentloop.AgentSnapshot
	s.await(ctx, "the next turn", func() bool {
		agent = s.finishedTurn(ctx)
		for _, message := range agent.History.Messages {
			if message.Sequence > sequence && message.Message.Role == conversation.MessageRoleAssistant && len(message.Message.ToolCalls) == 0 {
				return true
			}
		}
		return false
	})
	return agent
}

// lastToolResult returns the execution of the last result of tool in history.
func lastToolResult(t *testing.T, history conversation.HistoryPage, tool conversation.ToolName) conversation.ToolExecutionMetrics {
	t.Helper()
	for index := len(history.Messages) - 1; index >= 0; index-- {
		if message := history.Messages[index].Message; message.Role == conversation.MessageRoleTool && message.ToolName != nil && *message.ToolName == tool && message.ToolExecution != nil {
			return *message.ToolExecution
		}
	}
	t.Fatalf("the history has no %s result", tool)
	return conversation.ToolExecutionMetrics{}
}
