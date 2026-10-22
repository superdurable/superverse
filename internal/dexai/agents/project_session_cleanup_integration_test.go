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
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/subagent"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
)

// These scenarios create a throwaway READY Project each (projecttest) and
// delete it with the actual Worker of the local lane that
// SUPERVERSE_TEST_LANE names (SUPERVERSE_TEST_DEX_ADDRESS and
// SUPERVERSE_TEST_EXISTING_WORKER_TARGET). The deletion is the only close of
// the Project's one Session. Each demands the Session's workspace through the
// Preview-scope RPC, so no model is called, and reads what remains of the
// Session in the lane's runtime with the operator's tools (sandboxObjects).

// projectDeletionTimeout bounds a Project deletion with its Session.
const projectDeletionTimeout = 15 * time.Minute

// TestProjectDeletionClosesSessionDexIntegration deletes a Project whose
// Session holds a READY instance and whose PreviewFlow runs. The deletion
// asks the Session to complete exactly once, ends the Preview and releases
// every Sandbox resource by label: the Session completes with its cleanup
// receipt after one CompleteProjectSession Step, the PreviewFlow completes,
// and the Project ends DELETED with nothing of the Sandbox left.
func TestProjectDeletionClosesSessionDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+projectDeletionTimeout)
	defer cancel()
	probe := startCleanupProbe(t, ctx)
	probe.demandWorkspace(ctx)
	ready := probe.awaitReady(ctx)
	if err := probe.previews.Start(ctx, sandbox.Identity{ProjectID: probe.identity.ProjectID}); err != nil {
		t.Fatalf("the Preview did not start (%T)", err)
	}
	probe.requestDeletion(ctx, "delete-ready")
	probe.awaitDeleted(ctx)
	probe.requireReleased(ctx, ready.InstanceID)
	probe.requireCompleted(ctx)
	steps := completedSteps(t, ctx, probe.reader, string(probe.id))
	if completions := count(steps, dex.GetFinalStepType(sessionagent.CompleteProjectSession{})); completions != 1 {
		t.Fatalf("the deletion completed the Session %d times", completions)
	}
	previewFlow, err := probe.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: preview.FlowID(probe.identity.ProjectID)})
	if err != nil || previewFlow.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_COMPLETED {
		t.Fatalf("the deletion did not end the Project's Preview (%s)", dexflowtest.ErrorCategory(err))
	}
	t.Logf("FlowID=%s completed once by the deletion; the Preview ended and %s with volume %s was released", probe.id, ready.InstanceID, ready.Resources.Volume.UID)
}

// TestProjectDeletionReleaseFailureDexIntegration lets the Session reclaim its
// idle instance at its deadline, which keeps the volume, and gives the volume
// to another owner: its label that names the Project names another one. The
// Project's deletion completes the Session but cannot release that volume: it
// records the failure and stays DELETING, never DELETED. Once the label is
// restored, a retried deletion releases the volume and ends DELETED. Besides
// the settings above it needs SUPERVERSE_TEST_SANDBOX_HOLD_SECONDS.
func TestProjectDeletionReleaseFailureDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+hold+sandboxReclaimTimeout+2*projectDeletionTimeout)
	defer cancel()
	probe := startCleanupProbe(t, ctx)
	probe.demandWorkspace(ctx)
	ready := probe.awaitReady(ctx)
	probe.await(ctx, "the reclamation at the Sandbox deadline", func() bool {
		instance := probe.workspace(ctx).Instance
		return instance == sessionworkspace.InstanceReclaimed || instance == sessionworkspace.InstanceFailed
	})
	reclaimed := probe.workspace(ctx)
	if reclaimed.Instance != sessionworkspace.InstanceReclaimed || reclaimed.Resources.Instance.UID != "" || reclaimed.Resources.Volume.UID != ready.Resources.Volume.UID {
		t.Fatalf("the reclamation did not keep the volume: %s (%+v)", reclaimed.Instance, reclaimed.InstanceFailure)
	}
	volume := ready.Resources.Volume
	if !probe.objects.volumeKept(ctx, ready.Resources) {
		t.Fatalf("the reclamation did not keep the volume %s", volume.Name)
	}

	// Another owner takes the volume: its label that names the Project names
	// another one, so the deletion cannot release it.
	probe.objects.ownVolume(ctx, ready.Resources, "otherowner")
	probe.requestDeletion(ctx, "delete-conflict")
	failed := probe.awaitProject(ctx, "the release failure", func(project projects.Project) bool { return project.LastFailure != nil })
	if failed.ProjectStatus != projects.StatusDeleting {
		t.Fatalf("the failed deletion left the Project %s", failed.ProjectStatus)
	}
	t.Logf("deletion failed with %s and the Project stays %s", failed.LastFailure.Code, failed.ProjectStatus)
	probe.objects.ownVolume(ctx, ready.Resources, probe.identity.ProjectID)
	probe.requestDeletion(ctx, "delete-retry")
	probe.awaitDeleted(ctx)
	probe.requireReleased(ctx, ready.InstanceID)
	probe.requireCompleted(ctx)
	t.Logf("FlowID=%s reclaimed with volume %s kept; the retried deletion released it", probe.id, volume.UID)
}

