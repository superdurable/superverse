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
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type remoteDiagnosticEvent struct {
	runID          string
	eventID        int64
	stepType       string
	executionID    string
	rpcName        string
	subagentWrite  bool
	waitRegistered bool
	completed      bool
	admitted       []string
	executorChange *remoteDiagnosticExecutorChange
}

type remoteDiagnosticExecutorChange struct {
	request remoteexec.SetRequest
	result  remoteexec.SetResult
}

type remoteModelCheckpoint struct {
	flowID, firstRunID, currentRunID string
	events                           []remoteDiagnosticEvent
	sequence                         conversation.Sequence
}

var remoteForbiddenModelSteps = []string{
	dex.GetFinalStepType(sessionagent.CallModel{}), dex.GetFinalStepType(sessionagent.CompactContext{}),
	dex.GetFinalStepType(sessionagent.RecordPlatformModelResult{}), dex.GetFinalStepType(sessionagent.RecordModelFailure{}),
	dex.GetFinalStepType(sessionagent.RecordCompactionFailure{}), dex.GetFinalStepType(sessionagent.ReportPlatformIssue{}),
}

// Diagnostics use a bounded state-only wait at explicit checkpoints, followed
// by fully paginated reads. History is never chat or polled for progress.
func (session *mockSession) remoteModelCheckpoint(ctx context.Context) remoteModelCheckpoint {
	t := session.t
	t.Helper()
	snapshot, err := session.client.GetSnapshot(ctx, session.id)
	if err != nil || snapshot.Agent == nil || snapshot.Agent.Description.Status != agentloop.AgentStatusWaitingForMessage || len(snapshot.Agent.Description.Subagents) != 0 {
		t.Fatal("model diagnostic checkpoint is not quiescent or has platform subagents")
	}
	var state *dexpb.GetFlowStateResponse
	waitID := ""
	session.await(ctx, "quiescent diagnostic AwaitUser wait", func() bool {
		var err error
		state, err = session.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: string(session.id)})
		if err != nil || state == nil || proto.Size(state) > 16<<20 {
			t.Fatal("model diagnostic state read failed or exceeds its bound")
		}
		for _, step := range state.GetActiveStepExecutions() {
			if step.GetStepType() == dex.GetFinalStepType(sessionagent.AwaitUser{}) && step.GetPhase() == dexpb.ActiveStepPhase_ACTIVE_STEP_PHASE_WAITING && step.GetWaitingCondition() != nil {
				waitID = step.GetStepExecutionId()
				return waitID != ""
			}
		}
		return false
	})

	for _, step := range state.GetActiveStepExecutions() {
		if step == nil || step.GetStepType() == "" || slices.Contains(remoteForbiddenModelSteps, step.GetStepType()) {
			t.Fatal("a model/compaction/result/retry/incident Step is active at the diagnostic checkpoint")
		}
	}
	for _, step := range state.GetQueuedSteps() {
		if step == nil || step.GetStepType() == "" || slices.Contains(remoteForbiddenModelSteps, step.GetStepType()) {
			t.Fatal("a model Step is queued at the diagnostic checkpoint")
		}
	}
	summary, err := session.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: string(session.id)})
	if err != nil || summary.GetFirstRunId() == "" || summary.GetFlowExecutionId().GetRunId() == "" || summary.GetFlowExecutionId().GetFlowId() != string(session.id) {
		t.Fatal("model diagnostic summary lacks exact Flow/Run identity")
	}
	checkpoint := remoteModelCheckpoint{flowID: string(session.id), firstRunID: summary.GetFirstRunId(), currentRunID: summary.GetFlowExecutionId().GetRunId(), sequence: snapshot.Agent.Description.LastSequence}
	var runs [][]remoteDiagnosticEvent
	seenRuns := map[string]bool{}
	runID, pages, bytes, total := checkpoint.currentRunID, 0, 0, 0
	for len(runs) < 32 {
		if runID == "" || seenRuns[runID] {
			t.Fatal("model diagnostic continuation chain is missing or cyclic")
		}
		seenRuns[runID] = true
		var events []remoteDiagnosticEvent
		var token []byte
		cursor, previousEventID, previousRunID := int64(1), int64(0), ""
		for {
			pages++
			if pages > 128 {
				t.Fatal("model diagnostic history exceeded128 pages")
			}
			page, err := session.reader.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{FlowId: checkpoint.flowID, RunId: runID, StartInternalEventId: cursor, EstimatePageSize: 200, NextPageToken: token})
			if err != nil || page == nil || proto.Size(page) > 16<<20 {
				t.Fatal("model diagnostic history page failed, is missing or exceeds16MiB")
			}
			bytes += proto.Size(page)
			total += len(page.GetEvents())
			if bytes > 64<<20 || total > 20000 {
				t.Fatal("model diagnostic history exceeded its total byte/event bound")
			}
			for _, event := range page.GetEvents() {
				if event == nil || event.GetEventId() <= previousEventID || len(event.ProtoReflect().GetUnknown()) != 0 {
					t.Fatal("model diagnostic event ordering or supported format is unverifiable")
				}
				if previousEventID == 0 {
					start := event.GetFlowStartedOrContinued()
					if start == nil || start.GetFlowExecutionId().GetFlowId() != checkpoint.flowID || start.GetFlowExecutionId().GetRunId() != runID {
						t.Fatal("model diagnostic run lacks its untruncated identity/start event")
					}
					if continued := start.GetContinuedStart(); continued != nil {
						previousRunID = continued.GetPreviousRunId()
					} else if start.GetInitialStart() == nil || runID != checkpoint.firstRunID {
						t.Fatal("model diagnostic initial/continued start is unsupported")
					}
				}
				previousEventID = event.GetEventId()
				events = append(events, remoteModelEvent(ctx, t, session.reader, checkpoint.flowID, runID, event))
			}
			if len(page.GetNextPageToken()) == 0 {
				break
			}
			if page.GetNextInternalEventId() <= cursor {
				t.Fatal("model diagnostic cursor did not advance")
			}
			cursor, token = page.GetNextInternalEventId(), slices.Clone(page.GetNextPageToken())
		}
		if len(events) == 0 {
			t.Fatal("empty model diagnostic history is not proof of zero work")
		}
		runs = append(runs, events)
		if runID == checkpoint.firstRunID {
			break
		}
		runID = previousRunID
	}
	if !seenRuns[checkpoint.firstRunID] {
		t.Fatal("model diagnostic history did not reach the first run within32 runs")
	}
	for index := len(runs) - 1; index >= 0; index-- {
		checkpoint.events = append(checkpoint.events, runs[index]...)
	}
	if currentRun(t, ctx, session.reader, checkpoint.flowID) != checkpoint.currentRunID {
		t.Fatal("model diagnostic run changed during its bounded read")
	}
	if !slices.ContainsFunc(checkpoint.events, func(event remoteDiagnosticEvent) bool {
		return event.runID == checkpoint.currentRunID && event.executionID == waitID && event.waitRegistered
	}) {
		t.Fatal("model diagnostic history tail lacks the current durable AwaitUser wait anchor")
	}
	t.Logf("Model diagnostic FlowID=%s FirstRunID=%s RunID=%s pages=%d events=%d; engine/source proof, not an HTTP counter", checkpoint.flowID, checkpoint.firstRunID, checkpoint.currentRunID, pages, total)
	return checkpoint
}

