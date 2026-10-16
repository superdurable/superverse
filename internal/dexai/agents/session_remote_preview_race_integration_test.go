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
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"google.golang.org/protobuf/proto"
)

// This is an actual held Session Step, not a manual old-request substitute.
// Genuine request deadlines/retries continue while the same execution is held.
// The second real source gate/admission must finish within the2m gate-wide budget.
func TestSessionRemoteHeldPreviewBranchHTTPDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+2*previewDeployTimeout)
	defer cancel()
	cleanup := newRemotePreviewCleanup(t)
	session, driver := newRemotePreviewRace(ctx, t)
	driver.call(ctx, conversation.ToolWrite, map[string]any{"path": "raexec-preview-race.md", "content": "first immutable push\n"})
	driver.call(ctx, conversation.ToolBash, map[string]any{"command": "make bootstrap", "description": "Restore application dependencies", "timeout_seconds": 600})
	stepType := dex.GetFinalStepType(sessionagent.DeployPreviewHandoff{})
	gate := installRemotePreviewRelay(ctx, t, session, stepType, cleanup)
	firstID := driver.call(ctx, conversation.ToolCommitAndPush, map[string]any{"summary": "First held remote Preview push", "verification": []string{"raexec-preview-race.md holds first immutable push"}})
	held := gate.await(ctx, t)
	first := remotePushedRequest(ctx, t, session, firstID)
	firstIdentity := session.identity
	firstIdentity.HeadCommit = first.Commit
	if held.handoff.Commit != first.Commit || int64(held.handoff.Sequence) != first.Sequence || held.handoff.WorkspaceRequestID != first.WorkspaceRequestID || held.handoff.Session == nil || *held.handoff.Session != firstIdentity || held.handoff.PublishedSequence != held.handoff.Sequence {
		t.Fatal("the held real Step differs from its original committed push identity")
	}
	race, stopRace := context.WithDeadline(ctx, held.at.Add(remotePreviewGateBudget))
	defer stopRace()
	driver.call(race, conversation.ToolWrite, map[string]any{"path": "raexec-preview-race.md", "content": "newer immutable push\n"})
	secondID := driver.call(race, conversation.ToolCommitAndPush, map[string]any{"summary": "Newer remote Preview push", "verification": []string{"raexec-preview-race.md holds newer immutable push"}})
	second := remotePushedRequest(race, t, session, secondID)
	if first.Commit == second.Commit || second.Sequence <= first.Sequence || second.WorkspaceRequestID != first.WorkspaceRequestID {
		t.Fatal("the newer actual push lacks a distinct increasing identity")
	}
	session.await(race, "newer push admitted while original Step remains held", func() bool {
		gate.requireHealthy(t)
		state, err := session.previews.Get(race, session.identity.ProjectID)
		if err != nil {
			t.Fatal("actual Preview admission read failed during bounded race")
		}
		if state.ActiveCommand.ID == deployCommandID(first.Sequence) || state.LastCommand.ID == deployCommandID(first.Sequence) || state.Deployment.HandledSequence == first.Sequence || state.Deployment.Last.Sequence == first.Sequence || state.Deployment.Queued != nil && state.Deployment.Queued.Sequence == first.Sequence {
			t.Fatal("the held original Step already reached Preview admission")
		}
		if queued := state.Deployment.Queued; queued != nil && queued.Sequence == second.Sequence {
			if queued.DeploymentRequest != second {
				t.Fatal("newer queued admission changed its original immutable request")
			}
			return true
		}
		if state.Deployment.HandledSequence == second.Sequence {
			if state.Deployment.Last.Sequence != second.Sequence || state.Deployment.Last.CommandID != deployCommandID(second.Sequence) || state.Deployment.Last.Commit != second.Commit {
				t.Fatal("newer accepted admission changed its original immutable request")
			}
			return true
		}
		return false
	})
	gate.requireHealthy(t)
	if race.Err() != nil || time.Since(held.at) >= remotePreviewGateBudget {
		t.Fatal("the second source gate/admission exceeded the gate-wide2m budget")
	}
	gate.open()
	name, number, valid := agentloop.StepExecutionParts(held.executionID)
	if !valid || name != stepType {
		t.Fatal("held Step completion lacks its exact execution identity")
	}
	// Allow the real maximum30s retry backoff plus a45s attempt and transport
	// margin. This is a bounded observation, not a guarantee of retry capacity:
	// the production3m total remains authoritative and success is proved below.
	completion, stopCompletion := context.WithTimeout(ctx, 90*time.Second)
	defer stopCompletion()
	if _, err := session.reader.WaitForStepCompletion(completion, &dexpb.WaitForStepCompletionRequest{FlowId: string(session.id), StepType: name, StepExecutionNumber: number, RequestTimeoutSeconds: 90}); err != nil {
		t.Fatal("the released original Step did not complete through the real Worker")
	}
	deployed := session.awaitDeployment(ctx, func(state preview.Snapshot) bool { return state.Deployment.Last.Sequence == second.Sequence })
	if deployed.Deployment.Last.Status != preview.DeploymentReady || deployed.Deployment.Last.CommandID != deployCommandID(second.Sequence) || deployed.Deployment.Last.Commit != second.Commit || deployed.Deployment.HandledSequence != second.Sequence || deployed.Preview.SourceCommit != second.Commit || deployed.LastCommand.ID != deployCommandID(second.Sequence) || deployed.LastCommand.Commit != second.Commit || deployed.LastCommand.WorkspaceRequestID != second.WorkspaceRequestID {
		t.Fatal("actual Preview owner did not deploy the newer pinned request once on its captured workspace")
	}
	for retry := 0; retry < 2; retry++ {
		answer, err := session.previews.RequestDeployment(ctx, session.identity.ProjectID, second)
		if err != nil || answer != preview.AnswerDuplicate {
			t.Fatal("a retry of the newer actual request was not duplicate")
		}
	}
	gate.requireHealthy(t)
	attempts, maxAttempt, upstreamOpens := gate.observedAttempts()
	t.Logf("Preview gate FlowID=%s RunID=%s StepExecutionID=%s observedAttempts=%v maximumAttempt=%d realUpstreamOpens=%d", session.id, gate.runID, held.executionID, attempts, maxAttempt, upstreamOpens)
	// One bounded diagnostic checkpoint after completion; never history polling.
	requireRemotePreviewRaceHistory(ctx, t, session, gate.runID, held, maxAttempt, first, second)
	driver.complete(ctx)
	session.stopPreview(ctx)
}

