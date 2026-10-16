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
	"log/slog"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
)

// TestSessionNeverExpiresDexIntegration creates a throwaway READY Project,
// whose Session the Project starts, on the actual application Worker, and
// proves that the Session never expires: once idle, its engine execution holds
// no Flow timeout handler, and no active Step waits on a timer, so it only
// waits for the user. It calls no model and starts no workspace; the
// Project's deletion at the end of the test ends the Session.
func TestSessionNeverExpiresDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	probe := startCleanupProbe(t, ctx)
	// waiting returns the active Step types once the Session waits for the user.
	waiting := func() map[string]*dexpb.ActiveStepExecutionState {
		t.Helper()
		state, readErr := probe.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(probe.id)})
		if readErr != nil {
			t.Fatalf("bounded engine state read failed (%T)", readErr)
		}
		steps := map[string]*dexpb.ActiveStepExecutionState{}
		for _, step := range state.GetActiveStepExecutions() {
			steps[step.GetStepType()] = step
		}
		return steps
	}
	var steps map[string]*dexpb.ActiveStepExecutionState
	for steps = waiting(); steps["AwaitUser"] == nil; steps = waiting() {
		select {
		case <-ctx.Done():
			t.Fatalf("the Session never waited for the user; reconcile %s", probe.id)
		case <-time.After(200 * time.Millisecond):
		}
	}
	if len(steps) != 1 {
		t.Fatalf("an idle Session runs more than AwaitUser: %v", steps)
	}
	for stepType, step := range steps {
		if len(step.GetTimers()) != 0 || len(step.GetWaitingCondition().GetTimerConditions()) != 0 {
			t.Fatalf("idle Step %s waits on a timer", stepType)
		}
	}
	summary, err := probe.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: string(probe.id)})
	if err != nil || summary.GetFlowType() != "SessionAgentFlow" || summary.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		t.Fatalf("the admitted execution is not running (%T)", err)
	}
	if live, liveErr := probe.client.GetLiveSessionSummary(ctx, probe.id); liveErr != nil || live.Status != agentloop.SessionStatusActive {
		t.Fatalf("the idle Session does not read as a running ACTIVE Session (%T)", liveErr)
	}
	t.Logf("FlowID=%s idle with no Flow timeout handler and no timer", probe.id)
}

// TestSessionWorkspaceWorkerRestartDexIntegration replaces the actual
// application Worker of the local lane while the instance of a new Project's
// Session is provisioned. A Preview-scope demand opens the cold path, so no
// model is called. The SandboxProvisionFlow's polling Step must resume in a
// new attempt after the heartbeat timeout and report one READY outcome: the
// request has exactly one provisioning execution and one Sandbox. The
// Project's deletion at the end of the test releases it.
func TestSessionWorkspaceWorkerRestartDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+15*time.Minute)
	defer cancel()
	probe := startCleanupProbe(t, ctx)
	project := probe.identity.ProjectID
	// polling reports the active polling Step of the provisioning of
	// workspaceRequestID and whether an attempt of it failed, which is how the
	// engine records a heartbeat timeout.
	polling := func(workspaceRequestID string) (string, bool) {
		t.Helper()
		state, readErr := probe.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: provision.FlowIDForTest(project, workspaceRequestID)})
		if readErr != nil {
			return "", false
		}
		for _, step := range state.GetActiveStepExecutions() {
			switch step.GetStepType() {
			case "ProvisionInstance", "CheckoutWorkspace", "VerifyInstanceHealthy":
				return step.GetStepType(), step.GetLastFailureInfo().GetAttempt() > 0
			}
		}
		return "", false
	}
	workspaceRequestID := probe.demandWorkspace(ctx)
	probe.await(ctx, "the provisioning's polling Step", func() bool {
		step, _ := polling(workspaceRequestID)
		return step != ""
	})
	dexflowtest.RestartServerForTest(ctx, t, func(ctx context.Context) error {
		_, readErr := probe.client.GetSessionSummary(ctx, probe.id)
		return readErr
	})
	var retried bool
	probe.await(ctx, "workspace READY after the Worker replacement", func() bool {
		_, failed := polling(workspaceRequestID)
		retried = retried || failed
		ws := probe.workspace(ctx)
		if ws.State == agentloop.WorkspaceFailed {
			t.Fatalf("workspace request %s failed with %s", ws.RequestID, ws.Failure)
		}
		return ws.State == agentloop.WorkspaceReady && ws.Instance == sessionworkspace.InstanceReady
	})
	if !retried {
		t.Fatal("no polling attempt failed after the Worker replacement; the heartbeat timeout was not observed")
	}
	ready := probe.workspace(ctx)
	provisioned, err := probe.provisions.Get(ctx, project, workspaceRequestID)
	if err != nil || provisioned.Outcome == nil || provisioned.Outcome.Status != provision.OutcomeReady || provisioned.Outcome.Reported != provision.ReportAdmittedForTest ||
		provisioned.Outcome.Resources.Instance.UID != ready.Resources.Instance.UID {
		t.Fatalf("the READY request is not the one provisioned instance (%T)", err)
	}
	// The workspace Flow waited across the replacement and started the
	// provisioning once.
	if state, readErr := probe.workspaces.Get(ctx, project); readErr != nil || state.Outcome == nil || state.Outcome.Kind != sessionworkspace.OutcomeReady ||
		state.Outcome.Reported != sessionworkspace.ReportAdmittedForTest || state.Outcome.WorkspaceRequestID != workspaceRequestID {
		t.Fatalf("the workspace Flow did not report the READY outcome of %s to the Session (%T)", workspaceRequestID, readErr)
	}
	if steps := completedSteps(t, ctx, probe.reader, sessionworkspace.FlowIDForTest(project)); count(steps, stepType(sessionworkspace.ProvisionInstance{})) != 1 {
		t.Fatalf("the workspace Flow started the provisioning of %s more than once: %v", workspaceRequestID, steps)
	}
	// The FlowID of a provisioning is never reused, so one completed execution
	// provisioned the request.
	execution, err := probe.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: provision.FlowIDForTest(project, workspaceRequestID)})
	if err != nil || execution.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_COMPLETED {
		t.Fatalf("the provisioning of the request did not complete (%T)", err)
	}
	snapshot, err := probe.client.GetSnapshot(ctx, probe.id)
	if err != nil || snapshot.Agent == nil || snapshot.Agent.Description.ContextModel != "" {
		t.Fatalf("the workspace demand opened a context segment or the snapshot is unreadable (%T)", err)
	}
	t.Logf("FlowID=%s request=%s READY after a heartbeat-timeout retry of its only provisioning", probe.id, workspaceRequestID)
}

