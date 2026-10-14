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
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// workerOutage is how long no Worker serves the target: longer than a Worker
// restart, and far longer than the three seconds that three attempts with the
// engine's default one-second doubling backoff lasted.
const workerOutage = 15 * time.Second

// StateStepOutageFlow runs one ASYNC Step with StateStepOptions and no
// failure route, so exhausted retries would fail the Flow.
type StateStepOutageFlow struct{ dex.FlowDefaults }

func (*StateStepOutageFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(CommitStateAcrossOutage{})}
}

func (*StateStepOutageFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

type CommitStateAcrossOutage struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (CommitStateAcrossOutage) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (CommitStateAcrossOutage) Execute(dex.Context, dex.None) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

// ProviderStepOutageFlow runs one SYNC Step with ProviderStepOptions and no
// failure route.
type ProviderStepOutageFlow struct{ dex.FlowDefaults }

func (*ProviderStepOutageFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(CallProviderAcrossOutage{})}
}

func (*ProviderStepOutageFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

type CallProviderAcrossOutage struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (CallProviderAcrossOutage) GetStepOptions() *dex.StepOptions {
	return dexflow.ProviderStepOptions(nil)
}

func (CallProviderAcrossOutage) Execute(dex.Context, dex.None) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

// CappedStepOutageFlow runs one SYNC Step whose two attempts CappedStepRetry
// spaces, as the Preview source Step does, with no failure route.
type CappedStepOutageFlow struct{ dex.FlowDefaults }

func (*CappedStepOutageFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(CallCappedAcrossOutage{})}
}

func (*CappedStepOutageFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

type CallCappedAcrossOutage struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (CallCappedAcrossOutage) GetStepOptions() *dex.StepOptions {
	options := dexflow.ProviderStepOptions(nil)
	options.ExecuteRetry = dexflow.CappedStepRetry(2, 5*time.Minute)
	return options
}

func (CallCappedAcrossOutage) Execute(dex.Context, dex.None) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

// TestStepRetrySurvivesWorkerOutageDexIntegration starts one execution of
// each Step retry preset on a Worker target that nothing serves, so the real
// Dex Server's every attempt is refused, as during a Worker restart: the ASYNC
// state preset, whose first attempts run locally, the SYNC provider preset and
// the capped preset. After the outage every execution must still run, and once
// a Worker of this test serves the target each must complete. The executions
// are stopped before the test ends. It needs SUPERVERSE_TEST_DEX_ADDRESS and,
// where the Dex Server does not reach the test on 127.0.0.1,
// SUPERVERSE_TEST_WORKER_HOST.
func TestStepRetrySurvivesWorkerOutageDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	workerHost := os.Getenv("SUPERVERSE_TEST_WORKER_HOST")
	if workerHost == "" {
		workerHost = "127.0.0.1"
	}
	port := dexflowtest.FreePortForTest(ctx, t)
	flows := []dex.Flow{&StateStepOutageFlow{}, &ProviderStepOutageFlow{}, &CappedStepOutageFlow{}}
	starter := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: net.JoinHostPort(workerHost, port)}, flows...)
	reader := dexflowtest.ExecutionReaderForTest(t, address)

	suffix := flowid.RandomID(12)
	flowIDs := []string{"state-step-outage-" + suffix, "provider-step-outage-" + suffix, "capped-step-outage-" + suffix}
	started := time.Now()
	for index, flow := range flows {
		flowID := flowIDs[index]
		t.Cleanup(func() {
			cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer done()
			var inactive *dex.FlowNotActiveOrNotFoundError
			if err := starter.StopFlow(cleanup, flowID, dex.StopOptions{Type: dex.CancelFlow, Reason: "Step retry outage test cleanup"}); err != nil && !errors.As(err, &inactive) {
				t.Errorf("stop %s: %s", flowID, dexflowtest.ErrorCategory(err))
			}
		})
		if _, err := starter.StartFlow(ctx, flow, flowID, nil, dex.StartFlowOptions{}); err != nil {
			t.Fatalf("start %s: %s", flowID, dexflowtest.ErrorCategory(err))
		}
	}

	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-time.After(workerOutage - time.Since(started)):
	}
	for _, flowID := range flowIDs {
		summary, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
		if err != nil || summary.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_RUNNING {
			t.Errorf("%s did not outlast a %s Worker outage: %s (%s)", flowID, workerOutage, summary.GetFlowStatus(), dexflowtest.ErrorCategory(err))
		}
	}
	if t.Failed() {
		t.FailNow()
	}
	t.Logf("%v still run after %s without a Worker on %s", flowIDs, time.Since(started).Round(time.Second), net.JoinHostPort(workerHost, port))

	dexflowtest.WorkerClientAtForTest(ctx, t, address, workerHost, port, flows...)
	for _, flowID := range flowIDs {
		result, err := starter.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
		if err != nil || result.Status != dex.FlowCompleted {
			t.Fatalf("%s did not complete once a Worker served it: %s (%s)", flowID, result.Status, dexflowtest.ErrorCategory(err))
		}
	}
	t.Logf("%v completed %s after their start, once a Worker served them", flowIDs, time.Since(started).Round(time.Second))
}