type remotePreviewDriver struct {
	t        *testing.T
	remote   remoteHTTPClient
	active   bridgeapi.ActivateResponse
	sequence uint64
}

func newRemotePreviewRace(ctx context.Context, t *testing.T) (*mockSession, *remotePreviewDriver) {
	t.Helper()
	session, api, caller := remoteSessionAPI(ctx, t)
	session.send(ctx, mockInput(t, "inspect-basic", 1).Content)
	session.finishedTurn(ctx)
	session.preparePreview(ctx)
	session.configurePreview(ctx)
	driver := &remotePreviewDriver{t: t, remote: pairRemoteHTTP(ctx, t, api, caller, session.identity.ProjectID)}
	driver.remote.post(ctx, bridgeapi.PathActivate, bridgeapi.ActivateRequest{Binding: bridgeapi.Binding{OrcaVersion: "integration", CodexVersion: "integration", OrcaSessionID: "held-preview-race", ProviderThreadID: "held-preview-race", HostLabel: "integration host"}}, &driver.active, http.StatusOK)
	return session, driver
}

func (driver *remotePreviewDriver) call(ctx context.Context, name conversation.ToolName, arguments any) conversation.CallID {
	driver.t.Helper()
	encoded, err := json.Marshal(arguments)
	if err != nil {
		driver.t.Fatal(err)
	}
	driver.sequence++
	var accepted bridgeapi.ReportResponse
	driver.remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: driver.active.Epoch, Generation: driver.active.Generation, Sequence: driver.sequence, ToolCall: &bridgeapi.ToolCall{Name: string(name), Arguments: encoded}}, &accepted, http.StatusOK)
	if accepted.Duplicate || accepted.Reported != driver.sequence {
		driver.t.Fatal("Preview race report lacks its original admission")
	}
	id := conversation.CallID(fmt.Sprintf("remote-%d-%d", driver.active.Epoch, driver.sequence))
	driver.remote.awaitResult(ctx, driver.active.Epoch, id, "")
	return id
}

