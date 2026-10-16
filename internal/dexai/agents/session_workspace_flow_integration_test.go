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
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// These scenarios run on the Session of a throwaway READY Project and the
// actual Worker of the mock Session tests, like the Sandbox deadline tests:
// the Session's SessionWorkspaceFlow provisions and reclaims its instances
// through real SandboxProvisionFlows and the lane's runtime, one command at a
// time. They read both Flows' histories with the engine reader, a diagnostic
// read of which Steps ran.

// workspaceView is the Session's workspace as the API shows it: the
// Session's request side with the instance its SessionWorkspaceFlow owns,
// which tests read as the Session summary's workspace was read before the
// instance moved.
type workspaceView struct {
	agentloop.WorkspaceSummary
	Instance          sessionworkspace.InstanceState
	InstanceID        string
	Resources         sandbox.Resources
	VolumeState       string
	InstanceUpdatedAt time.Time
	Timings           []dexflow.PhaseTiming
	InstanceFailure   *sessionworkspace.Failure
}

// readWorkspaceView reads the Session's request side and then its instance;
// a Session that never sent a command has no instance.
func readWorkspaceView(t *testing.T, ctx context.Context, client *sessionagent.SessionAgentFlowClient, workspaces *sessionworkspace.SessionWorkspaceFlowClient, id agentloop.FlowID, projectID string) workspaceView {
	t.Helper()
	summary, err := client.GetSessionSummary(ctx, id)
	if err != nil {
		t.Fatalf("Session read failed (%T)", err)
	}
	view := workspaceView{WorkspaceSummary: summary.Workspace}
	state, err := workspaces.Get(ctx, projectID)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return view
	}
	if err != nil {
		t.Fatalf("Session workspace read failed (%T)", err)
	}
	instance := state.Instance
	view.Instance, view.InstanceID, view.Resources, view.VolumeState = instance.State, instance.InstanceID, instance.Resources, instance.VolumeState
	view.InstanceUpdatedAt, view.Timings, view.InstanceFailure = instance.UpdatedAt, instance.Timings, instance.Failure
	return view
}

// workspace reads the Session's workspace with its instance.
func (s *mockSession) workspace(ctx context.Context) workspaceView {
	s.t.Helper()
	return readWorkspaceView(s.t, ctx, s.client, s.workspaces, s.id, s.identity.ProjectID)
}

// workspace reads the Session's workspace with its instance.
func (p *cleanupProbe) workspace(ctx context.Context) workspaceView {
	p.t.Helper()
	return readWorkspaceView(p.t, ctx, p.client, p.workspaces, p.id, p.identity.ProjectID)
}

// stepType is the registered name of a Step.
func stepType[IN any](step dex.Step[IN]) string { return dex.GetFinalStepType(step) }

// activeStep reports whether flowID runs a Step of stepType now, a bounded
// diagnostic read of the engine state.
func activeStep(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flowID, stepType string) bool {
	t.Helper()
	state, err := reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: flowID})
	if err != nil {
		return false
	}
	return slices.ContainsFunc(state.GetActiveStepExecutions(), func(step *dexpb.ActiveStepExecutionState) bool { return step.GetStepType() == stepType })
}

