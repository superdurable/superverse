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
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// These scenarios run on the Session of a throwaway READY Project and the
// actual Worker of the mock Session tests: each Session cold-starts its
// instance through its SessionWorkspaceFlow and a real SandboxProvisionFlow,
// and the workspace Flow owns the instance until it reclaims it.

// previewCommandTimeout bounds one Preview command: a source preparation, or
// a start that builds and serves the application in the Sandbox.
const previewCommandTimeout = 20 * time.Minute

// TestSandboxProvisionStartJoinsDexIntegration cold-starts a Session's first
// instance through exactly one SandboxProvisionFlow. A repeated start of the
// same request, as a retry of the workspace Flow's start call makes, joins
// that completed execution: the engine keeps its one run and records no
// event. The provisioning measured provision and checkout, and the workspace
// Flow holds the same phases for its READY instance.
func TestSandboxProvisionStartJoinsDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	started := time.Now().UTC()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	ready := session.workspace(ctx)
	if ready.Instance != sessionworkspace.InstanceReady {
		t.Fatalf("the cold start left the instance %s %s", ready.InstanceID, ready.Instance)
	}
	flowID := provision.FlowIDForTest(session.identity.ProjectID, ready.InstanceID)
	if flowIDs := session.provisioningFlowIDs(ctx, started); !slices.Equal(flowIDs, []string{flowID}) {
		t.Fatalf("the cold start ran the provisionings %v instead of only %s", flowIDs, flowID)
	}
	provisioned, err := session.provisions.Get(ctx, session.identity.ProjectID, ready.InstanceID)
	if err != nil || provisioned.Outcome == nil || provisioned.Outcome.Status != provision.OutcomeReady || provisioned.Outcome.Reported != provision.ReportAdmittedForTest {
		t.Fatalf("the provisioning %s did not report its READY outcome to the workspace Flow (%T)", flowID, err)
	}
	requirePhases(t, "the provisioning "+flowID, provisioned.WorkspaceTimings, []string{provision.PhaseProvisionForTest, provision.PhaseCheckoutForTest}, started)
	if !slices.EqualFunc(ready.Timings, provisioned.WorkspaceTimings, samePhase) {
		t.Fatalf("the workspace Flow holds the phases %+v of %s, not the provisioning's %+v", ready.Timings, ready.InstanceID, provisioned.WorkspaceTimings)
	}
	before, events := session.execution(ctx, flowID), session.historyEvents(ctx, flowID)
	if err = session.provisions.Start(ctx, provisioned.Request); err != nil {
		t.Fatalf("a repeated start of %s was refused (%T)", flowID, err)
	}
	after := session.execution(ctx, flowID)
	if after.GetFlowExecutionId().GetRunId() != before.GetFlowExecutionId().GetRunId() || after.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_COMPLETED ||
		session.historyEvents(ctx, flowID) != events {
		t.Fatalf("the repeated start of %s started or changed an execution", flowID)
	}
	t.Logf("FlowID=%s cold-started %s through its only provisioning %s, which a repeated start joined", session.id, ready.InstanceID, flowID)
}