func (driver *remotePreviewDriver) complete(ctx context.Context) {
	driver.sequence++
	var accepted bridgeapi.ReportResponse
	driver.remote.post(ctx, bridgeapi.PathReport, bridgeapi.Report{Epoch: driver.active.Epoch, Generation: driver.active.Generation, Sequence: driver.sequence, TurnCompleted: true}, &accepted, http.StatusOK)
}

func remotePreviewRunHistory(ctx context.Context, t *testing.T, reader dexpb.FlowServiceClient, flowID, runID string) []*dexpb.FlowHistoryEvent {
	t.Helper()
	summary, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil || summary.GetFlowExecutionId().GetFlowId() != flowID || summary.GetFlowExecutionId().GetRunId() != runID || summary.GetFirstRunId() != runID {
		t.Fatal("Preview race checkpoint lacks an unchanged untruncated exact run")
	}
	var events []*dexpb.FlowHistoryEvent
	var token []byte
	cursor, previous, bytes := int64(1), int64(0), 0
	for pages := 0; ; pages++ {
		if pages >= 64 {
			t.Fatal("Preview race diagnostic exceeded64 pages")
		}
		page, err := reader.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{FlowId: flowID, RunId: runID, StartInternalEventId: cursor, EstimatePageSize: 100, NextPageToken: token})
		if err != nil || page == nil || proto.Size(page) > 4<<20 {
			t.Fatal("Preview race diagnostic page failed or exceeded4MiB")
		}
		bytes += proto.Size(page)
		if bytes > 32<<20 || len(events)+len(page.GetEvents()) > 10000 {
			t.Fatal("Preview race diagnostic exceeded its byte/event bound")
		}
		for _, event := range page.GetEvents() {
			if event == nil || event.GetPayload() == nil || event.GetEventId() <= previous || len(event.ProtoReflect().GetUnknown()) != 0 {
				t.Fatal("Preview race diagnostic is unsupported or not ordered")
			}
			if previous == 0 {
				start := event.GetFlowStartedOrContinued()
				if start.GetFlowExecutionId().GetFlowId() != flowID || start.GetFlowExecutionId().GetRunId() != runID || start.GetInitialStart() == nil {
					t.Fatal("Preview race diagnostic lacks its actual initial run identity")
				}
			}
			previous = event.GetEventId()
		}
		events = append(events, page.GetEvents()...)
		if len(page.GetNextPageToken()) == 0 {
			break
		}
		if page.GetNextInternalEventId() <= cursor {
			t.Fatal("Preview race diagnostic cursor did not advance")
		}
		cursor, token = page.GetNextInternalEventId(), slices.Clone(page.GetNextPageToken())
	}
	if len(events) == 0 || currentRun(t, ctx, reader, flowID) != runID {
		t.Fatal("Preview race diagnostic ended empty or changed run")
	}
	return events
}

