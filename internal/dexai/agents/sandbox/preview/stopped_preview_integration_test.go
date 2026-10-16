// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
)

// TestStoppedPreviewTimeTravelDexIntegration runs the operator recovery of a
// stopped Preview from the rollout reconciliation runbook against the real Dex
// and the actual Worker of the local lane. An operator terminates the engine
// execution of a Preview that waits for commands: every command then answers
// ErrClosed, and a new start joins the closed execution, so nothing replaces
// it. A time travel to its last AwaitPreviewCommands wait brings it back with
// its state, and it admits and runs a stop command to STOPPED again. The
// Preview belongs to no Project, so its stop finds no Sandbox namespace and no
// route and changes nothing; the test ends the Preview itself.
func TestStoppedPreviewTimeTravelDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	flow := &preview.PreviewFlow{}
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, flow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	client := &preview.PreviewFlowClient{Dex: sdk, Flow: flow, Execution: reader}
	project := flowid.RandomID(8)
	flowID := preview.FlowID(project)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer stop()
		for {
			ended, err := client.End(cleanup, project)
			if err == nil && ended {
				return
			}
			select {
			case <-cleanup.Done():
				t.Errorf("the Preview %s did not end (%v); end it explicitly", flowID, err)
				return
			case <-time.After(time.Second):
			}
		}
	})
	await := func(what string, done func() bool) {
		t.Helper()
		for !done() {
			select {
			case <-ctx.Done():
				t.Fatalf("%s: %v", what, ctx.Err())
			case <-time.After(time.Second):
			}
		}
	}
	waits := func() bool {
		state, err := reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: flowID})
		if err != nil {
			return false
		}
		for _, step := range state.GetActiveStepExecutions() {
			if step.GetStepType() == dex.GetFinalStepType(preview.AwaitPreviewCommands{}) {
				return true
			}
		}
		return false
	}
	status := func() dexpb.FlowStatus {
		summary, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
		if err != nil {
			t.Fatalf("the engine summary of %s is unreadable (%T)", flowID, err)
		}
		return summary.GetFlowStatus()
	}

	if err := client.Start(ctx, sandbox.Identity{ProjectID: project}); err != nil {
		t.Fatalf("the Preview did not start (%T)", err)
	}
	await("the Preview waits for its first command", waits)
	if err := sdk.StopFlow(ctx, flowID, dex.StopOptions{Type: dex.TerminateFlow, Reason: "test: an operator stops the Preview"}); err != nil {
		t.Fatalf("the operator stop failed (%T)", err)
	}
	await("the Preview's engine execution ends", func() bool { return status() == dexpb.FlowStatus_FLOW_STATUS_TERMINATED })

	stop := preview.Command{ID: "stop-" + flowid.RandomID(8), Kind: preview.CommandStopPreview, Generation: sandbox.InstanceGeneration}
	if _, err := client.Command(ctx, project, stop); !errors.Is(err, preview.ErrClosed) {
		t.Fatalf("a command to the stopped Preview was not refused ErrClosed (%v)", err)
	}
	// IDReuseDisallow: a new start joins the closed execution.
	if err := client.Start(ctx, sandbox.Identity{ProjectID: project}); err != nil {
		t.Fatalf("a repeated start did not join the closed execution (%T)", err)
	}
	if _, err := client.Command(ctx, project, stop); !errors.Is(err, preview.ErrClosed) || status() != dexpb.FlowStatus_FLOW_STATUS_TERMINATED {
		t.Fatalf("a repeated start replaced the stopped Preview (%v)", err)
	}

	// The runbook's recovery: back to the wait for commands.
	waitStep := dex.GetFinalStepType(preview.AwaitPreviewCommands{}) + "-1"
	if _, err := sdk.TimeTravel(ctx, flowID, dex.TimeTravelOptions{Type: dex.TimeTravelByStepExecutionID, StepExecutionID: waitStep, StepMethod: dex.TimeTravelStepWaitFor, Reason: "test: the runbook's recovery of a stopped Preview"}); err != nil {
		t.Fatalf("the time travel to %s failed (%v)", waitStep, err)
	}
	await("the recovered Preview runs", func() bool { return status() == dexpb.FlowStatus_FLOW_STATUS_RUNNING && waits() })
	if _, err := client.Command(ctx, project, stop); err != nil {
		t.Fatalf("the recovered Preview refused the stop (%v)", err)
	}
	await("the recovered Preview stops", func() bool {
		s, err := client.Get(ctx, project)
		return err == nil && s.ActiveCommand.ID == "" && s.LastCommand.ID == stop.ID && s.Preview.Status == preview.PreviewStoppedForTest && s.Failure == nil
	})
	t.Logf("FlowID=%s terminated, refused commands and a new start, came back through a time travel to %s and ran %s to STOPPED", flowID, waitStep, stop.ID)
}