// TestPlatformBootstrapRacesAgentDexIntegration runs build-basic on a fresh
// Project, whose script runs the template's make bootstrap before its first
// push. The provisioning started the bootstrap the template declares as the
// command ledger run platform-bootstrap-<instance> before the instance was
// READY, without waiting for it; the scenario's own make bootstrap waited for
// that run's lock and restored nothing again, and the first commit gate passed
// on the restored caches. The log names each gate stage's duration_ms.
func TestPlatformBootstrapRacesAgentDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	started := time.Now().UTC()
	session := newMockSession(t, ctx, mockInput(t, "build-basic", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	ready := session.workspace(ctx)
	provisioned, err := session.provisions.Get(ctx, session.identity.ProjectID, ready.InstanceID)
	if err != nil {
		t.Fatalf("the provisioning of %s did not read (%T)", ready.InstanceID, err)
	}
	bootstrap := provisioned.Bootstrap
	if !bootstrap.Declared || bootstrap.CommandID != "platform-bootstrap-"+ready.InstanceID || bootstrap.Refusal != "" ||
		bootstrap.StartedAt.Before(started.Add(-time.Minute)) || bootstrap.StartedAt.After(time.Now().Add(time.Minute)) {
		t.Fatalf("the provisioning of %s did not start the declared bootstrap: %+v", ready.InstanceID, bootstrap)
	}
	messages := agent.History.Messages
	if result := toolResult(t, messages, conversation.ToolBash, 1); result.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("the scenario's make bootstrap ended %s", result.Outcome)
	}
	if output := session.toolResultContent(ctx, messages, conversation.ToolBash, 1); !strings.Contains(output, "web/node_modules already matches web/package-lock.json") {
		t.Fatalf("the scenario's make bootstrap restored the dependencies again instead of waiting for the platform's run:\n%s", output)
	}
	if got := toolResult(t, messages, conversation.ToolCommitAndPush, 0); got.ResultCode != agentloop.CommitPushedForTest {
		t.Fatalf("the first commit_and_push ended as %q (%s)", got.ResultCode, got.Outcome)
	}
	var report struct {
		Gate struct {
			Stages []struct {
				Stage      string `json:"stage"`
				Status     string `json:"status"`
				DurationMS int64  `json:"duration_ms"`
			} `json:"stages"`
		} `json:"gate"`
	}
	if err = json.Unmarshal([]byte(session.toolResultContent(ctx, messages, conversation.ToolCommitAndPush, 0)), &report); err != nil {
		t.Fatalf("the pushed result is not the commit report: %v", err)
	}
	for _, stage := range report.Gate.Stages {
		t.Logf("gate stage %s %s duration_ms=%d", stage.Stage, stage.Status, stage.DurationMS)
	}
	t.Logf("FlowID=%s instance %s started the bootstrap %s at %s", session.id, ready.InstanceID, bootstrap.CommandID, bootstrap.StartedAt.Format(time.RFC3339Nano))
}

