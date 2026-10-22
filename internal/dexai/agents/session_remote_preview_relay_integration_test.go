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
	"errors"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// This relay runs no application handler and creates no dependency response.
// Every request, stream frame, status and metadata comes from the lane Worker.
// All genuine attempts of one exact execution wait. Production request
// deadlines and retries stay in force; the fixture never extends them.
const remotePreviewGateBudget = 2 * time.Minute

type remotePreviewHeld struct {
	executionID string
	handoff     agentloop.PreviewHandoff
	at          time.Time
}

type remotePreviewGate struct {
	flowID, runID, stepType string
	reader                  dexpb.FlowServiceClient
	mu                      sync.Mutex
	selected                string
	originalInput           *dexpb.Value
	original                remotePreviewHeld
	captured                bool
	attempts                []int32
	maxAttempt              int32
	upstreamOpens           int
	deadline                *time.Timer
	released                bool
	held                    chan remotePreviewHeld
	release                 chan struct{}
	releaseOnce             sync.Once
	failure                 chan string
}

func (gate *remotePreviewGate) fail(message string) {
	select {
	case gate.failure <- message:
	default:
	}
}

func (gate *remotePreviewGate) open() {
	gate.mu.Lock()
	gate.released = true
	if gate.deadline != nil {
		gate.deadline.Stop()
	}
	gate.mu.Unlock()
	gate.releaseOnce.Do(func() { close(gate.release) })
}

func (gate *remotePreviewGate) expire() {
	gate.mu.Lock()
	if gate.released {
		gate.mu.Unlock()
		return
	}
	gate.fail("Preview gate exhausted its2m budget before the newer admission; race coverage failed")
	gate.released = true
	gate.mu.Unlock()
	// Budget exhaustion records a test failure, but creates no Worker status:
	// only a still-live genuine request may now reach the real Worker.
	gate.releaseOnce.Do(func() { close(gate.release) })
}

func (gate *remotePreviewGate) observedAttempts() ([]int32, int32, int) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return slices.Clone(gate.attempts), gate.maxAttempt, gate.upstreamOpens
}

func (gate *remotePreviewGate) recordUpstreamOpen(request *dexpb.InvokeExecuteMethodRequest) {
	identity := request.GetContext()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if identity.GetFlowId() == gate.flowID && identity.GetRunId() == gate.runID && request.GetStepType() == gate.stepType && identity.GetStepExecutionId() == gate.selected {
		gate.upstreamOpens++
	}
}

func (gate *remotePreviewGate) await(ctx context.Context, t *testing.T) remotePreviewHeld {
	t.Helper()
	arrival, cancel := context.WithTimeout(ctx, remotePreviewGateBudget)
	defer cancel()
	select {
	case reason := <-gate.failure:
		t.Fatal(reason)
	case held := <-gate.held:
		return held
	case <-arrival.Done():
		t.Fatal("the exact real Preview Step did not reach the bounded gate")
	}
	return remotePreviewHeld{}
}

func (gate *remotePreviewGate) requireHealthy(t *testing.T) {
	t.Helper()
	select {
	case reason := <-gate.failure:
		t.Fatal(reason)
	default:
	}
}