// TestProjectDeletionDuringProvisioningDexIntegration deletes the Project
// while the Session's workspace Flow provisions its first instance. The
// deletion completes the Session, then ends the workspace Flow, which
// cancels its wait for the provisioning, and the cleanup pass waits until
// the provisioning ended before it releases the Sandbox by label: the
// Project ends DELETED, both Flows completed and nothing of the Sandbox
// remains.
func TestProjectDeletionDuringProvisioningDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+projectDeletionTimeout)
	defer cancel()
	probe := startCleanupProbe(t, ctx)
	requestID := probe.demandWorkspace(ctx)
	probe.requireProvisioning(ctx, requestID)
	probe.requestDeletion(ctx, "delete-provisioning")
	probe.awaitDeleted(ctx)
	probe.requireReleased(ctx, requestID)
	probe.requireCompleted(ctx)
	steps := completedSteps(t, ctx, probe.reader, sessionworkspace.FlowIDForTest(probe.identity.ProjectID))
	if count(steps, stepType(sessionworkspace.CompleteSessionWorkspace{})) != 1 {
		t.Fatalf("the deletion did not complete the workspace Flow once: %v", steps)
	}
	t.Logf("the workspace Flow ran %v; AwaitProvisioningOutcome cancelled: %t", steps, !slices.Contains(steps, stepType(sessionworkspace.AwaitProvisioningOutcome{})))
	t.Logf("FlowID=%s deleted while %s was provisioned; nothing remains", probe.id, requestID)
}

// TestProjectDeletionDuringReclamationDexIntegration deletes the Project while
// the Session's workspace Flow reclaims its idle instance at the Sandbox
// deadline. The deletion's end of the workspace Flow cancels the
// reclamation, and the cleanup pass releases everything by label: the
// Project ends DELETED, both Flows completed and nothing of the Sandbox
// remains. Besides the settings above it needs
// SUPERVERSE_TEST_SANDBOX_HOLD_SECONDS.
func TestProjectDeletionDuringReclamationDexIntegration(t *testing.T) {
	hold := sandboxHold(t)
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+hold+projectDeletionTimeout)
	defer cancel()
	probe := startCleanupProbe(t, ctx)
	requestID := probe.demandWorkspace(ctx)
	probe.awaitReady(ctx)
	probe.await(ctx, "the reclamation at the Sandbox deadline", func() bool {
		instance := probe.workspace(ctx).Instance
		if instance == sessionworkspace.InstanceReclaimed {
			t.Fatalf("the reclamation of %s ended before it was observed; the scenario was not exercised", requestID)
		}
		return instance == sessionworkspace.InstanceReclaiming
	})
	probe.requestDeletion(ctx, "delete-reclaiming")
	if instance := probe.workspace(ctx).Instance; instance != sessionworkspace.InstanceReclaiming {
		t.Fatalf("the reclamation ended as %s before the deletion was admitted; the scenario was not exercised", instance)
	}
	probe.awaitDeleted(ctx)
	probe.requireReleased(ctx, requestID)
	probe.requireCompleted(ctx)
	steps := completedSteps(t, ctx, probe.reader, sessionworkspace.FlowIDForTest(probe.identity.ProjectID))
	if count(steps, stepType(sessionworkspace.CompleteSessionWorkspace{})) != 1 {
		t.Fatalf("the deletion did not complete the workspace Flow once: %v", steps)
	}
	t.Logf("the workspace Flow ran %v; ReclaimInstance cancelled: %t", steps, !slices.Contains(steps, stepType(sessionworkspace.ReclaimInstance{})))
	t.Logf("FlowID=%s deleted while its workspace Flow reclaimed %s; nothing remains", probe.id, requestID)
}