// awaitActiveReclamation waits until the workspace Flow's ReclaimInstance
// runs for the instance of ready, checking four times a second, and fails at
// once when the reclamation ended before it was observed: the scenario then
// was not exercised.
func (s *mockSession) awaitActiveReclamation(ctx context.Context, ready workspaceView) {
	s.t.Helper()
	workspaceFlowID := sessionworkspace.FlowIDForTest(s.identity.ProjectID)
	for !activeStep(s.t, ctx, s.reader, workspaceFlowID, stepType(sessionworkspace.ReclaimInstance{})) {
		if ws := s.workspace(ctx); ws.Instance == sessionworkspace.InstanceReclaimed || ws.Instance == sessionworkspace.InstanceFailed {
			s.t.Fatalf("the reclamation of %s ended %s before it was observed; the scenario was not exercised", ready.InstanceID, ws.Instance)
		}
		select {
		case <-ctx.Done():
			s.t.Fatalf("the reclamation of %s did not begin; reconcile %s", ready.InstanceID, s.id)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// TestSessionWorkspaceCommandJoinsDexIntegration cold-starts a Session's
// first instance through its SessionWorkspaceFlow, the Project's one
// execution, and repeats the Session's provision command, as a retry of its
// SendWorkspaceCommand Step does after a lost answer: the repeat is accepted
// and joins the admitted command, so the workspace Flow runs no other Step,
// starts no other provisioning and stays the one execution of its FlowID. A
// command of an older request is refused.
func TestSessionWorkspaceCommandJoinsDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	started := time.Now().UTC()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	ready := session.workspace(ctx)
	if ready.Instance != sessionworkspace.InstanceReady || ready.InstanceID != ready.RequestID {
		t.Fatalf("the cold start left the instance %s %s for the request %s", ready.InstanceID, ready.Instance, ready.RequestID)
	}
	project := session.identity.ProjectID
	workspaceFlowID := sessionworkspace.FlowIDForTest(project)
	mark, before := markHistory(t, ctx, session.reader, workspaceFlowID), session.execution(ctx, workspaceFlowID)
	repeat := sessionworkspace.NewCommand(sessionworkspace.CommandProvision, ready.RequestID, session.identity.HeadCommit, "")
	if accepted, err := session.workspaces.Submit(ctx, project, repeat); err != nil || !accepted {
		t.Fatalf("the repeated command %s was answered %t (%T)", repeat.ID, accepted, err)
	}
	older := sessionworkspace.NewCommand(sessionworkspace.CommandProvision, "ws-1", session.identity.HeadCommit, "")
	if number, _ := flowid.WorkspaceRequestNumber(ready.RequestID); number > 1 {
		if accepted, err := session.workspaces.Submit(ctx, project, older); err != nil || accepted {
			t.Fatalf("the older command %s was answered %t (%T)", older.ID, accepted, err)
		}
	}
	// A repeat that was published would have run its command by now.
	time.Sleep(5 * time.Second)
	if steps := completedStepsSince(t, ctx, session.reader, mark); len(steps) != 0 {
		t.Fatalf("the repeated command ran the workspace Flow's Steps %v", steps)
	}
	after := session.execution(ctx, workspaceFlowID)
	if after.GetFlowExecutionId().GetRunId() != before.GetFlowExecutionId().GetRunId() || after.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		t.Fatalf("the repeated command started or ended an execution of %s", workspaceFlowID)
	}
	if flowIDs := session.provisioningFlowIDs(ctx, started); !slices.Equal(flowIDs, []string{provision.FlowIDForTest(project, ready.InstanceID)}) {
		t.Fatalf("the cold start and its repeat ran the provisionings %v", flowIDs)
	}
	t.Logf("FlowID=%s cold-started %s through %s; the repeated %s joined and the older %s was refused", session.id, ready.InstanceID, workspaceFlowID, repeat.ID, older.ID)
}

// TestSandboxDeadlineSparesInstanceInUseDexIntegration runs the
// background-long-turn scenario, whose background command sleeps for seven
// minutes, past the Sandbox deadline its dispatch set. The deadline fires
// while the command runs on the instance: the Session decides under its
// Workspace lock that the instance is in use, extends the deadline and sends
// no reclaim command, so the workspace Flow never reclaims it; the command's
// exit notification then opens a turn whose read_task_output runs on the same
// instance. It needs a sandbox.hold of at most five minutes.
func TestSandboxDeadlineSparesInstanceInUseDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	if hold > 5*time.Minute {
		t.Fatal("the deadline must pass while the seven-minute command runs: SUPERVERSE_TEST_SANDBOX_HOLD_SECONDS must be at most 300")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout+7*time.Minute+hold)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "background-long-turn", 1).Content)
	var first workspaceView
	session.await(ctx, "the running background command", func() bool {
		snapshot, err := session.client.GetSnapshot(ctx, session.id)
		first = session.workspace(ctx)
		return err == nil && snapshot.Agent != nil && len(snapshot.Agent.Description.BackgroundCommands) == 1 &&
			snapshot.Agent.Description.BackgroundCommands[0].Status == agenttasks.BackgroundCommandRunning && !first.SandboxDeadline.IsZero()
	})
	mark := markHistory(t, ctx, session.reader, sessionworkspace.FlowIDForTest(session.identity.ProjectID))
	session.await(ctx, "the passed Sandbox deadline", func() bool { return time.Now().After(first.SandboxDeadline.Add(10 * time.Second)) })
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || snapshot.Agent == nil || snapshot.Agent.Description.BackgroundCommands[0].Status != agenttasks.BackgroundCommandRunning {
		t.Fatal("the background command ended before the deadline passed; deploy with a shorter sandbox.hold")
	}
	held := session.workspace(ctx)
	switch {
	case held.State != agentloop.WorkspaceReady || held.Instance != sessionworkspace.InstanceReady || held.InstanceID != first.InstanceID:
		t.Fatalf("the instance %s in use was not kept at its deadline: %s %s", first.InstanceID, held.State, held.Instance)
	case !held.SandboxDeadline.After(first.SandboxDeadline):
		t.Fatalf("the deadline %s of the instance in use did not move", held.SandboxDeadline)
	}
	if steps := completedStepsSince(t, ctx, session.reader, mark); slices.Contains(steps, stepType(sessionworkspace.ReclaimInstance{})) {
		t.Fatalf("the workspace Flow reclaimed the instance in use: %v", steps)
	}
	agent := session.awaitNotificationTurn(ctx)
	requireNoFailure(t, agent.History)
	if result := lastToolResult(t, agent.History, conversation.ToolReadTaskOutput); result.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("the read of the command's output failed: %+v", result)
	}
	if after := session.workspace(ctx); after.InstanceID != first.InstanceID {
		t.Fatalf("the notification turn ran on %s, not on the kept %s", after.InstanceID, first.InstanceID)
	}
	t.Logf("FlowID=%s kept %s in use past its deadline %s, which moved to %s", session.id, first.InstanceID, first.SandboxDeadline, held.SandboxDeadline)
}

