// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package contribution

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// ciHarnessFlow replaces only the first admission Step, installing a known
// test record. Dex, the Worker, the production CI Steps, their options, the
// update and close RPCs, and the connector library are real. Test RPCs stage
// an observed CI fact without substituting a GitHub server.
type ciHarnessFlow struct{ *ConnectorContributionFlow }

func (flow *ciHarnessFlow) GetSteps() []dex.StepDef {
	steps := []dex.StepDef{dex.DefineStartStep(startCIHarness{})}
	return append(steps, flow.ConnectorContributionFlow.GetSteps()[1:]...)
}

func (flow *ciHarnessFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.UpdateConnectorContribution, dexflow.ShortRPC(statusLock)),
		dex.DefineRPC(flow.CloseConnectorContribution, dexflow.ShortRPC(statusLock)),
		dex.DefineRPC(flow.GetConnectorContributionSnapshot, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.GetDexSummary, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.GetDexDisplay, &dex.RPCOptions{Timeout: readRPCTimeout}),
		dex.DefineRPC(flow.StageChecksForTest, dexflow.ShortRPC(statusLock)),
		dex.DefineRPC(flow.StartCheckWaitForTest, dexflow.ShortRPC(statusLock)),
		dex.DefineRPC(flow.StageCandidateForTest, dexflow.ShortRPC(statusLock)),
		dex.DefineRPC(flow.SetClosingForTest, dexflow.ShortRPC(statusLock)),
	}
}

func (flow *ciHarnessFlow) UpdateConnectorContribution(ctx dex.Context, input Submission) (*dex.RPCResult[bool], error) {
	return flow.ConnectorContributionFlow.UpdateConnectorContribution(ctx, input)
}

func (flow *ciHarnessFlow) CloseConnectorContribution(ctx dex.Context, input dex.None) (*dex.RPCResult[bool], error) {
	return flow.ConnectorContributionFlow.CloseConnectorContribution(ctx, input)
}

func (flow *ciHarnessFlow) GetConnectorContributionSnapshot(ctx dex.Context, input dex.None) (*dex.RPCResult[Snapshot], error) {
	return flow.ConnectorContributionFlow.GetConnectorContributionSnapshot(ctx, input)
}

func (flow *ciHarnessFlow) GetDexSummary(ctx dex.Context, input dex.None) (*dex.RPCResult[map[string]any], error) {
	return flow.ConnectorContributionFlow.GetDexSummary(ctx, input)
}

func (flow *ciHarnessFlow) GetDexDisplay(ctx dex.Context, input dex.None) (*dex.RPCResult[map[string]any], error) {
	return flow.ConnectorContributionFlow.GetDexDisplay(ctx, input)
}

type startCIHarness struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (startCIHarness) GetStepOptions() *dex.StepOptions { return recordStepOptions }

func (startCIHarness) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	submission := comm.Must(submissionAttribute.Get(ctx))
	record := submission.record(StatusUnpublished, time.Now().UTC())
	comm.MustDo(recordAttribute.Set(ctx, record))
	comm.MustDo(statusAttribute.Set(ctx, StatusUnpublished.String()))
	return dex.GoToMany(dex.MovementOf(AwaitConnectorContributionEnd{}, nil),
		dex.MovementOf(WaitConnectorChecks{}, checksAttempt{Head: record.HeadCommit, CheckAt: record.HeadRecordedAt.Add(connectorCheckFirstDelay), Number: 1})), nil
}

func (*ciHarnessFlow) StageChecksForTest(_ dex.Context, checks checksAttempt) (*dex.RPCResult[bool], error) {
	return &dex.RPCResult[bool]{Output: true, NextSteps: []dex.StepMovement{dex.MovementOf(RecordConnectorChecks{}, checks)}}, nil
}

func (*ciHarnessFlow) StartCheckWaitForTest(_ dex.Context, head string) (*dex.RPCResult[bool], error) {
	return &dex.RPCResult[bool]{Output: true, NextSteps: []dex.StepMovement{dex.MovementOf(WaitConnectorChecks{}, checksAttempt{Head: head, CheckAt: time.Now().Add(connectorCheckFirstDelay), Number: 1})}}, nil
}

func (*ciHarnessFlow) StageCandidateForTest(_ dex.Context, attempt candidateAttempt) (*dex.RPCResult[bool], error) {
	return &dex.RPCResult[bool]{Output: true, NextSteps: []dex.StepMovement{dex.MovementOf(RecordReleaseCandidate{}, attempt)}}, nil
}

func (*ciHarnessFlow) SetClosingForTest(ctx dex.Context, closing bool) (*dex.RPCResult[bool], error) {
	comm.MustDo(closingAttribute.Set(ctx, closing))
	return &dex.RPCResult[bool]{Output: true}, nil
}