// TestProjectDeletionAfterAbnormalSessionEndDexIntegration terminates the
// engine execution of the Project's Session while its instance is READY, as an
// operator stop would, and deletes the Project. The Session cannot complete,
// so the deletion does not ask it: the cleanup pass releases its instance by
// label, the instance, the volume and the boundary, and discharges it by exact
// membership and resource absence. The Project ends DELETED.
func TestProjectDeletionAfterAbnormalSessionEndDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+projectDeletionTimeout)
	defer cancel()
	probe := startCleanupProbe(t, ctx)
	probe.demandWorkspace(ctx)
	ready := probe.awaitReady(ctx)
	if err := probe.sdk.StopFlow(ctx, string(probe.id), dex.StopOptions{Type: dex.TerminateFlow, Reason: "abnormal Session end before a Project deletion test"}); err != nil {
		t.Fatalf("terminate failed (%T)", err)
	}
	if abnormal, err := probe.client.HasAbnormalProjectSessionTermination(ctx, probe.id); err != nil || !abnormal {
		t.Fatalf("the terminated Session does not read as ended abnormally (%T)", err)
	}
	probe.requestDeletion(ctx, "delete-abnormal")
	probe.awaitDeleted(ctx)
	probe.requireReleased(ctx, ready.InstanceID)
	t.Logf("FlowID=%s terminated with %s READY; the deletion released volume %s", probe.id, ready.InstanceID, ready.Resources.Volume.UID)
}

// TestProjectDeletionStopsBackgroundCommandWatchDexIntegration deletes a
// Project while its Session's background command sleeps and the turn of the
// background-long-turn scenario still runs, on the mock model. The deletion's
// completion call stops the watch of the running command instead of cancelling
// its wait: AwaitBackgroundCommand stops its BackgroundCommandFlow SubFlow and
// records the command stopped. The Session completes with its cleanup receipt,
// the Project ends DELETED, and no BackgroundCommandFlow runs on.
func TestProjectDeletionStopsBackgroundCommandWatchDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+projectDeletionTimeout)
	defer cancel()
	probe := startCleanupProbeOn(t, ctx, conversation.ModelMock)
	message := conversation.PendingUserMessage{MessageID: conversation.MessageID(fmt.Sprintf("delete-background-%d", time.Now().UnixNano())),
		Value: conversation.UserMessage{Content: mockInput(t, "background-long-turn", 1).Content}}
	if err := probe.client.SendPendingMessage(ctx, probe.id, message); err != nil {
		t.Fatalf("sending the scenario's message failed (%T)", err)
	}
	var watch string
	probe.await(ctx, "the background command's watch", func() bool {
		snapshot, err := probe.client.GetSnapshot(ctx, probe.id)
		if err != nil || snapshot.Agent == nil || len(snapshot.Agent.Description.BackgroundCommands) != 1 {
			return false
		}
		watches := backgroundCommandWatches(t, ctx, probe.reader, probe.id)
		if len(watches) == 1 {
			watch = watches[0]
		}
		return watch != ""
	})
	ready := probe.workspace(ctx)
	if status := probe.flowStatus(ctx, watch); status != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		t.Fatalf("the watch %s of the running command is %s before the deletion; the scenario was not exercised", watch, status)
	}
	probe.requestDeletion(ctx, "delete-background-command")
	probe.awaitDeleted(ctx)
	probe.requireReleased(ctx, ready.InstanceID)
	probe.requireCompleted(ctx)
	probe.await(ctx, "the end of the watch "+watch, func() bool {
		return probe.flowStatus(ctx, watch) != dexpb.FlowStatus_FLOW_STATUS_RUNNING
	})
	snapshot, err := probe.client.GetSnapshot(ctx, probe.id)
	if err != nil || snapshot.Agent == nil {
		t.Fatalf("the completed Session's Snapshot read failed (%T)", err)
	}
	if commands := snapshot.Agent.Description.BackgroundCommands; len(commands) != 1 || commands[0].Status != agenttasks.BackgroundCommandStopped {
		t.Fatalf("the deletion did not record the background command stopped: %+v", commands)
	}
	steps := completedSteps(t, ctx, probe.reader, string(probe.id))
	if completions := count(steps, dex.GetFinalStepType(sessionagent.AwaitBackgroundCommand{})); completions != 1 {
		t.Fatalf("AwaitBackgroundCommand completed %d times, not once, before the Session completed", completions)
	}
	t.Logf("FlowID=%s completed by the deletion after stopping the watch %s (%s)", probe.id, watch, probe.flowStatus(ctx, watch))
}