// awaitNotificationTurn waits until the automatic turn of a task
// notification ended with its read_task_output result in the history.
func (s *mockSession) awaitNotificationTurn(ctx context.Context) agentloop.AgentSnapshot {
	s.t.Helper()
	var agent agentloop.AgentSnapshot
	s.await(ctx, "the turn of the command's exit notification", func() bool {
		agent = s.finishedTurn(ctx)
		return slices.ContainsFunc(agent.History.Messages, func(message conversation.SequencedMessage) bool {
			return message.Message.ToolName != nil && *message.Message.ToolName == conversation.ToolReadTaskOutput
		})
	})
	return agent
}

// TestSandboxReclaimThenProvisionSerializedDexIntegration sends the next
// message while the workspace Flow reclaims the Session's idle instance at its
// deadline. The Session opens a new request, whose provision command waits
// behind the reclamation: the workspace Flow completes the reclamation, which
// ends only once every Pod of the instance is gone, before it starts the new
// instance's provisioning, so the volume never carries two Pods. The next
// turn runs on the new instance of the same volume.
func TestSandboxReclaimThenProvisionSerializedDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout+hold+sandboxReclaimTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	ready := session.workspace(ctx)
	workspaceFlowID := sessionworkspace.FlowIDForTest(session.identity.ProjectID)
	mark := markHistory(t, ctx, session.reader, workspaceFlowID)
	session.awaitActiveReclamation(ctx, ready)
	next := conversation.PendingUserMessage{MessageID: conversation.MessageID("during-reclaim-" + ready.InstanceID),
		Value: conversation.UserMessage{Content: mockInput(t, "inspect-basic", 1).Content}}
	if err := session.client.SendPendingMessage(ctx, session.id, next); err != nil {
		t.Fatalf("the next message was not admitted (%T)", err)
	}
	agent = session.finishedTurnAfter(ctx, agent.Description.LastSequence)
	requireNoFailure(t, agent.History)
	replaced := session.workspace(ctx)
	if replaced.InstanceID == ready.InstanceID || replaced.Instance != sessionworkspace.InstanceReady || replaced.Resources.Volume.UID != ready.Resources.Volume.UID {
		t.Fatalf("the next turn did not run on a new instance of the same volume: %s %s", replaced.InstanceID, replaced.Instance)
	}
	steps := completedStepsSince(t, ctx, session.reader, mark)
	reclaimed, provisioned := slices.Index(steps, stepType(sessionworkspace.ReclaimInstance{})), slices.Index(steps, stepType(sessionworkspace.ProvisionInstance{}))
	if reclaimed < 0 || provisioned < reclaimed || count(steps, stepType(sessionworkspace.ReclaimInstance{})) != 1 || count(steps, stepType(sessionworkspace.ProvisionInstance{})) != 1 {
		t.Fatalf("the workspace Flow did not finish one reclamation of %s before one provisioning of %s: %v", ready.InstanceID, replaced.InstanceID, steps)
	}
	t.Logf("FlowID=%s reclaimed %s, then provisioned %s on volume %s, in order: %v", session.id, ready.InstanceID, replaced.InstanceID, ready.Resources.Volume.UID, steps)
}

// TestSandboxReclaimWorkerRestartDexIntegration replaces the actual
// application Worker of the local lane while the workspace Flow's
// ReclaimInstance polls the reclamation of the Session's idle instance at
// its deadline. The polling Step resumes in a new attempt, whose stages each
// query before they change anything: the instance ends reclaimed once, with
// its volume kept and no failure.
func TestSandboxReclaimWorkerRestartDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+hold+sandboxReclaimTimeout+5*time.Minute)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	ready := session.workspace(ctx)
	workspaceFlowID := sessionworkspace.FlowIDForTest(session.identity.ProjectID)
	mark := markHistory(t, ctx, session.reader, workspaceFlowID)
	session.awaitActiveReclamation(ctx, ready)
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
		_, readErr := session.workspaces.Get(ctx, session.identity.ProjectID)
		return readErr
	})
	closed := session.awaitReclaim(ctx, ready)
	if closed.Resources.Instance.UID != "" || closed.Resources.Volume.UID != ready.Resources.Volume.UID {
		t.Fatalf("the reclamation of %s did not keep exactly the volume: %+v", ready.InstanceID, closed.Resources)
	}
	steps := completedStepsSince(t, ctx, session.reader, mark)
	if count(steps, stepType(sessionworkspace.ReclaimInstance{})) != 1 || slices.Contains(steps, stepType(sessionworkspace.RecordReclaimFailure{})) {
		t.Fatalf("the reclamation did not complete once without a failure across the Worker replacement: %v", steps)
	}
	t.Logf("FlowID=%s reclaimed %s once across a Worker replacement: %v", session.id, ready.InstanceID, steps)
}