func (gate *remotePreviewGate) wait(ctx context.Context, request *dexpb.InvokeExecuteMethodRequest) error {
	identity := request.GetContext()
	if identity.GetFlowId() != gate.flowID || identity.GetRunId() != gate.runID || request.GetStepType() != gate.stepType {
		return nil
	}
	gate.mu.Lock()
	first := gate.selected == ""
	if first {
		if identity.GetStepExecutionId() == "" || request.GetStepInput() == nil || proto.Size(request.GetStepInput()) > 20<<10 {
			gate.mu.Unlock()
			gate.fail("the original Preview request lacks its bounded execution/input identity")
			gate.open()
			return ctx.Err()
		}
		gate.selected = identity.GetStepExecutionId()
		gate.original = remotePreviewHeld{executionID: gate.selected, at: time.Now()}
		gate.originalInput = proto.Clone(request.GetStepInput()).(*dexpb.Value)
		if !gate.released {
			gate.deadline = time.AfterFunc(time.Until(gate.original.at.Add(remotePreviewGateBudget)), gate.expire)
		}
	}
	if gate.selected != identity.GetStepExecutionId() {
		gate.mu.Unlock()
		return nil
	}
	// A transport reattachment may repeat an attempt, and delayed arrivals
	// may be out of order. The maximum only increases; retain at most64 arrivals.
	attempt := identity.GetAttempt()
	if attempt < 1 || len(gate.attempts) >= 64 || !proto.Equal(request.GetStepInput(), gate.originalInput) {
		gate.mu.Unlock()
		gate.fail("Preview retries changed their original input, lacked an attempt or exceeded64 requests")
		gate.open()
		return ctx.Err()
	}
	gate.attempts = append(gate.attempts, attempt)
	gate.maxAttempt = max(gate.maxAttempt, attempt)
	gate.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	// One5s budget per attempt, including exact-key bounded object hydration.
	// The original request and its deadline remain unchanged upstream.
	hydrate, cancel := context.WithTimeout(ctx, 5*time.Second)
	var handoff agentloop.PreviewHandoff
	err := remoteBoundedJSON(hydrate, gate.reader, gate.flowID, request.GetStepInput(), &handoff)
	cancel()
	if err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		gate.fail("the held Preview input lacks its bounded original typed payload")
		gate.open()
		return ctx.Err()
	}
	if handoff.Session == nil || handoff.Commit == "" || handoff.Sequence == 0 || handoff.WorkspaceRequestID == "" {
		gate.fail("the held Preview input lacks an immutable original handoff")
		gate.open()
		return ctx.Err()
	}
	gate.mu.Lock()
	if !gate.captured {
		gate.original.handoff = handoff
		gate.captured = true
		gate.held <- gate.original
	} else if !sameRemotePreviewHandoff(gate.original.handoff, handoff) {
		gate.mu.Unlock()
		gate.fail("a genuine Preview retry changed its original immutable handoff")
		gate.open()
		return ctx.Err()
	}
	gate.mu.Unlock()
	select {
	case <-gate.release:
		return ctx.Err()
	case <-ctx.Done():
		// This is the genuine request's cancellation, not a fixture-generated
		// timeout. The same execution's next attempt remains held.
		return ctx.Err()
	}
}

func sameRemotePreviewHandoff(first, next agentloop.PreviewHandoff) bool {
	return first.Session != nil && next.Session != nil && *first.Session == *next.Session &&
		first.WorkspaceRequestID == next.WorkspaceRequestID && first.Commit == next.Commit &&
		first.Sequence == next.Sequence && first.PublishedSequence == next.PublishedSequence
}

type remotePreviewRelay struct {
	dexpb.UnimplementedWorkerServiceServer
	worker dexpb.WorkerServiceClient
	gate   *remotePreviewGate
}

func remotePreviewForwardContext(ctx context.Context) context.Context {
	if incoming, ok := metadata.FromIncomingContext(ctx); ok {
		return metadata.NewOutgoingContext(ctx, incoming.Copy())
	}
	return ctx
}

func forwardRemotePreviewFrames[T any](upstream grpc.ServerStreamingClient[T], downstream grpc.ServerStreamingServer[T]) error {
	defer func() { downstream.SetTrailer(upstream.Trailer()) }()
	header, err := upstream.Header()
	if err != nil {
		return err
	}
	if err := downstream.SendHeader(header); err != nil {
		return err
	}
	for {
		frame, err := upstream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := downstream.Send(frame); err != nil {
			return err
		}
	}
}

func (relay *remotePreviewRelay) InvokeWaitForMethod(request *dexpb.InvokeWaitForMethodRequest, downstream grpc.ServerStreamingServer[dexpb.InvokeWaitForMethodOutput]) error {
	upstream, err := relay.worker.InvokeWaitForMethod(remotePreviewForwardContext(downstream.Context()), request)
	if err != nil {
		return err
	}
	return forwardRemotePreviewFrames(upstream, downstream)
}

func (relay *remotePreviewRelay) InvokeExecuteMethod(request *dexpb.InvokeExecuteMethodRequest, downstream grpc.ServerStreamingServer[dexpb.InvokeExecuteMethodOutput]) error {
	if err := relay.gate.wait(downstream.Context(), request); err != nil {
		return err
	}
	// Release and cancellation can become ready together. A cancelled
	// request must never be forwarded with a replacement live context.
	if err := downstream.Context().Err(); err != nil {
		return err
	}
	upstream, err := relay.worker.InvokeExecuteMethod(remotePreviewForwardContext(downstream.Context()), request)
	if err != nil {
		return err
	}
	relay.gate.recordUpstreamOpen(request)
	return forwardRemotePreviewFrames(upstream, downstream)
}

func (relay *remotePreviewRelay) InvokeWorkerRPC(ctx context.Context, request *dexpb.InvokeWorkerRPCRequest) (*dexpb.InvokeWorkerRPCResponse, error) {
	var header, trailer metadata.MD
	response, err := relay.worker.InvokeWorkerRPC(remotePreviewForwardContext(ctx), request, grpc.Header(&header), grpc.Trailer(&trailer))
	if headerErr := grpc.SendHeader(ctx, header); headerErr != nil && err == nil {
		return nil, headerErr
	}
	if trailerErr := grpc.SetTrailer(ctx, trailer); trailerErr != nil && err == nil {
		return nil, trailerErr
	}
	return response, err
}

