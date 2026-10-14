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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	sessionagent "github.com/superdurable/superversev2/internal/agents/session"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/projectdao"
	"github.com/superdurable/superversev2/internal/projects/projecttest"
)

// TestProjectSessionIdentityDexIntegration creates a throwaway Project and
// reads the Session that ProjectFlow started when the Project became READY:
// exactly session-<projectID>, started with the Project's fingerprint as its
// Start RequestID, registered as the Project's Session child and running for
// the Project's HEAD, with no start failure recorded. A repeated setup commit,
// as a retried Setup makes, starts nothing. A repeated start with the same
// RequestID and other server defaults (another HEAD and model), as a retried
// StartProjectSession makes, joins the running execution without a new run or
// a changed Session, and a start with another RequestID is refused.
func TestProjectSessionIdentityDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	probe := startCleanupProbe(t, ctx)
	project := probe.identity.ProjectID
	execution := func() *dexpb.GetFlowSummaryResponse {
		t.Helper()
		summary, err := probe.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: string(probe.id)})
		if err != nil {
			t.Fatalf("engine summary read of %s failed (%T)", probe.id, err)
		}
		return summary
	}
	started := execution()
	if probe.id != agentloop.FlowID("session-"+project) || started.GetFlowType() != dex.GetFinalFlowType(probe.flow) ||
		started.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING || started.GetRequestId() != agentloop.SessionStartIdentity(project) {
		t.Fatalf("the Session %s is not the running SessionAgentFlow started with the Project fingerprint", probe.id)
	}
	before := probe.summary(ctx)
	if before.Identity != probe.identity || before.Status != agentloop.SessionStatusActive {
		t.Fatalf("the Session runs for %+v as %s, not for the Project's HEAD", before.Identity, before.Status)
	}
	child, err := probe.projects.ReadChildForTest(ctx, project, string(probe.id))
	if err != nil || child != (projects.ChildFlowReference{FlowID: string(probe.id), FlowType: projects.ChildSession.FlowType()}) {
		t.Fatalf("the Session is not the Project's registered Session child: %+v (%T)", child, err)
	}
	ready, err := probe.projects.Read(ctx, project)
	if err != nil || ready.ProjectStatus != projects.StatusReady || ready.LastFailure != nil {
		t.Fatalf("the Project is not READY without a failure after its Session started (%T)", err)
	}

	// A retried Setup commits again: the READY Project starts nothing.
	commit := projects.SetupCommit{SetupFlowID: ready.SetupFlowID, Repository: ready.RepositoryBinding, AppManifest: ready.AppManifest}
	if err = probe.projects.CommitProjectSetup(ctx, project, commit); err != nil {
		t.Fatalf("the repeated setup commit was refused (%T)", err)
	}
	startStep := dex.GetFinalStepType(projects.StartProjectSession{})
	if starts := count(completedSteps(t, ctx, probe.reader, flowid.ProjectFlowID(project)), startStep); starts != 1 {
		t.Fatalf("the Project ran %s %d times", startStep, starts)
	}
	state, err := probe.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: flowid.ProjectFlowID(project)})
	if err != nil {
		t.Fatalf("bounded engine state read failed (%T)", err)
	}
	for _, step := range state.GetActiveStepExecutions() {
		if step.GetStepType() == startStep {
			t.Fatal("the repeated setup commit scheduled another Session start")
		}
	}

	// A retried start joins: other server defaults change nothing.
	retry := probe.client.SessionRequest(agentloop.SessionIdentity{ProjectID: project, Profile: agentloop.ProfileSessionV1, HeadCommit: strings.Repeat("b", 40)})
	retry.Model = conversation.ModelOpus55
	if err = probe.client.StartAccepted(ctx, retry); err != nil {
		t.Fatalf("the repeated start did not join (%T)", err)
	}
	if joined := execution(); joined.GetFirstRunId() != started.GetFirstRunId() || joined.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		t.Fatal("the repeated start began another run")
	}
	if after := probe.summary(ctx); !reflect.DeepEqual(after, before) {
		t.Fatalf("the repeated start changed the Session: %+v", after.Identity)
	}
	other := flowid.Fingerprint(struct{ ProjectID, RequestID string }{project, "another"})
	_, err = probe.sdk.StartFlow(ctx, probe.flow, string(probe.id), nil, dex.StartFlowOptions{RequestID: &other, IDReusePolicy: dex.IDReuseDisallow})
	var already *dex.FlowAlreadyStartedError
	if !errors.As(err, &already) {
		t.Fatalf("a start with another RequestID was not refused (%s)", dexflowtest.ErrorCategory(err))
	}
	t.Logf("FlowID=%s RequestID=%s registered as the Project's Session; a repeated commit and start joined, another RequestID was refused", probe.id, started.GetRequestId())
}

// TestProjectDeletionBeforeReadyDexIntegration deletes a Project while its
// Setup runs: the Project never becomes READY, so no Session ever starts.
func TestProjectDeletionBeforeReadyDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), projectDeletionTimeout)
	defer cancel()
	sdk, client, _ := projectClients(t)
	projectID := projecttest.StartForTest(ctx, t, client, t.Name())
	deleted := projecttest.DeleteForTest(ctx, t, client, projectID)
	if deleted.ProjectStatus != projects.StatusDeleted || deleted.AppManifest != nil {
		t.Fatalf("the Project became READY before its deletion; the scenario was not exercised (%s)", deleted.ProjectStatus)
	}
	dexflowtest.RequireAbsentForTest(ctx, t, sdk, flowid.SessionFlowID(projectID))
	t.Logf("Project %s deleted before READY; no Session exists", projectID)
}