// TestConnectorCheckHeadAndCloseFenceDexIntegration proves a newer head and
// Project close cancel an active durable timer Step. A late result for the old
// head cannot replace the current record or schedule a notification.
func TestConnectorCheckHeadAndCloseFenceDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	host := dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST")
	library := connectorlibrary.New(nil, nil, deploymentnames.HostedRepositoryPrefix(deploymentnames.EnvironmentTest))
	flow := &ciHarnessFlow{ConnectorContributionFlow: &ConnectorContributionFlow{pullRequests: library, releaseCandidates: library, checks: library}}
	sdk := dexflowtest.WorkerClientForTest(ctx, t, address, host, flow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	id := Identity{ProjectID: "ci" + strings.ToLower(rand.Text()[:8]), Slug: "mockci"}
	first, next := strings.Repeat("a", 40), strings.Repeat("b", 40)
	submission := Submission{Identity: id, Report: "CI fence test", Contribution: conversation.ConnectorContribution{
		Slug: id.Slug, Module: connectorlibrary.ModulePath + "/connectors/mockci", Kind: conversation.ContributionFix,
		Branch: "superverse/test/" + id.ProjectID + "/" + id.Slug, HeadCommit: first,
		Location: connectorlibrary.LocationSandbox, Summary: "Check the CI fence"}}
	requestID := submission.requestID()
	runID, err := sdk.StartFlow(ctx, flow, id.flowID(), nil, dex.StartFlowOptions{RequestID: &requestID,
		IDReusePolicy: dex.IDReuseDisallow, Attributes: startAttributes(submission)})
	if err != nil {
		t.Fatal(err)
	}
	awaitCheckStep(t, ctx, reader, id.flowID(), true)
	later := submission
	later.Contribution.HeadCommit = next
	accepted, err := dexflow.InvokeLocked[bool](ctx, sdk, commandLockWait, id.flowID(), flow.UpdateConnectorContribution, later)
	if err != nil || !accepted {
		t.Fatalf("later head was not admitted: %t, %v", accepted, err)
	}
	awaitCheckStep(t, ctx, reader, id.flowID(), false)
	late := checksAttempt{Head: first, Checks: connectorlibrary.HeadChecks{Head: first, State: connectorlibrary.HeadChecksFailed,
		Jobs: []string{"dex-compat-current"}, UpdatedAt: time.Now().UTC()}}
	if _, err = dexflow.InvokeLocked[bool](ctx, sdk, commandLockWait, id.flowID(), flow.StageChecksForTest, late); err != nil {
		t.Fatal(err)
	}
	awaitRecordedStepCompleted(t, ctx, reader, id.flowID(), runID)
	awaitRecordedHead(t, ctx, sdk, flow, id, next, false)
	if _, err = dexflow.InvokeLocked[bool](ctx, sdk, commandLockWait, id.flowID(), flow.StartCheckWaitForTest, next); err != nil {
		t.Fatal(err)
	}
	awaitCheckStep(t, ctx, reader, id.flowID(), true)
	if _, err = dexflow.InvokeLocked[bool](ctx, sdk, commandLockWait, id.flowID(), flow.CloseConnectorContribution, nil); err != nil {
		t.Fatal(err)
	}
	awaitCheckStep(t, ctx, reader, id.flowID(), false)
	result, err := sdk.WaitForFlow(ctx, id.flowID(), dex.WaitForFlowOptions{NeedsResults: false})
	if err != nil || result.Status != dex.FlowCompleted {
		t.Fatalf("closing the contribution did not complete its Flow: %+v, %v", result, err)
	}
}