func remoteModelEvent(ctx context.Context, t *testing.T, reader dexpb.FlowServiceClient, flowID, runID string, event *dexpb.FlowHistoryEvent) remoteDiagnosticEvent {
	t.Helper()
	record := remoteDiagnosticEvent{runID: runID, eventID: event.GetEventId()}
	var context *dexpb.StepMethodEventContext
	var decision *dexpb.StepDecision
	var writes []*dexpb.AttributeWrite
	method := false
	switch payload := event.GetPayload().(type) {
	case *dexpb.FlowHistoryEvent_StepExecuteCompleted:
		method = true
		context, decision, record.completed = payload.StepExecuteCompleted.GetContext(), payload.StepExecuteCompleted.GetOutput().GetStepDecision(), true
		writes = payload.StepExecuteCompleted.GetOutput().GetUpsertAttributes()
	case *dexpb.FlowHistoryEvent_StepExecuteFailed:
		method = true
		context = payload.StepExecuteFailed.GetContext()
	case *dexpb.FlowHistoryEvent_StepWaitForCompleted:
		method = true
		context = payload.StepWaitForCompleted.GetContext()
		record.waitRegistered = payload.StepWaitForCompleted.GetOutput().GetWaitForCondition() != nil
		writes = payload.StepWaitForCompleted.GetOutput().GetUpsertAttributes()
	case *dexpb.FlowHistoryEvent_StepWaitForFailed:
		method = true
		context = payload.StepWaitForFailed.GetContext()
	case *dexpb.FlowHistoryEvent_StepExecutePending:
		method = true
		context = payload.StepExecutePending.GetContext()
	case *dexpb.FlowHistoryEvent_StepWaitForPending:
		method = true
		context = payload.StepWaitForPending.GetContext()
	case *dexpb.FlowHistoryEvent_RpcExecutionCompleted:
		decision = payload.RpcExecutionCompleted.GetStepDecision()
		record.rpcName = payload.RpcExecutionCompleted.GetRpcName()
		if record.rpcName == "" {
			t.Fatal("model diagnostic RPC event lacks its owner identity")
		}
		writes = payload.RpcExecutionCompleted.GetUpsertAttributes()
		if record.rpcName == "SetExecutor" {
			change := &remoteDiagnosticExecutorChange{}
			remoteDiagnosticJSON(ctx, t, reader, flowID, payload.RpcExecutionCompleted.GetInput(), &change.request)
			remoteDiagnosticJSON(ctx, t, reader, flowID, payload.RpcExecutionCompleted.GetOutput(), &change.result)
			record.executorChange = change
		}
	case *dexpb.FlowHistoryEvent_FlowStartedOrContinued:
		start := payload.FlowStartedOrContinued
		if initial := start.GetInitialStart(); initial != nil {
			record.admitted = append(record.admitted, initial.GetStartStepType())
		} else if continued := start.GetContinuedStart(); continued != nil {
			for _, step := range continued.GetStepsToStart() {
				record.admitted = append(record.admitted, step.GetStepType())
			}
			for _, step := range continued.GetStepsToResume() {
				record.admitted = append(record.admitted, step.GetStep().GetStepType())
			}
		} else {
			t.Fatal("model diagnostic start event is unsupported")
		}
	case *dexpb.FlowHistoryEvent_FlowClosed, *dexpb.FlowHistoryEvent_ChannelExternalPublish, *dexpb.FlowHistoryEvent_ChannelExternalDelete:
		// These supported events schedule no Step and invoke no provider.
	default:
		t.Fatal("model diagnostic encountered an unsupported event, including time travel")
	}
	if method && context == nil {
		t.Fatal("model diagnostic method event lacks its original context")
	}
	if context != nil {
		if context.GetStepType() == "" || context.GetStepExecutionId() == "" {
			t.Fatal("model diagnostic method event lacks execution identity")
		}
		record.stepType, record.executionID = context.GetStepType(), context.GetStepExecutionId()
	}
	for _, write := range writes {
		if write == nil || write.GetKey() == "" {
			t.Fatal("model diagnostic Attribute write lacks its owner key")
		}
		if strings.HasPrefix(write.GetKey(), "Subagents/") {
			record.subagentWrite = true
		}
	}
	for _, next := range decision.GetNextSteps() {
		if next.GetStepType() == "" {
			t.Fatal("model diagnostic movement lacks Step identity")
		}
		record.admitted = append(record.admitted, next.GetStepType())
	}
	return record
}

