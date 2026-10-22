// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflow_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

type FailureBoundaryFlow struct{ dex.FlowDefaults }

func (*FailureBoundaryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(AwaitFailureBoundary{}), dex.DefineStep(RecordFailureBoundary{})}
}

func (*FailureBoundaryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

type AwaitFailureBoundary struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (AwaitFailureBoundary) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(2*time.Second, dex.ProceedToOnExecuteFailure(RecordFailureBoundary{}, nil))
}

func (AwaitFailureBoundary) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type RecordFailureBoundary struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

type failureBoundaryResult struct {
	Detail, Message string
	TimedOut        bool
}

func (RecordFailureBoundary) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	cause := ""
	if recovery := ctx.RecoveryError(); recovery != nil {
		cause = recovery.Detail
	}
	return dex.GracefulComplete(failureBoundaryResult{
		Detail: dexflow.RecoveryDetail(ctx), Message: dexflow.FailureMessage("Source preparation did not finish within 5m30s.", cause),
		TimedOut: dexflow.RecoveryTimedOut(ctx),
	}), nil
}

// Exercise the shared boundary with a real engine deadline, including the
// actual SDK/server recovery envelope rather than a constructed context.
func TestFailureCauseBusinessMessageDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	flow := &FailureBoundaryFlow{}
	client := dexflowtest.WorkerClientForTest(ctx, t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), os.Getenv("SUPERVERSE_TEST_WORKER_HOST"), flow)
	id := "failure-boundary-" + flowid.RandomID(12)
	if _, err := client.StartFlow(ctx, flow, id, nil, dex.StartFlowOptions{}); err != nil {
		t.Fatal(dexflowtest.ErrorCategory(err))
	}
	result, err := client.WaitForFlow(ctx, id, dex.WaitForFlowOptions{NeedsResults: true})
	if err != nil || result.Status != dex.FlowCompleted {
		t.Fatalf("failure boundary did not complete: %s (%s)", result.Status, dexflowtest.ErrorCategory(err))
	}
	var output failureBoundaryResult
	if err = result.DecodeSingleOutput(&output); err != nil {
		t.Fatal(err)
	}
	if !output.TimedOut || output.Message != "Source preparation did not finish within 5m30s." {
		t.Fatalf("engine deadline did not become the authored business message: %+v", output)
	}
	for _, marker := range []string{"temporal", "activity", "scheduletoclose", "starttoclose", "retry"} {
		if strings.Contains(strings.ToLower(output.Detail), marker) {
			t.Fatalf("recovery detail exposes engine marker %q", marker)
		}
	}
}
