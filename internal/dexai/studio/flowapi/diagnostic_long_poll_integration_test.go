// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package flowapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// IdleDiagnosticsFlow has no Steps: its run stays open and records no event
// after its start until the test stops it.
type IdleDiagnosticsFlow struct{ dex.FlowDefaults }

func (*IdleDiagnosticsFlow) GetSteps() []dex.StepDef { return nil }

func (*IdleDiagnosticsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

// TestDiagnosticLongPollWindowDexIntegration long-polls the history of an idle
// run on a real Dex. The window ends on this process's clock with the 408
// timeout document, and Dex receives only the caller's later deadline, so a Dex
// clock that runs ahead cannot end the read first as Canceled. A caller that
// leaves still gets 499.
func TestDiagnosticLongPollWindowDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	host := dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	flow := &IdleDiagnosticsFlow{}
	sdk := dexflowtest.WorkerClientForTest(ctx, t, address, host, flow)
	flowID := "idle-diagnostics-" + uuid.NewString()
	runID, err := sdk.StartFlow(ctx, flow, flowID, nil, dex.StartFlowOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if stopErr := sdk.StopFlow(cleanup, flowID, dex.StopOptions{Type: dex.CancelFlow, Reason: "Idle diagnostics read test cleanup"}); stopErr != nil {
			t.Error(stopErr)
		}
	}()

	// The connection records the deadline that each history wait sends Dex.
	var mu sync.Mutex
	var sent []time.Time
	recordDeadline := func(ctx context.Context, method string, request, reply any, connection *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
		if method == dexpb.FlowService_WaitForHistoryEvent_FullMethodName {
			deadline, _ := ctx.Deadline()
			mu.Lock()
			sent = append(sent, deadline)
			mu.Unlock()
		}
		return invoke(ctx, method, request, reply, connection, options...)
	}
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(recordDeadline))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := connection.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	flows := New(dexpb.NewFlowServiceClient(connection), Authorization{}, nil, nil)
	// The idle run never records this event.
	request := HistoryRequest{FlowRun: FlowRun{FlowID: flowID, RunID: runID}, AfterEventID: 1 << 40}

	// The Studio BFF gives a Flow API call 35 seconds.
	caller, callerDone := context.WithTimeout(ctx, 35*time.Second)
	defer callerDone()
	callerDeadline, _ := caller.Deadline()
	started := time.Now()
	document := flows.WaitHistory(caller, request)
	elapsed := time.Since(started)
	var body errorResponse
	if err = json.Unmarshal(document.JSON, &body); err != nil {
		t.Fatal(err)
	}
	if document.StatusCode != http.StatusRequestTimeout || body.GRPCCode == nil || *body.GRPCCode != 4 {
		t.Fatalf("an idle wait must end with the 408 timeout document, got %d %s", document.StatusCode, document.JSON)
	}
	if elapsed < diagnosticLongPollTimeout || elapsed > diagnosticLongPollTimeout+2*time.Second {
		t.Fatalf("the idle window took %v instead of %v", elapsed, diagnosticLongPollTimeout)
	}
	mu.Lock()
	if len(sent) != 1 || !sent[0].Equal(callerDeadline) {
		t.Fatalf("Dex must receive only the caller's deadline %v, got %v", callerDeadline, sent)
	}
	mu.Unlock()

	// A caller that leaves ends the wait at once as canceled.
	leaving, leave := context.WithCancel(ctx)
	time.AfterFunc(time.Second, leave)
	started = time.Now()
	document = flows.WaitHistory(leaving, request)
	elapsed = time.Since(started)
	if document.StatusCode != 499 || elapsed > 5*time.Second {
		t.Fatalf("a caller that leaves must end the wait as canceled, got %d %s after %v", document.StatusCode, document.JSON, elapsed)
	}
	t.Logf("FlowID=%s idle window %v ended with 408; Dex received the caller's deadline; a leaving caller got 499", flowID, diagnosticLongPollTimeout)
}