func (after remoteModelCheckpoint) since(t *testing.T, before remoteModelCheckpoint) []remoteDiagnosticEvent {
	t.Helper()
	if after.flowID != before.flowID || after.firstRunID != before.firstRunID || len(before.events) == 0 {
		t.Fatal("model diagnostic checkpoints do not identify the same untruncated Flow")
	}
	last := before.events[len(before.events)-1]
	for index, event := range after.events {
		if event.runID == last.runID && event.eventID == last.eventID {
			return after.events[index+1:]
		}
	}
	t.Fatal("model diagnostic checkpoint cursor disappeared from retained engine history")
	return nil
}

func requireRemoteModelInterval(t *testing.T, after, before remoteModelCheckpoint) {
	t.Helper()
	events := after.since(t, before)
	if len(events) == 0 {
		t.Fatal("remote interval has no diagnostic events to substantiate its coverage")
	}
	for _, event := range events {
		if event.rpcName == "AdmitSubagent" || event.subagentWrite {
			t.Fatalf("remote interval has subagent admission/record evidence at %s/%d", event.runID, event.eventID)
		}
		for _, step := range append([]string{event.stepType}, event.admitted...) {
			if slices.Contains(remoteForbiddenModelSteps, step) {
				t.Fatalf("remote interval admitted/executed model or incident work: %s at %s/%d", step, event.runID, event.eventID)
			}
		}
	}
}