func requireRemotePreviewRaceHistory(ctx context.Context, t *testing.T, session *mockSession, runID string, held remotePreviewHeld, maxAttempt int32, first, second preview.DeploymentRequest) {
	t.Helper()
	checkpoint, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ctx = checkpoint
	inputs, originalCompletions := 0, 0
	for _, event := range remotePreviewRunHistory(ctx, t, session.reader, string(session.id), runID) {
		context := remotePreviewMethodContext(t, event)
		if context.GetStepType() == dex.GetFinalStepType(sessionagent.AbandonPreviewHandoff{}) {
			t.Fatal("a Preview handoff reached AbandonPreviewHandoff instead of successful Execute")
		}
		// Pinned synchronous Execute failure history is terminal, not the
		// ordinary retry telemetry. The latter is not read as history here.
		if failed := event.GetStepExecuteFailed(); failed != nil && failed.GetContext().GetStepExecutionId() == held.executionID {
			t.Fatal("the held original synchronous Execute ended in terminal failure")
		}
		completed := event.GetStepExecuteCompleted()
		if completed == nil || completed.GetContext().GetStepType() != dex.GetFinalStepType(sessionagent.DeployPreviewHandoff{}) {
			continue
		}
		var handoff agentloop.PreviewHandoff
		remoteDiagnosticJSON(ctx, t, session.reader, string(session.id), completed.GetInput().GetStepInput(), &handoff)
		if handoff.Commit == second.Commit && int64(handoff.Sequence) == second.Sequence {
			secondIdentity := session.identity
			secondIdentity.HeadCommit = second.Commit
			if handoff.WorkspaceRequestID != second.WorkspaceRequestID || handoff.Session == nil || *handoff.Session != secondIdentity || handoff.PublishedSequence != handoff.Sequence {
				t.Fatal("the actual newer Step input changed its captured publication identity")
			}
			inputs++
		}
		if completed.GetContext().GetStepExecutionId() == held.executionID {
			output := completed.GetOutput()
			deadEnd := &dexpb.StepDecision{CloseDecision: &dexpb.CloseDecision{CloseDecisionType: dexpb.CloseDecisionType_CLOSE_DECISION_TYPE_DEAD_END}}
			if !sameRemotePreviewHandoff(held.handoff, handoff) || !proto.Equal(output.GetStepDecision(), deadEnd) || len(output.GetUpsertAttributes()) != 0 || len(output.GetPublishToChannel()) != 0 || len(output.GetDeleteFromChannel()) != 0 || len(output.GetUpsertStepExecutionLocals()) != 0 || len(output.GetRecordEvents()) != 0 || maxAttempt < 1 || completed.GetContext().GetFinalAttempt() != maxAttempt {
				t.Fatal("the held old Step lacks exact successful dead-end completion, immutable input, final attempt or absence of Session writes")
			}
			originalCompletions++
		}
	}
	if inputs != 1 || originalCompletions != 1 {
		t.Fatal("the actual Session lacks exactly one old completion and newer captured Step input")
	}
	projectFlow := preview.FlowID(session.identity.ProjectID)
	previewRun := currentRun(t, ctx, session.reader, projectFlow)
	oldRequests, newerRequests, newerDuplicates, newerAdmissions, admittedCommands := 0, 0, 0, 0, 0
	newerDeploymentReady := false
	for _, event := range remotePreviewRunHistory(ctx, t, session.reader, projectFlow, previewRun) {
		var publishedMessages []*dexpb.ChannelMessage
		var writes []*dexpb.AttributeWrite
		if rpc := event.GetRpcExecutionCompleted(); rpc != nil {
			publishedMessages = rpc.GetPublishToChannel()
			writes = rpc.GetUpsertAttributes()
			if rpc.GetRpcName() == "RequestPreviewDeployment" {
				var request preview.DeploymentRequest
				var answer preview.DeploymentAnswer
				remoteDiagnosticJSON(ctx, t, session.reader, projectFlow, rpc.GetInput(), &request)
				remoteDiagnosticJSON(ctx, t, session.reader, projectFlow, rpc.GetOutput(), &answer)
				switch request.Sequence {
				case first.Sequence:
					if request != first || answer != preview.AnswerDuplicate || len(rpc.GetPublishToChannel()) != 0 || len(rpc.GetUpsertAttributes()) != 0 || len(rpc.GetDeleteFromChannel()) != 0 || len(rpc.GetRecordEvents()) != 0 || rpc.GetStepDecision() != nil {
						t.Fatal("the released old actual request was not side-effect-free duplicate")
					}
					oldRequests++
				case second.Sequence:
					if request != second || answer != preview.AnswerAccepted && answer != preview.AnswerQueued && answer != preview.AnswerDuplicate {
						t.Fatal("the newer actual admission changed its pinned triple")
					}
					newerRequests++
					if answer == preview.AnswerDuplicate {
						newerDuplicates++
					} else if len(rpc.GetUpsertAttributes()) > 0 {
						// A queued request retry can still answer QUEUED without a
						// mutation. Count the one effective admission, not RPCs.
						newerAdmissions++
					}
				}
			}
		}
		if step := event.GetStepExecuteCompleted(); step != nil {
			publishedMessages = step.GetOutput().GetPublishToChannel()
			writes = step.GetOutput().GetUpsertAttributes()
		}
		if wait := event.GetStepWaitForCompleted(); wait != nil {
			publishedMessages = wait.GetOutput().GetPublishToChannel()
			writes = wait.GetOutput().GetUpsertAttributes()
		}
		if external := event.GetChannelExternalPublish(); external != nil {
			publishedMessages = append(publishedMessages, external.GetMessages()...)
		}
		for _, write := range writes {
			if write.GetKey() != "PreviewDeployment" {
				continue
			}
			var deployment preview.PreviewDeployment
			remoteDiagnosticJSON(ctx, t, session.reader, projectFlow, write.GetValue(), &deployment)
			if deployment.HandledSequence == first.Sequence || deployment.Last.Sequence == first.Sequence || deployment.Queued != nil && deployment.Queued.Sequence == first.Sequence {
				t.Fatal("the held original request wrote an old deployment record")
			}
			if deployment.Last.Sequence == second.Sequence {
				if deployment.Last.Commit != second.Commit || deployment.Last.CommandID != deployCommandID(second.Sequence) {
					t.Fatal("a newer deployment record changed its immutable identity")
				}
				newerDeploymentReady = newerDeploymentReady || deployment.Last.Status == preview.DeploymentReady
			}
			if queued := deployment.Queued; queued != nil && queued.Sequence == second.Sequence && queued.DeploymentRequest != second {
				t.Fatal("a newer queued deployment record changed its immutable identity")
			}
		}
		for _, published := range publishedMessages {
			if published.GetChannelName() == "PreviewCommands" {
				var command preview.Command
				remoteDiagnosticJSON(ctx, t, session.reader, projectFlow, published.GetValue(), &command)
				if command.ID == deployCommandID(first.Sequence) {
					t.Fatal("the held original request created a Preview command")
				}
				if command.ID == deployCommandID(second.Sequence) {
					if command.Commit != second.Commit || command.WorkspaceRequestID != second.WorkspaceRequestID {
						t.Fatal("the newer actual command changed its captured publication triple")
					}
					admittedCommands++
				}
			}
		}
	}
	if oldRequests < 1 || newerRequests < 3 || newerDuplicates < 2 || newerAdmissions != 1 || admittedCommands != 1 || !newerDeploymentReady {
		t.Fatal("Preview history lacks old duplicate owner calls, one effective newer admission/command/deployment and its two explicit harmless retries")
	}
	t.Logf("Preview owner FlowID=%s RunID=%s oldDuplicateRPCs=%d newerRPCs=%d newerDuplicateRPCs=%d effectiveNewerAdmissions=%d newerCommands=%d", projectFlow, previewRun, oldRequests, newerRequests, newerDuplicates, newerAdmissions, admittedCommands)
}