// TestReleaseCandidateHeadAndCloseFenceDexIntegration stages an observed
// external workflow failure through test RPCs while the real Dex Worker runs
// the production record Step. Neither an older head nor Closing can retain it
// or schedule its notification.
func TestReleaseCandidateHeadAndCloseFenceDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	host := dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST")
	library := connectorlibrary.New(nil, nil, deploymentnames.HostedRepositoryPrefix(deploymentnames.EnvironmentTest))
	flow := &ciHarnessFlow{ConnectorContributionFlow: &ConnectorContributionFlow{pullRequests: library, releaseCandidates: library, checks: library}}
	sdk := dexflowtest.WorkerClientForTest(ctx, t, address, host, flow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	id := Identity{ProjectID: "rc" + strings.ToLower(rand.Text()[:8]), Slug: "mockrc"}
	first, next := strings.Repeat("c", 40), strings.Repeat("d", 40)
	submission := Submission{Identity: id, Report: "RC fence test", Contribution: conversation.ConnectorContribution{
		Slug: id.Slug, Module: connectorlibrary.ModulePath + "/connectors/mockrc", Kind: conversation.ContributionFix,
		Branch: "superverse/test/" + id.ProjectID + "/" + id.Slug, HeadCommit: first,
		Location: connectorlibrary.LocationSandbox, Summary: "Check the RC fence"}}
	requestID := submission.requestID()
	runID, err := sdk.StartFlow(ctx, flow, id.flowID(), nil, dex.StartFlowOptions{RequestID: &requestID,
		IDReusePolicy: dex.IDReuseDisallow, Attributes: startAttributes(submission)})
	if err != nil {
		t.Fatal(err)
	}
	later := submission
	later.Contribution.HeadCommit = next
	if accepted, updateErr := dexflow.InvokeLocked[bool](ctx, sdk, commandLockWait, id.flowID(), flow.UpdateConnectorContribution, later); updateErr != nil || !accepted {
		t.Fatalf("later head was not admitted: %t, %v", accepted, updateErr)
	}
	stale := failedCandidateAttempt(first)
	if _, err = dexflow.InvokeLocked[bool](ctx, sdk, commandLockWait, id.flowID(), flow.StageCandidateForTest, stale); err != nil {
		t.Fatal(err)
	}
	awaitStepCompleted(t, ctx, reader, id.flowID(), runID, dex.GetFinalStepType(RecordReleaseCandidate{}), 1)
	awaitRecordedHead(t, ctx, sdk, flow, id, next, false)
	if snapshot, snapshotErr := snapshotForTest(ctx, sdk, flow, id); snapshotErr != nil || snapshot.Record.ReleaseCandidate != nil {
		t.Fatalf("the old head's candidate was retained: %+v, %v", snapshot.Record.ReleaseCandidate, snapshotErr)
	}
	if _, err = dexflow.InvokeLocked[bool](ctx, sdk, commandLockWait, id.flowID(), flow.SetClosingForTest, true); err != nil {
		t.Fatal(err)
	}
	if _, err = dexflow.InvokeLocked[bool](ctx, sdk, commandLockWait, id.flowID(), flow.StageCandidateForTest, failedCandidateAttempt(next)); err != nil {
		t.Fatal(err)
	}
	awaitStepCompleted(t, ctx, reader, id.flowID(), runID, dex.GetFinalStepType(RecordReleaseCandidate{}), 2)
	if snapshot, snapshotErr := snapshotForTest(ctx, sdk, flow, id); snapshotErr != nil || snapshot.Record.ReleaseCandidate != nil {
		t.Fatalf("Closing retained the candidate: %+v, %v", snapshot.Record.ReleaseCandidate, snapshotErr)
	}
	if _, err = dexflow.InvokeLocked[bool](ctx, sdk, commandLockWait, id.flowID(), flow.SetClosingForTest, false); err != nil {
		t.Fatal(err)
	}
	if _, err = dexflow.InvokeLocked[bool](ctx, sdk, commandLockWait, id.flowID(), flow.CloseConnectorContribution, nil); err != nil {
		t.Fatal(err)
	}
}

func failedCandidateAttempt(head string) candidateAttempt {
	return candidateAttempt{Candidate: connectorlibrary.ReleaseCandidate{Directory: "connectors/mockrc", Version: "v0.1.0-rc.1",
		Tag: "connectors/mockrc/v0.1.0-rc.1", Commit: head}, Location: connectorlibrary.LocationUpstream,
		FailureKind: ReleaseCandidateWorkflowFailed, Failure: "The connectors library release-candidate workflow failed.",
		Run: connectorlibrary.ReleaseCandidateRun{State: connectorlibrary.ReleaseCandidateRunFailed,
			Failure: connectorlibrary.ActionFailure{Jobs: []string{"verify"}, Step: "Verify compatibility", LogTail: "redacted tail"}}}
}

func snapshotForTest(ctx context.Context, sdk *dex.Client, flow *ciHarnessFlow, id Identity) (Snapshot, error) {
	var snapshot Snapshot
	err := dexflow.InvokeRetainedRead(ctx, sdk, id.flowID(), flow.GetConnectorContributionSnapshot, nil, &snapshot)
	return snapshot, err
}

func awaitCheckStep(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flowID string, active bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		state, err := reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: flowID})
		if err == nil {
			found := false
			for _, step := range state.GetActiveStepExecutions() {
				found = found || step.GetStepType() == dex.GetFinalStepType(WaitConnectorChecks{})
			}
			if found == active {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the connector check wait did not become active=%t", active)
}

func awaitRecordedHead(t *testing.T, ctx context.Context, sdk *dex.Client, flow *ciHarnessFlow, id Identity, head string, withChecks bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var snapshot Snapshot
		err := dexflow.InvokeRetainedRead(ctx, sdk, id.flowID(), flow.GetConnectorContributionSnapshot, nil, &snapshot)
		if err == nil && snapshot.Record.HeadCommit == head && (snapshot.Record.Checks != nil) == withChecks {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the contribution did not retain head %s with checks=%t", head, withChecks)
}

func awaitRecordedStepCompleted(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flowID, runID string) {
	awaitStepCompleted(t, ctx, reader, flowID, runID, dex.GetFinalStepType(RecordConnectorChecks{}), 1)
}

func awaitStepCompleted(t *testing.T, ctx context.Context, reader dexpb.FlowServiceClient, flowID, runID, stepType string, expected int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		count := 0
		var page []byte
		for {
			history, err := reader.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{FlowId: flowID, RunId: runID, EstimatePageSize: 200, NextPageToken: page})
			if err != nil {
				t.Fatalf("read the staged CI record's Dex history: %v", err)
			}
			for _, event := range history.GetEvents() {
				completed := event.GetStepExecuteCompleted()
				if completed != nil && completed.GetContext().GetStepType() == stepType {
					count++
				}
			}
			page = history.GetNextPageToken()
			if len(page) == 0 {
				break
			}
		}
		if count >= expected {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the staged stale CI result never completed its production record Step")
}