// flowStatus reads the engine status of flowID, a diagnostic read.
func (p *cleanupProbe) flowStatus(ctx context.Context, flowID string) dexpb.FlowStatus {
	p.t.Helper()
	summary, err := p.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil {
		p.t.Fatalf("engine summary read of %s failed (%s)", flowID, dexflowtest.ErrorCategory(err))
	}
	return summary.GetFlowStatus()
}

// cleanupProbe is the Session of a throwaway READY Project that a deletion
// test created, with the clients of the Flows the test drives and the objects
// of the lane's runtime. boundary names the Sandbox's namespace in Kind and
// its network in Docker.
type cleanupProbe struct {
	t          *testing.T
	sdk        *dex.Client
	flow       *sessionagent.SessionAgentFlow
	projects   *projects.ProjectFlowClient
	client     *sessionagent.SessionAgentFlowClient
	provisions *provision.SandboxProvisionFlowClient
	workspaces *sessionworkspace.SessionWorkspaceFlowClient
	previews   *preview.PreviewFlowClient
	subagents  *subagent.SubagentFlowClient
	reader     dexpb.FlowServiceClient
	objects    sandboxObjects
	id         agentloop.FlowID
	identity   agentloop.SessionIdentity
	boundary   string
}

// startCleanupProbe creates a throwaway READY Project and waits until its
// Session, which the Project started, is initialized.
func startCleanupProbe(t *testing.T, ctx context.Context) *cleanupProbe {
	t.Helper()
	return startCleanupProbeOn(t, ctx, "")
}