// Use the actual pinned history variants, including pending and failure
// contexts, so an Abandon branch cannot disappear behind completion-only reads.
func remotePreviewMethodContext(t *testing.T, event *dexpb.FlowHistoryEvent) *dexpb.StepMethodEventContext {
	t.Helper()
	var identity *dexpb.StepMethodEventContext
	switch payload := event.GetPayload().(type) {
	case *dexpb.FlowHistoryEvent_StepExecuteCompleted:
		identity = payload.StepExecuteCompleted.GetContext()
	case *dexpb.FlowHistoryEvent_StepExecuteFailed:
		identity = payload.StepExecuteFailed.GetContext()
	case *dexpb.FlowHistoryEvent_StepWaitForCompleted:
		identity = payload.StepWaitForCompleted.GetContext()
	case *dexpb.FlowHistoryEvent_StepWaitForFailed:
		identity = payload.StepWaitForFailed.GetContext()
	case *dexpb.FlowHistoryEvent_StepExecutePending:
		identity = payload.StepExecutePending.GetContext()
	case *dexpb.FlowHistoryEvent_StepWaitForPending:
		identity = payload.StepWaitForPending.GetContext()
	default:
		return nil
	}
	if identity == nil || identity.GetStepType() == "" || identity.GetStepExecutionId() == "" {
		t.Fatal("Preview race method history lacks its exact Step identity")
	}
	return identity
}