// TestSandboxReclaimStopsPreviewDexIntegration serves the Project's Preview on
// the Session's instance, which the lend to its commands holds for another
// Sandbox hold and its ready notification for the Preview hold, and lets the
// Session reclaim the instance at its deadline. The start measures its five
// phases in order. The Preview holds no Project
// lease: its start passes VerifyPreviewSource. The reclamation asks the
// Preview to release the instance first: the Preview stops its engine through
// the supervisor in the Pod, which therefore still ran, records the stop
// GRACEFUL and gives up the instance before the Session records the Pod gone,
// and it stays STOPPED. A later pushed-commit build may resume the workspace;
// read-only configuration does not. The reclamation removed the Project's
// route, so the local gateway (Envoy Gateway on Kind, Traefik on Native) soon
// answers 404 for the Project's host, and the Preview configuration of the
// prepared HEAD stays readable: the read starts no provisioning and leaves the
// reclaimed workspace as it was. The fresh Project's Preview is configured as
// a person does in Studio (configurePreview). Besides the mock Session inputs it needs
// SUPERVERSE_TEST_SANDBOX_HOLD_SECONDS,
// SUPERVERSE_TEST_SANDBOX_PREVIEW_HOLD_SECONDS, at most 600,
// SUPERVERSE_E2E_OPENAI_FILE and the lane's storage inputs.
func TestSandboxReclaimStopsPreviewDexIntegration(t *testing.T) {
	hold := max(sandboxHold(t), sandboxPreviewHold(t, 10*time.Minute))
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+2*previewCommandTimeout+hold+sandboxReclaimTimeout)
	defer cancel()
	begun := time.Now().UTC()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	ready := session.workspace(ctx)
	service, run := session.service(), time.Now().UTC().Format("20060102150405")
	project := session.identity.ProjectID

	// The first command, in the first-demand form, starts the Preview.
	prepare := preview.Command{ID: "prepare-" + run, Kind: preview.CommandPreparePreviewSource}
	if err := service.PreparePreviewSource(ctx, session.account, project, prepare); err != nil {
		t.Fatalf("the source preparation was not admitted (%T)", err)
	}
	prepared := session.awaitPreview(ctx, prepare.ID)
	if prepared.PreparedSource.Status != preview.PreviewSourceReadyForTest || prepared.WorkspaceRequestID != ready.InstanceID {
		t.Fatalf("the source of instance %s was not prepared: %s (%s)", ready.InstanceID, prepared.PreparedSource.Status, failureCode(prepared))
	}
	// The template's declared connection is unconfigured on a fresh Project.
	session.configurePreview(ctx)
	startedAt := time.Now().UTC()
	start := preview.Command{ID: "start-" + run, Kind: preview.CommandStartPreview,
		WorkspaceRequestID: prepared.WorkspaceRequestID, Generation: prepared.Generation, ExpectedRevision: prepared.Revision}
	if _, err := service.PreviewCommand(ctx, session.account, project, start); err != nil {
		t.Fatalf("the Preview start was not admitted (%T)", err)
	}
	started := session.awaitPreview(ctx, start.ID)
	if started.Preview.Status != preview.PreviewReady || started.Resources.Instance.UID != ready.Resources.Instance.UID {
		t.Fatalf("the Preview does not serve on instance %s: %s (%s)", ready.InstanceID, started.Preview.Status, failureCode(started))
	}
	requirePhases(t, "the Preview start "+start.ID, started.Preview.Timings,
		[]string{preview.PhaseConfigurationForTest, preview.PhaseEnvironmentForTest, preview.PhaseGrantForTest, preview.PhaseApplicationForTest, preview.PhaseRouteForTest}, startedAt)
	if steps := completedSteps(t, ctx, session.reader, preview.FlowID(project)); !slices.Contains(steps, dex.GetFinalStepType(preview.VerifyPreviewSource{})) {
		t.Fatalf("the Preview started without VerifyPreviewSource: %v", steps)
	}
	if code := previewRouteStatus(t, ctx, started.Preview.URL); code >= http.StatusBadRequest {
		t.Fatalf("the served Preview answered %d through the gateway", code)
	}

	held := session.workspace(ctx)
	closed := session.awaitReclaim(ctx, held)
	stopped, err := session.previews.Get(ctx, project)
	switch {
	case err != nil:
		t.Fatalf("the Preview read failed (%T)", err)
	case stopped.Preview.Status != preview.PreviewStoppedForTest || stopped.LastCommand.Kind != preview.CommandReclaim || stopped.LastCommand.WorkspaceRequestID != held.InstanceID:
		t.Fatalf("the reclamation of %s did not stop the Preview: %s after %s", held.InstanceID, stopped.Preview.Status, stopped.LastCommand.Kind)
	case stopped.Environment.Status != preview.EnvironmentStoppedForTest || stopped.Environment.LastStop != preview.EnvironmentStopGracefulForTest:
		t.Fatalf("the Preview engine on %s was not stopped by its supervisor before the Pod deletion: %s %s (%s)",
			held.InstanceID, stopped.Environment.Status, stopped.Environment.LastStop, stopped.Environment.LastStopCause)
	case stopped.Resources.Instance.UID != "" || stopped.UpdatedAt.After(closed.InstanceUpdatedAt):
		t.Fatalf("the Preview released the instance %s after the Session reclaimed it", held.InstanceID)
	}
	// The stop removed the route: once the gateway loaded that, it knows the
	// host no more.
	awaitRouteGone(t, ctx, started.Preview.URL)
	// The Preview configuration is the prepared HEAD's, read from storage.
	manifest, err := service.ReadPreviewTarget(ctx, project)
	if err != nil || manifest.GetSourceCommit() != prepared.PreparedSource.SourceCommit {
		t.Fatalf("the Preview configuration of %s is unreadable without a workspace (%v)", prepared.PreparedSource.SourceCommit, err)
	}
	if after := session.workspace(ctx); after.RequestID != closed.RequestID || after.Instance != sessionworkspace.InstanceReclaimed || after.State != agentloop.WorkspaceRevoked {
		t.Fatalf("the configuration read woke the Sandbox: %s %s %s", after.RequestID, after.Instance, after.State)
	}
	if flowIDs := session.provisioningFlowIDs(ctx, begun); len(flowIDs) != 1 {
		t.Fatalf("the Session provisioned %v; the configuration read must start none", flowIDs)
	}
	t.Logf("FlowID=%s Preview %s stopped and released %s before the Session reclaimed it at its deadline; its route went with it and its configuration read woke nothing", session.id, start.ID, held.InstanceID)
}

