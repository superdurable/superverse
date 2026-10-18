// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package dexflow_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

var hardDeleteProbeAttribute = dex.DefineAttribute[string]("hard-delete-probe", dex.HardDeleteOnNull())
var hardDeleteProbeHold = dex.DefineChannel[dex.None]("hard-delete-probe-hold")

// HardDeleteProbeFlow exists only to exercise the released Dex hard-delete contract.
type HardDeleteProbeFlow struct{ dex.FlowDefaults }

func (HardDeleteProbeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(HoldHardDeleteProbe{})}
}

func (flow HardDeleteProbeFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.DeleteValue, nil)}
}

func (HardDeleteProbeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{
		Attributes: []dex.AttributeDef{hardDeleteProbeAttribute},
		Channels:   []dex.ChannelDef{hardDeleteProbeHold},
	}
}

func (HardDeleteProbeFlow) DeleteValue(ctx dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	if err := hardDeleteProbeAttribute.Delete(ctx); err != nil {
		return nil, err
	}
	return &dex.RPCResult[dex.None]{}, nil
}

type HoldHardDeleteProbe struct{ dex.StepDefaults }

func (HoldHardDeleteProbe) WaitFor(dex.Context, dex.None) (*dex.Wait, error) {
	return dex.Until(hardDeleteProbeHold.ForOne()), nil
}

func (HoldHardDeleteProbe) Execute(dex.Context, dex.None) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

// TestHardDeleteAttributeDexIntegration uses the real Server and blob store. The
// fixture value is synthetic and never appears in a test log or failure output.
func TestHardDeleteAttributeDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	workerHost := os.Getenv("SUPERVERSE_TEST_WORKER_HOST")
	if workerHost == "" {
		workerHost = "127.0.0.1"
	}
	flow := HardDeleteProbeFlow{}
	client := dexflowtest.WorkerClientForTest(ctx, t, address, workerHost, flow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	info, err := reader.GetServerInfo(ctx, &emptypb.Empty{})
	if err != nil || info.GetCurrentProtocolVersion() < 3 {
		t.Fatalf("Dex Server must support protocol 3: version %d (%s)", info.GetCurrentProtocolVersion(), dexflowtest.ErrorCategory(err))
	}

	flowID := "hard-delete-probe-" + flowid.RandomID(12)
	value := "synthetic-hard-delete-probe-" + flowid.RandomID(12)
	initial, err := dex.InitialAttribute(hardDeleteProbeAttribute, value)
	if err != nil {
		t.Fatalf("define initial Attribute: %s", dexflowtest.ErrorCategory(err))
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := client.StopFlow(cleanup, flowID, dex.StopOptions{Type: dex.CancelFlow, Reason: "hard-delete probe cleanup"}); err != nil {
			t.Errorf("stop hard-delete probe: %s", dexflowtest.ErrorCategory(err))
		}
	})
	requestID := flowID
	if _, err := client.StartFlow(ctx, flow, flowID, nil, dex.StartFlowOptions{
		Attributes:    []dex.InitialAttributeDef{initial},
		RequestID:     &requestID,
		IDReusePolicy: dex.IDReuseDisallow,
	}); err != nil {
		t.Fatalf("start hard-delete probe: %s", dexflowtest.ErrorCategory(err))
	}
	read := func() []*dexpb.KV {
		t.Helper()
		response, err := reader.GetAttributes(ctx, &dexpb.GetAttributesRequest{FlowId: flowID, Keys: []string{"hard-delete-probe"}})
		if err != nil {
			t.Fatalf("read exact Attribute: %s", dexflowtest.ErrorCategory(err))
		}
		return response.GetAttributes()
	}
	await := func(match func([]*dexpb.KV) bool, failure string) {
		t.Helper()
		deadline := time.NewTimer(30 * time.Second)
		defer deadline.Stop()
		for {
			if match(read()) {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-deadline.C:
				t.Fatal(failure)
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	await(func(attributes []*dexpb.KV) bool {
		return len(attributes) == 1 && attributes[0].GetKey() == "hard-delete-probe" && attributes[0].GetValue().GetStringValue() == value
	}, "exact GetAttributes did not return the written value")
	var none dex.None
	if err := client.InvokeRPC(ctx, flowID, flow.DeleteValue, none, &none); err != nil {
		t.Fatalf("delete hard-delete Attribute: %s", dexflowtest.ErrorCategory(err))
	}
	await(func(attributes []*dexpb.KV) bool { return len(attributes) == 0 }, "GetAttributes still returns the deleted value")

	summary, err := reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil {
		t.Fatalf("read Flow summary: %s", dexflowtest.ErrorCategory(err))
	}
	eventCount := 0
	for start := int64(1); ; {
		page, err := reader.GetHistoryEvents(ctx, &dexpb.GetHistoryEventsRequest{
			FlowId: flowID, RunId: summary.GetFlowExecutionId().GetRunId(), StartInternalEventId: start, EstimatePageSize: 200,
		})
		if err != nil {
			t.Fatalf("read Flow history: %s", dexflowtest.ErrorCategory(err))
		}
		for _, event := range page.GetEvents() {
			eventCount++
			encoded, err := proto.Marshal(event)
			if err != nil {
				t.Fatalf("encode Flow history event: %s", dexflowtest.ErrorCategory(err))
			}
			if bytes.Contains(encoded, []byte(value)) {
				t.Fatal("Flow history contains the deleted value")
			}
		}
		if len(page.GetNextPageToken()) == 0 {
			break
		}
		start = page.GetNextInternalEventId()
	}
	if eventCount == 0 {
		t.Fatal("Flow history contained no events to inspect")
	}
	t.Logf("Dex %s protocol %d with Blob Store accepted, read, and hard-deleted an Attribute; history has no fixture value", info.GetServerVersion(), info.GetCurrentProtocolVersion())
}