func requireModelPositiveControl(t *testing.T, events []remoteDiagnosticEvent) {
	t.Helper()
	for _, event := range events {
		if event.completed && event.stepType == dex.GetFinalStepType(sessionagent.CallModel{}) {
			return
		}
	}
	t.Fatal("bounded diagnostic positive control did not observe actual CallModel completion")
}

// Load only the selected typed object's reference, never the whole history.
// Missing, oversized or unsupported payloads fail instead of guessing a fence.
func remoteDiagnosticJSON(ctx context.Context, t *testing.T, reader dexpb.FlowServiceClient, flowID string, value *dexpb.Value, target any) {
	t.Helper()
	if err := remoteBoundedJSON(ctx, reader, flowID, value, target); err != nil {
		t.Fatal(err)
	}
}

// The relay calls this on a gRPC goroutine, so it returns errors to the owning
// test instead of invoking testing.Fatal outside the test goroutine.
func remoteBoundedJSON(ctx context.Context, reader dexpb.FlowServiceClient, flowID string, value *dexpb.Value, target any) error {
	if reference, ok := value.GetKind().(*dexpb.Value_InternalBlobIdForObjValue); ok {
		if flowID == "" || reference.InternalBlobIdForObjValue == "" {
			return errors.New("diagnostic object reference lacks its exact Flow or blob identity")
		}
		load, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		loaded, err := reader.LoadBlobs(load, &dexpb.LoadBlobsRequest{Entries: []*dexpb.LoadBlobRequestEntry{{FlowId: flowID, BlobValue: value}}}, grpc.MaxCallRecvMsgSize(20<<10))
		if err != nil || len(loaded.GetValues()) != 1 {
			return errors.New("selected diagnostic object did not load within its byte/time bound")
		}
		value = loaded.GetValues()[reference.InternalBlobIdForObjValue]
	}
	object := value.GetObjValue()
	if object == nil || object.GetEncoding() != "json" || len(object.GetPayload()) > 16<<10 || json.Unmarshal(object.GetPayload(), target) != nil {
		return errors.New("executor diagnostic commit lacks its supported bounded typed payload")
	}
	return nil
}

func requireRemoteExecutorCommits(t *testing.T, after, before remoteModelCheckpoint, epoch, generation uint64, switched bool) {
	t.Helper()
	var changes []*remoteDiagnosticExecutorChange
	for _, event := range after.since(t, before) {
		if event.executorChange != nil {
			changes = append(changes, event.executorChange)
		}
	}
	wanted := 1
	if switched {
		wanted++
	}
	if len(changes) != wanted {
		t.Fatal("bounded diagnostic interval lacks the exact activation/switch commits")
	}
	activation := changes[0]
	if !activation.result.Accepted || !activation.result.Changed || activation.request.Mode != remoteexec.ModeRemote || activation.request.BasisEpoch+1 != epoch || activation.result.Executor.Mode != remoteexec.ModeRemote || activation.result.Executor.Epoch != epoch || activation.result.Executor.Generation != generation || activation.result.Executor.Remote == nil || activation.result.Executor.Remote.ConnectionID != activation.request.ConnectionID {
		t.Fatal("diagnostic activation commit does not match the actual admitted authority")
	}
	if switched {
		change := changes[1]
		if !change.result.Accepted || !change.result.Changed || change.request.ProjectID != activation.request.ProjectID || change.request.Mode != remoteexec.ModePlatform || change.request.BasisEpoch != epoch || change.result.Executor.Mode != remoteexec.ModePlatform || change.result.Executor.Epoch != epoch+1 || change.result.Executor.Generation != generation+1 || change.result.Executor.Remote != nil {
			t.Fatal("diagnostic switch commit does not follow the exact activation fence")
		}
	}
}