// startCleanupProbeOn creates a throwaway READY Project, waits until its
// Session, which the Project started, is initialized, and selects model unless
// the Session started on it or model is empty.
func startCleanupProbeOn(t *testing.T, ctx context.Context, model conversation.Model) *cleanupProbe {
	t.Helper()
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	objects := newSandboxObjects(t)
	projectFlow, provisionFlow, previewFlow, flow, subagentFlow := &projects.ProjectFlow{}, &provision.SandboxProvisionFlow{}, &preview.PreviewFlow{}, &sessionagent.SessionAgentFlow{}, &subagent.SubagentFlow{}
	workspaceFlow := &sessionworkspace.SessionWorkspaceFlow{}
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow, provisionFlow, previewFlow, flow, subagentFlow, workspaceFlow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	names := deploymentnames.MustFor(deploymentnames.EnvironmentTest)
	// This client registry supplies definitions only. Real handlers and every
	// application dependency belong to the existing Worker selected above.
	probe := &cleanupProbe{t: t, sdk: sdk, flow: flow, reader: reader, objects: objects,
		projects:   &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(postgrestest.OpenForTest(t)), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmails: []string{"admin@superverse.test"}},
		client:     sessionagent.NewSessionAgentFlowClientForTest(sdk, flow, reader, ""),
		provisions: provision.NewSandboxProvisionFlowClientForTest(sdk, provisionFlow, reader, names),
		workspaces: sessionworkspace.NewSessionWorkspaceFlowClientForTest(sdk, workspaceFlow, reader),
		previews:   &preview.PreviewFlowClient{Dex: sdk, Flow: previewFlow, Execution: reader},
		subagents:  subagent.NewSubagentFlowClientForTest(sdk, subagentFlow, reader)}
	project := projecttest.CreateForTest(ctx, t, probe.projects, t.Name())
	probe.identity = agentloop.SessionIdentity{ProjectID: project.ProjectID, Profile: agentloop.ProfileSessionV1, HeadCommit: project.AppManifest.SourceCommit}
	probe.id, probe.boundary = agentloop.SessionFlowID(project.ProjectID), names.SessionNamespace(project.ProjectID)
	var started agentloop.AgentDescription
	probe.await(ctx, "Session initialization", func() bool {
		snapshot, readErr := probe.client.GetSnapshot(ctx, probe.id)
		if readErr != nil || snapshot.Agent == nil {
			return false
		}
		started = snapshot.Agent.Description
		return true
	})
	if model != "" && started.Model != model {
		if err := probe.client.UpdateSessionModel(ctx, probe.id, agentloop.UpdateSessionModelRequest{Model: model, ExpectedRevision: started.ModelRevision}); err != nil {
			t.Fatalf("selecting %s for %s failed (%T)", model, probe.id, err)
		}
	}
	return probe
}