// installRemotePreviewRelay redirects only the throwaway Session. The original
// FULL config is read back even after a lost update response. Unconfirmed
// restoration keeps forwarding alive through the existing Project cleanup.
func installRemotePreviewRelay(ctx context.Context, t *testing.T, session *mockSession, stepType string, cleanup *remotePreviewCleanup) *remotePreviewGate {
	t.Helper()
	flowID := string(session.id)
	summary, err := session.reader.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil || summary.GetFlowExecutionId().GetFlowId() != flowID || summary.GetFlowExecutionId().GetRunId() == "" {
		t.Fatal("Preview relay lacks the exact live Session identity")
	}
	state, err := session.reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: flowID, RunId: summary.GetFlowExecutionId().GetRunId()})
	if err != nil || !state.GetFlowConfig().GetWorkerTarget().GetIsPullMode() {
		t.Fatal("Preview relay cannot preserve a missing original Worker target/config")
	}
	original := proto.Clone(state.GetFlowConfig()).(*dexpb.FlowConfig)
	connection, err := grpc.NewClient(remotePreviewWorkerDialTarget(ctx, t, cleanup), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal("Preview relay real Worker connection failed")
	}
	port := dexflowtest.FreePortForTest(ctx, t)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("", port))
	if err != nil {
		_ = connection.Close()
		t.Fatal("Preview relay listener failed")
	}
	gate := &remotePreviewGate{flowID: flowID, runID: summary.GetFlowExecutionId().GetRunId(), stepType: stepType, reader: cleanup.reader,
		held: make(chan remotePreviewHeld, 1), release: make(chan struct{}), failure: make(chan string, 1)}
	server := grpc.NewServer()
	cleanup.gate = gate
	cleanup.closeRelay = func() {
		server.Stop()
		_ = listener.Close()
		if err := connection.Close(); err != nil {
			t.Error("Preview relay upstream connection close failed")
		}
	}
	dexpb.RegisterWorkerServiceServer(server, &remotePreviewRelay{worker: dexpb.NewWorkerServiceClient(connection), gate: gate})
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			gate.fail("Preview relay transport stopped unexpectedly")
		}
	}()
	redirected := proto.Clone(original).(*dexpb.FlowConfig)
	redirected.WorkerTarget = &dexpb.WorkerTarget{Address: net.JoinHostPort(dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST"), port)}
	t.Cleanup(func() {
		gate.open()
		reconcile, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := reconcileRemotePreviewConfig(reconcile, cleanup.reader, flowID, gate.runID, original, redirected); err != nil {
			t.Errorf("Preview relay original config unconfirmed for FlowID=%s RunID=%s; forwarding remains live through ordinary Project deletion (%T)", flowID, gate.runID, err)
			return
		}
		cleanup.restored = true
		cleanup.close()
	})
	if _, err := session.reader.UpdateFlowConfig(ctx, &dexpb.UpdateFlowConfigRequest{FlowId: flowID, RunId: gate.runID, FlowConfig: redirected}); err != nil {
		t.Fatal("Preview relay isolated config admission failed")
	}
	ready, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := awaitRemotePreviewConfig(ready, session.reader, flowID, gate.runID, redirected); err != nil {
		t.Fatal("Preview relay isolated full config did not apply")
	}
	return gate
}

// Retry an idempotent restore only after an exact read of our redirected config.
// Every response, including an error, is reconciled by readback. An observed
// foreign config is refused, and an unconfirmed effect is never success.
func reconcileRemotePreviewConfig(ctx context.Context, reader dexpb.FlowServiceClient, flowID, runID string, original, redirected *dexpb.FlowConfig) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		read, cancel := context.WithTimeout(ctx, 3*time.Second)
		state, err := reader.GetFlowState(read, &dexpb.GetFlowStateRequest{FlowId: flowID, RunId: runID})
		cancel()
		if err == nil {
			switch {
			case proto.Equal(state.GetFlowConfig(), original):
				return nil
			case !proto.Equal(state.GetFlowConfig(), redirected):
				return errors.New("foreign full Flow config; restore refused")
			default:
				write, cancel := context.WithTimeout(ctx, 5*time.Second)
				_, _ = reader.UpdateFlowConfig(write, &dexpb.UpdateFlowConfigRequest{FlowId: flowID, RunId: runID, FlowConfig: original})
				cancel()
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func awaitRemotePreviewConfig(ctx context.Context, reader dexpb.FlowServiceClient, flowID, runID string, expected *dexpb.FlowConfig) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := reader.GetFlowState(ctx, &dexpb.GetFlowStateRequest{FlowId: flowID, RunId: runID})
		if err != nil {
			return err
		}
		if proto.Equal(state.GetFlowConfig(), expected) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