// TestProjectDeletionRightAfterReadyDexIntegration deletes a Project as soon
// as it is READY, racing the Session's start: the Session either never
// started, because the Project was no longer READY when its start Step ran,
// or started and was denied at admission, or was admitted and completed by the
// deletion's cleanup. It never runs on after the Project's deletion.
func TestProjectDeletionRightAfterReadyDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+projectDeletionTimeout)
	defer cancel()
	sdk, client, flow := projectClients(t)
	projectID := projecttest.StartForTest(ctx, t, client, t.Name())
	for {
		project, err := client.Read(ctx, projectID)
		if err == nil && project.ProjectStatus != projects.StatusCreating {
			if project.ProjectStatus != projects.StatusReady {
				t.Fatalf("Project %s reached %s instead of READY", projectID, project.ProjectStatus)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Project %s did not become READY", projectID)
		case <-time.After(50 * time.Millisecond):
		}
	}
	projecttest.DeleteForTest(ctx, t, client, projectID)
	sessions := sessionagent.NewSessionAgentFlowClientForTest(sdk, flow, dexflowtest.ExecutionReaderForTest(t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")), "")
	_, err := sdk.WaitForFlow(ctx, flowid.SessionFlowID(projectID), dex.WaitForFlowOptions{})
	var absent *dex.FlowNotFoundError
	if errors.As(err, &absent) {
		t.Logf("Project %s deleted right after READY before its Session started", projectID)
		return
	}
	completed, err := sessions.ProjectSessionCompleted(ctx, projectID)
	if err != nil || !completed {
		t.Fatalf("the Session of the deleted Project %s did not complete with a cleanup receipt (%s)", projectID, dexflowtest.ErrorCategory(err))
	}
	summary, err := sessions.GetSessionSummary(ctx, agentloop.SessionFlowID(projectID))
	if err != nil || summary.Status != agentloop.SessionStatusArchived {
		t.Fatalf("the completed Session is not ARCHIVED (%T)", err)
	}
	t.Logf("Project %s deleted right after READY; its Session ended %s", projectID, summary.CloseReason)
}

// TestProjectSessionStartFailureDexIntegration occupies session-<projectID>
// with an execution of another Start RequestID before the Project exists, as a
// foreign or corrupted start would, and lets the Project become READY. The
// Project's StartProjectSession is refused for its own RequestID until its
// retries run out, and RecordProjectSessionStartFailure records
// SESSION_START_FAILED in the Project's SessionStartFailure: the Project stays
// READY without a Session child, and its LastFailure, which a command owns,
// stays empty. The occupying execution has no Session Attributes, so it ends
// before any effect; the test terminates it if it still runs.
func TestProjectSessionStartFailureDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	sdk, client, flow := projectClients(t)
	var sessionFlowID string
	projectID := projecttest.StartAfterForTest(ctx, t, client, t.Name(), func(projectID string) {
		sessionFlowID = flowid.SessionFlowID(projectID)
		foreign := flowid.Fingerprint(struct{ ProjectID, RequestID string }{projectID, "foreign"})
		if _, err := sdk.StartFlow(ctx, flow, sessionFlowID, nil, dex.StartFlowOptions{RequestID: &foreign, IDReusePolicy: dex.IDReuseDisallow}); err != nil {
			t.Fatalf("the foreign start of %s failed (%T)", sessionFlowID, err)
		}
		t.Cleanup(func() {
			stopErr := sdk.StopFlow(context.WithoutCancel(ctx), sessionFlowID, dex.StopOptions{Type: dex.TerminateFlow, Reason: "test: the foreign Session start"})
			var inactive *dex.FlowNotActiveOrNotFoundError
			if stopErr != nil && !errors.As(stopErr, &inactive) {
				t.Errorf("the foreign execution of %s did not stop (%T)", sessionFlowID, stopErr)
			}
		})
	})
	projecttest.AwaitReadyForTest(ctx, t, client, projectID)
	var project projects.Project
	for {
		var err error
		if project, err = client.Read(ctx, projectID); err == nil && project.SessionStartFailure != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Project %s recorded no Session start failure (%s)", projectID, dexflowtest.ErrorCategory(err))
		case <-time.After(2 * time.Second):
		}
	}
	if project.SessionStartFailure.Code != "SESSION_START_FAILED" || project.ProjectStatus != projects.StatusReady || project.LastFailure != nil {
		t.Fatalf("Project %s recorded %s while %s with the command failure %+v", projectID, project.SessionStartFailure.Code, project.ProjectStatus, project.LastFailure)
	}
	if child, err := client.ReadChildForTest(ctx, projectID, sessionFlowID); err != nil || child != (projects.ChildFlowReference{}) {
		t.Fatalf("the refused start registered a Session child %+v (%T)", child, err)
	}
	t.Logf("Project %s stayed READY with %s apart from its command failure after StartProjectSession was refused", projectID, project.SessionStartFailure.Code)
}

// projectClients returns a Dex client whose registry knows the Project and
// Session Flows, on the existing Worker, a ProjectFlow client on it and the
// Session's Flow definition.
func projectClients(t *testing.T) (*dex.Client, *projects.ProjectFlowClient, *sessionagent.SessionAgentFlow) {
	t.Helper()
	projectFlow, flow := &projects.ProjectFlow{}, &sessionagent.SessionAgentFlow{}
	sdk := dexflowtest.ClientForTest(t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), &dex.WorkerTarget{Address: dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")},
		projectFlow, flow)
	return sdk, &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(postgrestest.OpenForTest(t)), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmail: "admin@superverse.test"}, flow
}