// await polls done until it holds; past ctx the test fails.
func (p *cleanupProbe) await(ctx context.Context, what string, done func() bool) {
	p.t.Helper()
	for !done() {
		select {
		case <-ctx.Done():
			p.t.Fatalf("%s did not converge; reconcile %s", what, p.id)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// summary reads the retained lifecycle of the Session.
func (p *cleanupProbe) summary(ctx context.Context) agentloop.SessionSummary {
	p.t.Helper()
	summary, err := p.client.GetSessionSummary(ctx, p.id)
	if err != nil {
		p.t.Fatalf("Session read failed (%T)", err)
	}
	return summary
}

// demandWorkspace opens the cold path through the Preview-scope demand and
// returns the instance whose provisioning it started.
func (p *cleanupProbe) demandWorkspace(ctx context.Context) string {
	p.t.Helper()
	if _, err := p.client.RequestSessionWorkspace(ctx, p.id, agentloop.WorkspaceDemandRequest{}); err != nil {
		p.t.Fatalf("workspace demand failed (%T)", err)
	}
	var requestID string
	p.await(ctx, "a provisioning instance", func() bool {
		ws := p.workspace(ctx)
		requestID = ws.InstanceID
		return ws.Instance == sessionworkspace.InstanceProvisioning
	})
	return requestID
}

// awaitReady waits until the Session holds a READY instance and returns its
// workspace.
func (p *cleanupProbe) awaitReady(ctx context.Context) workspaceView {
	p.t.Helper()
	var ws workspaceView
	p.await(ctx, "a READY instance", func() bool {
		ws = p.workspace(ctx)
		if ws.State == agentloop.WorkspaceFailed {
			p.t.Fatalf("workspace request %s failed with %s", ws.RequestID, ws.Failure)
		}
		return ws.State == agentloop.WorkspaceReady && ws.Instance == sessionworkspace.InstanceReady
	})
	return ws
}

// requireProvisioning waits until the provisioning of requestID started and
// fails if it already ended, since the scenario needs it running while the
// deletion begins. The Session marks its instance provisioning before its
// Step starts the provisioning, so the execution can be missing at first.
func (p *cleanupProbe) requireProvisioning(ctx context.Context, requestID string) {
	p.t.Helper()
	p.await(ctx, "the provisioning of "+requestID, func() bool {
		state, err := p.provisions.Get(ctx, p.identity.ProjectID, requestID)
		var missing *dex.FlowNotFoundError
		if errors.As(err, &missing) {
			return false
		}
		if err != nil || state.Outcome != nil {
			p.t.Fatalf("the provisioning of %s ended before the deletion began; the scenario was not exercised (%T)", requestID, err)
		}
		return true
	})
}

// requestDeletion admits the deletion of the Project at its current
// revision; a failed deletion admits it again.
func (p *cleanupProbe) requestDeletion(ctx context.Context, requestID string) {
	p.t.Helper()
	project, err := p.projects.Read(ctx, p.identity.ProjectID)
	if err != nil {
		p.t.Fatalf("Project read failed (%T)", err)
	}
	if _, err = p.projects.RequestDeletion(ctx, p.identity.ProjectID, requestID, project.Revision); err != nil {
		p.t.Fatalf("the deletion of Project %s was not admitted (%T)", p.identity.ProjectID, err)
	}
}

// awaitProject waits until the Project satisfies done and returns it.
func (p *cleanupProbe) awaitProject(ctx context.Context, what string, done func(projects.Project) bool) projects.Project {
	p.t.Helper()
	var project projects.Project
	p.await(ctx, what, func() bool {
		var err error
		project, err = p.projects.Read(ctx, p.identity.ProjectID)
		return err == nil && done(project)
	})
	return project
}

// awaitDeleted waits until the Project is DELETED; a recorded failure fails
// the test.
func (p *cleanupProbe) awaitDeleted(ctx context.Context) {
	p.t.Helper()
	p.awaitProject(ctx, "the deletion of the Project", func(project projects.Project) bool {
		if project.LastFailure != nil {
			p.t.Fatalf("the deletion of Project %s failed in %s with %s: %s", project.ProjectID, project.LastFailure.Phase, project.LastFailure.Code, project.LastFailure.Message)
		}
		return project.ProjectStatus == projects.StatusDeleted
	})
}

// requireReleased fails unless the deleted Project left nothing of its
// Sandbox: the provisioning of requestID ended, the Sandbox's boundary, with
// its instance and volume, is gone, and the Project's Preview does not run.
func (p *cleanupProbe) requireReleased(ctx context.Context, requestID string) {
	p.t.Helper()
	execution, err := p.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: provision.FlowIDForTest(p.identity.ProjectID, requestID)})
	if err != nil || execution.GetFlowStatus() == dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		p.t.Fatalf("the provisioning of %s still runs after the deletion (%T)", requestID, err)
	}
	if !p.objects.released(ctx, p.identity.ProjectID, p.boundary) {
		p.t.Fatalf("the boundary %s of the deleted Project's Sandbox remains", p.boundary)
	}
	previewFlow, err := p.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: preview.FlowID(p.identity.ProjectID)})
	if err == nil && previewFlow.GetFlowStatus() == dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		p.t.Fatal("the Preview of the deleted Project still runs")
	}
}

// requireCompleted fails unless the Session completed with its graceful
// cleanup receipt and its workspace Flow, when the Session sent it a command,
// completed.
func (p *cleanupProbe) requireCompleted(ctx context.Context) {
	p.t.Helper()
	if done, err := p.client.ProjectSessionCompleted(ctx, p.identity.ProjectID); err != nil || !done {
		p.t.Fatalf("Session %s has no matching graceful cleanup receipt (%s)", p.id, dexflowtest.ErrorCategory(err))
	}
	workspace, err := p.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: sessionworkspace.FlowIDForTest(p.identity.ProjectID)})
	if err == nil && workspace.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_COMPLETED {
		p.t.Fatalf("the workspace Flow of the deleted Project is %s, not completed", workspace.GetFlowStatus())
	}
}

// count is how often value occurs in values.
func count(values []string, value string) int {
	return len(slices.DeleteFunc(slices.Clone(values), func(candidate string) bool { return candidate != value }))
}