// routeRemovalWait bounds how long the local gateway takes to stop serving a
// removed route: Envoy Gateway watches HTTPRoutes on Kind and Traefik polls
// the server's route provider on Native, each within seconds.
const routeRemovalWait = time.Minute

// awaitRouteGone waits until the local gateway answers 404 for the host of
// publicURL, as either gateway does for a host it has no route for.
func awaitRouteGone(t *testing.T, ctx context.Context, publicURL string) {
	t.Helper()
	deadline := time.Now().Add(routeRemovalWait)
	for {
		code := previewRouteStatus(t, ctx, publicURL)
		if code == http.StatusNotFound {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the gateway still answers %d for the removed route of %s", code, publicURL)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the gateway still answers %d for the removed route of %s (%v)", code, publicURL, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

// previewRouteStatus requests the root of the Project's public Preview URL
// through the local gateway, which serves every local host on the loopback
// address of the URL's port, and returns the status it answers.
func previewRouteStatus(t *testing.T, ctx context.Context, publicURL string) int {
	t.Helper()
	target, err := url.Parse(publicURL)
	if err != nil || target.Port() == "" {
		t.Fatalf("the Preview URL %q names no local gateway port", publicURL)
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", target.Port()))
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, publicURL, nil)
	if err != nil {
		t.Fatalf("the Preview request is invalid (%T)", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("the gateway did not answer the Preview host (%T)", err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode
}

// TestPreviewAfterRefusedFirstCommandDexIntegration refuses the first command
// of a Project's Preview before the Preview acquired an instance, then sends the
// next command as Studio does: it names the Preview it observed, its revision
// and generation with no instance. The Session reclaims its first instance at
// its deadline, and another owner then takes the workspace volume with the
// operator's tools of the local lane (sandboxObjects.ownVolume), so the
// request the first command opens fails its provisioning and the Preview
// refuses the command. Once the volume is the Project's again, the next
// command is admitted and prepares the source on a new instance. While it
// waits for that instance, the Session Service repeats the first command's
// failed settlement, as a retried Session Step does; the Preview admits it only
// for the command it names, so the next command is not refused with it. Besides
// the mock Session inputs it needs SUPERVERSE_TEST_SANDBOX_HOLD_SECONDS.
func TestPreviewAfterRefusedFirstCommandDexIntegration(t *testing.T) {
	hold, objects := sandboxHold(t), newSandboxObjects(t)
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout+hold+sandboxReclaimTimeout+2*previewCommandTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	closed := session.awaitReclaim(ctx, session.workspace(ctx))
	service, run := session.service(), time.Now().UTC().Format("20060102150405")
	project := session.identity.ProjectID

	objects.ownVolume(ctx, closed.Resources, "otherowner")
	isRestored := false
	t.Cleanup(func() {
		if !isRestored {
			restore, stop := context.WithTimeout(context.Background(), time.Minute)
			defer stop()
			objects.ownVolume(restore, closed.Resources, project)
		}
	})
	first := preview.Command{ID: "prepare-refused-" + run, Kind: preview.CommandPreparePreviewSource}
	if err := service.PreparePreviewSource(ctx, session.account, project, first); err != nil {
		t.Fatalf("the first command was not admitted (%T)", err)
	}
	refused := session.awaitPreview(ctx, first.ID)
	// The Session announces the settlement in the Step that records it.
	var failed workspaceView
	session.await(ctx, "the settlement of the workspace request", func() bool {
		failed = session.workspace(ctx)
		return failed.State != agentloop.WorkspaceRequested
	})
	if refused.Failure == nil || refused.WorkspaceRequestID != "" || failed.State != agentloop.WorkspaceFailed {
		t.Fatalf("the first command was not refused before the Preview acquired an instance: %s on %q, workspace %s", failureCode(refused), refused.WorkspaceRequestID, failed.State)
	}
	objects.ownVolume(ctx, closed.Resources, project)
	isRestored = true

	next := preview.Command{ID: "prepare-" + run, Kind: preview.CommandPreparePreviewSource, Generation: refused.Generation, ExpectedRevision: refused.Revision}
	if err := service.PreparePreviewSource(ctx, session.account, project, next); err != nil {
		t.Fatalf("the command that observed the refused Preview was not admitted (%T)", err)
	}
	if err := service.NotifyPreviewWorkspace(ctx, session.identity, first.ID, failed.RequestID, failed.Failure); err != nil {
		t.Fatalf("the repeated settlement of %s was not answered (%v)", first.ID, err)
	}
	prepared := session.awaitPreview(ctx, next.ID)
	ready := session.workspace(ctx)
	if prepared.PreparedSource.Status != preview.PreviewSourceReadyForTest || prepared.WorkspaceRequestID == "" || prepared.WorkspaceRequestID != ready.InstanceID || ready.Instance != sessionworkspace.InstanceReady {
		t.Fatalf("the next command, with the first command's settlement repeated, did not prepare the source on a new instance: %s on %q (%s)", prepared.PreparedSource.Status, prepared.WorkspaceRequestID, failureCode(prepared))
	}
	t.Logf("FlowID=%s refused %s with %s after %s failed with %s; %s prepared the source on %s", session.id, first.ID, failureCode(refused), failed.RequestID, failed.Failure, next.ID, ready.InstanceID)
}

// TestSandboxReplacementReclaimsFirstDexIntegration removes a Session's READY
// instance behind its back, as a lost node does, with the operator's tools of
// the local lane (sandboxObjects): the Sandbox object on Kind, the container
// on Docker. The next tool call finds the workspace unreachable, and the
// Session's provision command replaces the instance on the same volume: the
// SessionWorkspaceFlow reclaims the lost instance before it provisions the
// next one, so one volume never carries two instances. The workspace Flow's
// history shows the order, and the replacement holds the phases of its own
// provisioning, not the lost instance's.
func TestSandboxReplacementReclaimsFirstDexIntegration(t *testing.T) {
	objects := newSandboxObjects(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*mockTurnTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "inspect-basic", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	lost := session.workspace(ctx)
	if lost.Instance != sessionworkspace.InstanceReady || lost.Resources.Instance.Name == "" {
		t.Fatalf("the first turn left no READY instance: %s %s", lost.InstanceID, lost.Instance)
	}
	objects.removeInstance(ctx, lost.Resources)
	session.await(ctx, "the removal of the instance", func() bool { return objects.instanceGone(ctx, lost.Resources) })
	mark, replacing := markHistory(t, ctx, session.reader, sessionworkspace.FlowIDForTest(session.identity.ProjectID)), time.Now().UTC()
	next := conversation.PendingUserMessage{MessageID: conversation.MessageID("replacement-" + lost.InstanceID),
		Value: conversation.UserMessage{Content: mockInput(t, "inspect-basic", 1).Content}}
	if err := session.client.SendPendingMessage(ctx, session.id, next); err != nil {
		t.Fatalf("the next message was not admitted (%T)", err)
	}
	agent = session.finishedTurnAfter(ctx, agent.Description.LastSequence)
	requireNoFailure(t, agent.History)
	replaced := session.workspace(ctx)
	if replaced.InstanceID == lost.InstanceID || replaced.Instance != sessionworkspace.InstanceReady || replaced.Resources.Volume.UID != lost.Resources.Volume.UID {
		t.Fatalf("the turn did not run on a new instance of the same volume: %s %s", replaced.InstanceID, replaced.Instance)
	}
	requirePhases(t, "the replacement "+replaced.InstanceID, replaced.Timings, []string{provision.PhaseProvisionForTest, provision.PhaseCheckoutForTest}, replacing)
	steps := completedStepsSince(t, ctx, session.reader, mark)
	reclaimed := slices.Index(steps, stepType(sessionworkspace.ReclaimInstance{}))
	provisioned := slices.Index(steps, stepType(sessionworkspace.ProvisionInstance{}))
	if reclaimed < 0 || provisioned < reclaimed {
		t.Fatalf("the workspace Flow did not reclaim %s before it provisioned %s: %v", lost.InstanceID, replaced.InstanceID, steps)
	}
	t.Logf("FlowID=%s reclaimed the lost %s, then provisioned %s on volume %s", session.id, lost.InstanceID, replaced.InstanceID, lost.Resources.Volume.UID)
}

// service is the Session Service over the test's clients, for the Preview
// commands a browser sends through the API.
func (s *mockSession) service() *SessionFlowService {
	return &SessionFlowService{Projects: s.projects, Provisions: s.provisions, Workspaces: s.workspaces, Previews: s.previews, Agent: s.client,
		Logger: slog.New(slog.DiscardHandler)}
}

// awaitPreview waits until the Project's Preview finished the command
// commandID and returns the Preview after it.
func (s *mockSession) awaitPreview(ctx context.Context, commandID string) preview.Snapshot {
	s.t.Helper()
	var state preview.Snapshot
	s.await(ctx, "Preview command "+commandID, func() bool {
		var err error
		state, err = s.previews.Get(ctx, s.identity.ProjectID)
		return err == nil && state.ActiveCommand.ID == "" && state.LastCommand.ID == commandID
	})
	return state
}

// failureCode is the safe code of the Preview's last failure, if any.
func failureCode(state preview.Snapshot) string {
	if state.Failure == nil {
		return "no failure"
	}
	return state.Failure.Code
}

// phaseClockSkew is the difference the Worker's clock may have to the test's:
// on Kind the Worker runs in another clock domain.
const phaseClockSkew = time.Minute

// requirePhases requires timings to be exactly the phases want in order, all
// ended: the first started after begun, each starts where the previous ended
// and the last ended before now.
func requirePhases(t *testing.T, subject string, timings []dexflow.PhaseTiming, want []string, begun time.Time) {
	t.Helper()
	got := make([]string, 0, len(timings))
	for _, timing := range timings {
		got = append(got, timing.Phase)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s measured the phases %v, not %v", subject, got, want)
	}
	for i, timing := range timings {
		if timing.Duration <= 0 || timing.StartedAt.Before(begun.Add(-phaseClockSkew)) {
			t.Fatalf("%s measured the phase %s from %s for %s, not ended after %s", subject, timing.Phase, timing.StartedAt, timing.Duration, begun)
		}
		// The one-millisecond minimum of an ended phase can end it after the next one began.
		if i > 0 && (timing.StartedAt.Before(timings[i-1].StartedAt) || timing.StartedAt.After(timings[i-1].StartedAt.Add(timings[i-1].Duration))) {
			t.Fatalf("%s started the phase %s at %s, not where %s ended", subject, timing.Phase, timing.StartedAt, timings[i-1].Phase)
		}
	}
	if last := timings[len(timings)-1]; last.StartedAt.Add(last.Duration).After(time.Now().Add(phaseClockSkew)) {
		t.Fatalf("%s measured its last phase %s to end after now", subject, last.Phase)
	}
}

// samePhase reports whether two measurements are the same phase, however
// their times were encoded.
func samePhase(a, b dexflow.PhaseTiming) bool {
	return a.Phase == b.Phase && a.StartedAt.Equal(b.StartedAt) && a.Duration == b.Duration
}

// execution reads the engine summary of flowID, a diagnostic read of its
// current run.
func (s *mockSession) execution(ctx context.Context, flowID string) *dexpb.GetFlowSummaryResponse {
	s.t.Helper()
	summary, err := s.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil {
		s.t.Fatalf("engine summary read of %s failed (%T)", flowID, err)
	}
	return summary
}

// provisioningFlowIDs lists the SandboxProvisionFlows of the Project that
// started since started, a diagnostic search of the engine's executions.
func (s *mockSession) provisioningFlowIDs(ctx context.Context, started time.Time) []string {
	s.t.Helper()
	query := "FlowType = '" + dex.GetFinalFlowType(&provision.SandboxProvisionFlow{}) + "' AND StartTime >= '" + started.Add(-time.Minute).Format(time.RFC3339) + "'"
	var flowIDs []string
	token := ""
	for range 20 {
		page, err := s.reader.SearchFlows(ctx, &dexpb.SearchFlowsRequest{Query: query, PageSize: 100, NextPageToken: token})
		if err != nil {
			s.t.Fatalf("the provisioning search failed (%T)", err)
		}
		for _, run := range page.GetFlowRuns() {
			if _, own := flowid.ParseSandboxProvision(s.identity.ProjectID, run.GetFlowId()); own && !slices.Contains(flowIDs, run.GetFlowId()) {
				flowIDs = append(flowIDs, run.GetFlowId())
			}
		}
		if token = page.GetNextPageToken(); token == "" {
			return flowIDs
		}
	}
	s.t.Fatal("the provisioning search did not end within 20 pages")
	return nil
}

// historyMark is a point in the engine history of a Flow: its run and that
// run's last event then. A Flow can move to a new run, whose events count from
// 1 again, so what it completed later is read from both runs.
type historyMark struct {
	flowID, runID string
	lastEventID   int64
}

// markHistory records the current run of flowID and its last event.
func markHistory(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flowID string) historyMark {
	t.Helper()
	mark := historyMark{flowID: flowID, runID: currentRun(t, ctx, reader, flowID)}
	if events := runHistory(t, ctx, reader, flowID, mark.runID); len(events) > 0 {
		mark.lastEventID = events[len(events)-1].GetEventId()
	}
	return mark
}

// completedStepsSince lists the Step types whose Execute completed after mark,
// in history order: a diagnostic read of what a Flow ran. It reads the rest of
// the marked run and, when the Flow moved on, its current run.
func completedStepsSince(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, mark historyMark) []string {
	t.Helper()
	steps := completedStepsAfter(runHistory(t, ctx, reader, mark.flowID, mark.runID), mark.lastEventID)
	if current := currentRun(t, ctx, reader, mark.flowID); current != mark.runID {
		steps = append(steps, completedStepsAfter(runHistory(t, ctx, reader, mark.flowID, current), 0)...)
	}
	return steps
}

// completedSteps lists the Step types whose Execute completed in flowID since
// its first run began, in history order: completedStepsSince a mark at the
// start of that run.
func completedSteps(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flowID string) []string {
	t.Helper()
	summary, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil {
		t.Fatalf("engine summary read of %s failed (%T)", flowID, err)
	}
	return completedStepsSince(t, ctx, reader, historyMark{flowID: flowID, runID: summary.GetFirstRunId()})
}

func completedStepsAfter(events []*dexpb.FlowHistoryEvent, afterEventID int64) []string {
	var steps []string
	for _, event := range events {
		if completed := event.GetStepExecuteCompleted(); completed != nil && event.GetEventId() > afterEventID {
			steps = append(steps, completed.GetContext().GetStepType())
		}
	}
	return steps
}

// currentRun reads the run the engine currently runs flowID in.
func currentRun(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flowID string) string {
	t.Helper()
	summary, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil {
		t.Fatalf("engine summary read of %s failed (%T)", flowID, err)
	}
	return summary.GetFlowExecutionId().GetRunId()
}

// runHistory reads every event of one run of flowID. The engine serves history
// by run, and each page continues at the event the engine read up to,
// NextInternalEventId, which can lie after the page's last event.
func runHistory(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flowID, runID string) []*dexpb.FlowHistoryEvent {
	t.Helper()
	var events []*dexpb.FlowHistoryEvent
	for start := int64(1); ; {
		page, err := reader.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{
			FlowId: flowID, RunId: runID, StartInternalEventId: start, EstimatePageSize: 200,
		})
		if err != nil {
			t.Fatalf("history read of %s failed (%T)", flowID, err)
		}
		events = append(events, page.GetEvents()...)
		if len(page.GetNextPageToken()) == 0 {
			return events
		}
		start = page.GetNextInternalEventId()
	}
}
