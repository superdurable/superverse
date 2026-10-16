// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package platformissue

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/githubissues"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/toolfailure"
)

// TestPlatformIssueJoinDexIntegration reports a platform fault of a fresh
// signature on a Worker target that nothing serves yet, so the run of the
// signature's PlatformIssueFlow stays open: a report of the same signature
// from another call of another Session joins that run, and a retry of the
// first report joins it again. Once a Worker of this test serves the target,
// with the issue repository of a server that names none
// (githubissues.NotConfigured), the one run runs EnsurePlatformIssue and
// RecordPlatformIssue once each and records the outcome logged with the first
// report's incident. It needs SUPERVERSE_TEST_DEX_ADDRESS and, where the Dex
// Server does not reach the test on 127.0.0.1, SUPERVERSE_TEST_WORKER_HOST
// (Kind: host.docker.internal). The run is closed before the test ends.
func TestPlatformIssueJoinDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	incident, state := joinReports(ctx, t, githubissues.NotConfigured{})
	if state.Outcome.Status != outcomeLogged || state.Outcome.IssueNumber != 0 {
		t.Fatalf("the run of %s did not only log the incident: %+v", incident.flowID(), *state.Outcome)
	}
}

// joinReports starts two reports of one fresh signature, from two calls of
// two Sessions, and a retry of the first while the signature's run cannot
// progress, requires that the later two joined the first, then serves the run
// on a Worker of this test whose Flow files through issues, and returns the
// first report's incident and the run's snapshot once it completed. It fails
// unless the run kept the first report's incident and ran each Step once.
func joinReports(ctx context.Context, t *testing.T, issues PlatformIssueFlowDependency) (Incident, snapshot) {
	t.Helper()
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	workerHost := os.Getenv("SUPERVERSE_TEST_WORKER_HOST")
	if workerHost == "" {
		workerHost = "127.0.0.1"
	}
	port := dexflowtest.FreePortForTest(ctx, t)
	flow := &PlatformIssueFlow{}
	starter := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: net.JoinHostPort(workerHost, port)}, flow)
	client := newPlatformIssueFlowClient(starter, flow, deploymentnames.EnvironmentTest)
	reader := dexflowtest.ExecutionReaderForTest(t, address)

	// A fresh error type gives the fault a signature no other run has.
	first := Incident{
		SessionFlowID: flowid.SessionFlowID(flowid.RandomID(8)), CallID: "toolu_" + flowid.RandomID(12), Tool: string(conversation.ToolReadTaskOutput),
		Code: toolfailure.ToolUnavailable.String(), Owner: toolfailure.OwnerPlatform.String(), Effect: toolfailure.EffectNone.String(),
		ErrorType: "join-" + flowid.RandomID(12), OccurredAt: time.Now().UTC().Truncate(time.Second),
	}
	second := first
	second.SessionFlowID, second.CallID = flowid.SessionFlowID(flowid.RandomID(8)), "toolu_"+flowid.RandomID(12)
	first.Environment, second.Environment = deploymentnames.EnvironmentTest, deploymentnames.EnvironmentTest
	flowID := first.flowID()
	t.Cleanup(func() { stopReport(ctx, t, starter, flowID) })

	for index, report := range []struct {
		incident Incident
		joined   bool
	}{{first, false}, {second, true}, {first, false}} {
		started, err := client.Report(ctx, report.incident)
		if err != nil || started.FlowID != flowID || started.Joined != report.joined {
			t.Fatalf("report %d of %s answered %+v (%s), want joined=%t", index+1, flowID, started, dexflowtest.ErrorCategory(err), report.joined)
		}
	}
	running, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil || running.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		t.Fatalf("the run of %s does not wait for its Worker: %s (%s)", flowID, running.GetFlowStatus(), dexflowtest.ErrorCategory(err))
	}
	t.Logf("%s: three reports of one signature, two of them joined, wait on the Worker target %s", flowID, net.JoinHostPort(workerHost, port))

	dexflowtest.WorkerClientAtForTest(ctx, t, address, workerHost, port, NewPlatformIssueFlowBootstrap(issues, deploymentnames.EnvironmentTest, slog.Default()).Flow())
	result, err := starter.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	if err != nil || result.Status != dex.FlowCompleted {
		t.Fatalf("the run of %s did not complete: %s (%s)", flowID, result.Status, dexflowtest.ErrorCategory(err))
	}
	state, err := client.GetForTest(ctx, first.signature())
	if err != nil || state.Outcome == nil {
		t.Fatalf("the completed run of %s has no outcome (%s)", flowID, dexflowtest.ErrorCategory(err))
	}
	kept, want := state.Incident, first
	kept.OccurredAt, want.OccurredAt = time.Time{}, time.Time{}
	if kept != want || !state.Incident.OccurredAt.Equal(first.OccurredAt) {
		t.Fatalf("a joined report replaced the incident of %s: %+v", flowID, state.Incident)
	}
	summary, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil || summary.GetFirstRunId() != summary.GetFlowExecutionId().GetRunId() {
		t.Fatalf("the reports of %s ran more than one run (%s)", flowID, dexflowtest.ErrorCategory(err))
	}
	if steps := completedSteps(ctx, t, reader, flowID, summary.GetFlowExecutionId().GetRunId()); !slices.Equal(steps, []string{"EnsurePlatformIssue", "RecordPlatformIssue"}) {
		t.Fatalf("the one run of %s completed %v", flowID, steps)
	}
	t.Logf("%s: one run filed the three reports: %+v", flowID, *state.Outcome)
	return first, state
}

// completedSteps lists the Step types whose Execute completed in the run of
// flowID, in history order, a diagnostic read of the engine history.
func completedSteps(ctx context.Context, t *testing.T, reader dexpb.FlowServiceClient, flowID, runID string) []string {
	t.Helper()
	var steps []string
	for start := int64(1); ; {
		page, err := reader.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{FlowId: flowID, RunId: runID, StartInternalEventId: start, EstimatePageSize: 200})
		if err != nil {
			t.Fatalf("history read of %s failed (%T)", flowID, err)
		}
		for _, event := range page.GetEvents() {
			if completed := event.GetStepExecuteCompleted(); completed != nil {
				steps = append(steps, completed.GetContext().GetStepType())
			}
		}
		if len(page.GetNextPageToken()) == 0 {
			return steps
		}
		start = page.GetNextInternalEventId()
	}
}

// stopReport closes the run of flowID if it still runs, so no execution
// stays on this test's Worker target, which the Native launcher's callback
// proof would refuse.
func stopReport(ctx context.Context, t *testing.T, client *dex.Client, flowID string) {
	stopped, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var closed *dex.FlowNotActiveOrNotFoundError
	if err := client.StopFlow(stopped, flowID, dex.StopOptions{}); err != nil && !errors.As(err, &closed) {
		t.Errorf("stop of %s failed (%s)", flowID, dexflowtest.ErrorCategory(err))
	}
}