// TestStoppedSandboxProvisionDexIntegration demands the workspace of a new
// Project's Session through the Preview-scope RPC, so no model is called, and
// terminates the provisioning execution while it provisions, as an operator
// stop would. The stopped provisioning is never reported as running: its read
// and the Session Service's observation report it failed
// WORKSPACE_EXECUTION_STOPPED with what it recorded. The ACTIVE Session
// settles its request at its own bounded wait; the Project's deletion
// releases what the stopped provisioning created, which it finds by name, and
// ends DELETED.
func TestStoppedSandboxProvisionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+projectDeletionTimeout)
	defer cancel()
	probe := startCleanupProbe(t, ctx)
	project := probe.identity.ProjectID
	service := &SessionFlowService{Projects: probe.projects, Provisions: probe.provisions, Workspaces: probe.workspaces, Agent: probe.client, Logger: slog.New(slog.DiscardHandler)}
	requestID := probe.demandWorkspace(ctx)
	probe.await(ctx, "a running provisioning", func() bool {
		state, readErr := probe.provisions.Get(ctx, project, requestID)
		return readErr == nil && state.Outcome == nil
	})
	provisionFlowID := provision.FlowIDForTest(project, requestID)
	if err := probe.sdk.StopFlow(ctx, provisionFlowID, dex.StopOptions{Type: dex.TerminateFlow, Reason: "stopped-provisioning detection test"}); err != nil {
		t.Fatalf("terminate failed (%T)", err)
	}
	stopped, err := probe.provisions.Get(ctx, project, requestID)
	if err != nil || stopped.Outcome == nil || stopped.Outcome.Status != provision.OutcomeFailed || stopped.Outcome.Failure.Code != provision.FailureExecutionStoppedForTest.String() {
		t.Fatalf("the terminated provisioning %s does not read as stopped (%T)", provisionFlowID, err)
	}
	observed, err := service.ObserveProvisioning(ctx, project, requestID)
	if err != nil || observed == nil || observed.Failure == nil || observed.Failure.Code != provision.FailureExecutionStoppedForTest.String() {
		t.Fatalf("the Session Service observed the terminated provisioning as running (%T)", err)
	}
	if ws := probe.workspace(ctx); ws.State != agentloop.WorkspaceRequested || ws.Instance != sessionworkspace.InstanceProvisioning {
		t.Fatalf("the request settled before the bounded waits: %s %s", ws.State, ws.Instance)
	}
	probe.requestDeletion(ctx, "delete-stopped-provisioning")
	probe.awaitDeleted(ctx)
	probe.requireReleased(ctx, requestID)
	probe.requireCompleted(ctx)
	t.Logf("FlowID=%s request %s: terminated provisioning reads failed %s; the deletion released what it created", probe.id, requestID, provision.FailureExecutionStoppedForTest)
}
