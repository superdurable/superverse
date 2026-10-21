// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/superdurable/superversev2/cmd/dexai-connect/mcpserver"
	"github.com/superdurable/superversev2/cmd/dexai-connect/platform"
	"github.com/superdurable/superversev2/cmd/dexai-connect/state"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
)

// restartDelay is how long a binding whose loops failed waits to restart.
const restartDelay = 30 * time.Second

// detachTimeout bounds the clean-up after a binding ends.
const detachTimeout = 15 * time.Second

// relay runs one binding: activation, then the sync, delivery, report and
// event loops until the binding ends or serve stops.
type relay struct {
	binding  *state.Binding
	client   *platform.Client
	provider Provider
	logger   *slog.Logger
	// toolWait is toolWaitBudget; tests shorten it.
	toolWait time.Duration
	// changed tells the supervisor that the tool set or the relay ended.
	changed func()

	connectionID string
	bindingID    string
	sessionID    string
	projectName  string

	wake chan struct{}
	done chan struct{}
	// queued wakes the delivery loop; dequeued wakes a read loop whose queue
	// was full.
	queued   chan struct{}
	dequeued chan struct{}

	mu       sync.Mutex
	progress state.Progress
	tools    []bridgeapi.Tool
	digest   string
	// agentContext is what the platform tells Codex (SyncResponse.AgentContext),
	// read once per relay start and with the brief; contextRead ends the ask.
	agentContext string
	contextRead  bool
	slotCalls    map[uint64]*pendingCall
	calls        map[string]*pendingCall
	// session holds the question the Session waits on and its other state.
	session sessionState
	// The read position, the queued deliveries not yet settled and the scan
	// while the queue is full (sync.go).
	readCursor    uint64
	queuedThrough uint64
	scanning      bool
	// settled counts settled deliveries; a scan rewinds only once it moved
	// past scanSettled, the count when the scan began.
	settled     uint64
	scanSettled uint64
	// scanResolved marks, by entry sequence, the calls an answer or tool
	// result resolved, with their delivery obligations, until queuedThrough
	// passes the entry or retention drops it, so a rewind does not deliver
	// them again but still accounts for them.
	scanResolved map[uint64][]resolvedCall
	queue        []delivery
	queueBytes   int
	runningTurn  string
	turnStarted  chan struct{}
	// ready is closed once the relay accepts new work: MCP calls and
	// deliveries (drain.go).
	ready chan struct{}
	// lost is closed when a call saw the conversation changed (reports.go).
	lost         chan struct{}
	lastSync     time.Time
	interrupt    *bridgeapi.Interrupt
	agentVersion string
	problem      string
	stopping     *detachment
	cancel       context.CancelFunc
}

// detachment is why a binding ends for good.
type detachment struct {
	// code is the endpoint's refusal; empty for the Builder's disconnect.
	code    bridgeapi.ErrorCode
	message string
}

func (d *detachment) Error() string {
	if d.code == "" {
		return "disconnected"
	}
	return fmt.Sprintf("%s: %s", d.code, d.message)
}

// detachOn returns the detachment a refusal means, or err unchanged: a
// replaced connection (409 executor_changed), a bad or revoked token (401)
// and a lost Builder role (403) end the binding.
func detachOn(err error) error {
	code, ok := platform.RefusalCode(err)
	if !ok {
		return err
	}
	switch code {
	case bridgeapi.ErrorExecutorChanged, bridgeapi.ErrorUnauthorized, bridgeapi.ErrorForbidden, bridgeapi.ErrorNotFound:
		var refusal *platform.RefusalError
		errors.As(err, &refusal)
		return &detachment{code: code, message: refusal.Body.Message}
	default:
		return err
	}
}

func newRelay(binding *state.Binding, client *platform.Client, provider Provider, logger *slog.Logger, changed func()) (*relay, error) {
	if binding.Record.ProviderID != provider.ProviderID() && !(binding.Record.ProviderID == "" && provider.ProviderID() == OrcaProviderID) {
		return nil, fmt.Errorf("binding %s belongs to provider %q, not %q", binding.Record.ConnectionID, binding.Record.ProviderID, provider.ProviderID())
	}
	progress, err := binding.Progress()
	if err != nil {
		return nil, err
	}
	return &relay{
		binding: binding, client: client, provider: provider, logger: logger, changed: changed, toolWait: toolWaitBudget,
		connectionID: binding.Record.ConnectionID, bindingID: mcpBindingID(binding.Record), sessionID: binding.Record.SessionID,
		projectName: binding.Record.ProjectName, agentVersion: binding.Record.AgentVersion,
		wake: make(chan struct{}, 1), done: make(chan struct{}),
		queued: make(chan struct{}, 1), dequeued: make(chan struct{}, 1),
		progress: progress, slotCalls: map[uint64]*pendingCall{}, calls: map[string]*pendingCall{},
		turnStarted: make(chan struct{}), lost: make(chan struct{}),
	}, nil
}

// mcpBindingID retains the Orca session address for existing MCP processes.
// Direct providers address the local socket by the unique connection ID.
func mcpBindingID(record state.BindingRecord) string {
	if record.ProviderID == "" || record.ProviderID == OrcaProviderID {
		return record.SessionID
	}
	return record.ConnectionID
}

// run keeps the binding's loops running until it is detached or ctx ends.
func (r *relay) run(ctx context.Context) {
	defer close(r.done)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r.mu.Lock()
	r.cancel = cancel
	if r.stopping != nil {
		cancel()
	}
	r.mu.Unlock()
	for {
		err := r.runLoops(ctx)
		var ended *detachment
		if errors.As(err, &ended) {
			r.finishDetach(ctx, ended, true)
			return
		}
		if errors.Is(err, platform.ErrEndpointGone) {
			r.logger.ErrorContext(ctx, "The endpoint no longer serves this connection; platform access was not revoked. Use dexai disconnect --local, then switch to the platform agent in Studio or pair again to take over.", "project", r.projectName)
			r.holdFailure(ctx, err, true)
			return
		}
		if isSessionChanged(err) {
			r.holdChanged(ctx, err)
			return
		}
		if errors.Is(err, ErrUnsupportedProtocol) {
			r.logger.ErrorContext(ctx, "binding stopped after an unsupported provider protocol answer", "project", r.projectName, "error", err)
			r.holdFailure(ctx, err, false)
			return
		}
		r.mu.Lock()
		stopping := r.stopping
		r.mu.Unlock()
		if stopping != nil {
			r.finishDetach(ctx, stopping, true)
			return
		}
		if ctx.Err() != nil {
			return
		}
		r.logger.ErrorContext(ctx, "binding loops failed; restarting", "project", r.projectName, "after", restartDelay, "error", err)
		r.setProblem(err)
		select {
		case <-ctx.Done():
		case <-time.After(restartDelay):
		}
	}
}

// stop ends the binding with reason and waits until it has cleaned up.
func (r *relay) stop(reason *detachment) {
	r.mu.Lock()
	r.stopping = reason
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-r.done
}

func (r *relay) runLoops(ctx context.Context) error {
	r.mu.Lock()
	r.ready = make(chan struct{})
	r.mu.Unlock()
	if err := r.start(ctx); err != nil {
		return detachOn(err)
	}
	r.mu.Lock()
	r.resetQueueLocked()
	r.mu.Unlock()
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error { return r.syncLoop(ctx) })
	group.Go(func() error { return r.deliveryLoop(ctx) })
	group.Go(func() error { return r.sendLoop(ctx) })
	group.Go(func() error { return r.eventLoop(ctx) })
	group.Go(func() error {
		select {
		case <-ctx.Done():
			return nil
		case <-r.lost:
			return r.provider.SessionChangedError()
		}
	})
	return group.Wait()
}

// start checks that the Orca conversation is still the bound one, activates
// (or repeats the activation of) the connection, adopts its generation,
// stops a Codex turn that is not positively over (drain.go) and does the
// binding's one-time history import. The brief waits for the relay to be
// ready (deliveryLoop).
func (r *relay) start(ctx context.Context) error {
	runtime, err := r.provider.Runtime(ctx)
	if err != nil {
		return fmt.Errorf("read the %s runtime: %w", r.provider.ProviderName(), err)
	}
	if runtime.Warning != "" {
		r.logger.WarnContext(ctx, runtime.Warning, "project", r.projectName)
	}
	conversation, err := r.provider.Conversation(ctx, r.sessionID)
	if err != nil {
		return fmt.Errorf("read the %s conversation: %w", r.provider.ProviderName(), err)
	}
	switch conversation.ProviderThreadID {
	case r.binding.Record.ProviderThreadID:
	case "":
		// Unconfirmed, not changed: start again later.
		return fmt.Errorf("the %s conversation has no %s thread to confirm", r.provider.ProviderName(), r.provider.AgentLabel())
	default:
		return r.provider.SessionChangedError()
	}
	// adopt clears the running turn; the drain needs to know it.
	r.mu.Lock()
	tracked := r.runningTurn
	r.mu.Unlock()
	record := r.binding.Record
	activation, err := r.client.Activate(ctx, bridgeapi.Binding{
		OrcaVersion: runtime.ProviderVersion, CodexVersion: runtime.AgentVersion, OrcaSessionID: record.SessionID,
		ProviderThreadID: record.ProviderThreadID, HostLabel: record.HostLabel,
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.agentVersion = runtime.AgentVersion
	if !r.progress.Activated {
		r.progress.Activated = true
		r.progress.Cursor = activation.LastSequence
	}
	r.progress.Epoch = activation.Epoch
	r.progress.Acknowledged = activation.Reported
	adopt := activation.Generation != r.progress.Generation || r.progress.Boundary.IsZero()
	err = r.saveLocked()
	r.mu.Unlock()
	if err != nil {
		return err
	}
	r.setTools(activation.Tools, activation.ToolSetDigest)
	if adopt {
		if err := r.adopt(ctx, activation.Generation); err != nil {
			return err
		}
	}
	if err := r.beginDrain(ctx, conversation, tracked); err != nil {
		return err
	}
	return r.importHistory(ctx)
}

// adopt makes generation current: it cancels the session's running turn and
// records Orca's event position once no turn runs as the boundary, before
// which no turn belongs to the new generation.
func (r *relay) adopt(ctx context.Context, generation uint64) error {
	position, err := r.provider.Cancel(ctx, r.sessionID, r.binding.Record.ProviderThreadID)
	if err != nil {
		return fmt.Errorf("cancel the %s turn for generation %d: %w", r.provider.AgentLabel(), generation, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation < r.progress.Generation {
		return nil
	}
	r.progress.Generation = generation
	r.progress.Boundary = position
	if r.progress.EventCursor.IsZero() {
		r.progress.EventCursor = position
	}
	r.runningTurn = ""
	return r.saveLocked()
}

// finishDetach cleans up a binding that ended: it cancels the running Codex
// turn when cancelTurn, revokes a replaced connection's own row, answers
// every open call and removes the binding.
func (r *relay) finishDetach(ctx context.Context, reason *detachment, cancelTurn bool) {
	// The relay's context has ended; the clean-up gets its own time.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), detachTimeout)
	defer cancel()
	if cancelTurn {
		if _, err := r.provider.Cancel(ctx, r.sessionID, r.binding.Record.ProviderThreadID); err != nil {
			r.logger.WarnContext(ctx, "cancel the "+r.provider.AgentLabel()+" turn of an ended binding", "project", r.projectName, "error", err)
		}
	}
	if reason.code == bridgeapi.ErrorExecutorChanged {
		if err := r.client.Disconnect(ctx); err != nil {
			r.logger.WarnContext(ctx, "revoke the replaced connection", "project", r.projectName, "error", err)
		}
	}
	r.mu.Lock()
	for _, call := range r.slotCalls {
		if call.callID == "" {
			call.finish(interruptedResult)
		}
	}
	for _, call := range r.calls {
		call.finish(detachedResult)
	}
	r.slotCalls, r.calls = map[uint64]*pendingCall{}, map[string]*pendingCall{}
	r.mu.Unlock()
	if err := r.binding.Remove(); err != nil {
		r.logger.ErrorContext(ctx, "remove the ended binding", "project", r.projectName, "error", err)
	}
	switch reason.code {
	case "":
		r.logger.InfoContext(ctx, "disconnected", "project", r.projectName)
	case bridgeapi.ErrorExecutorChanged:
		r.logger.WarnContext(ctx, "Connection replaced by another connection or the Platform agent; reconnect from Studio to take over again",
			"project", r.projectName, "detail", reason.message)
	default:
		r.logger.WarnContext(ctx, "Reconnect from Studio: the connection is no longer authorized", "project", r.projectName, "detail", reason.message)
	}
	r.changed()
}

// setTools replaces the tool set the binding's MCP clients see.
func (r *relay) setTools(tools []bridgeapi.Tool, digest string) {
	r.mu.Lock()
	changed := digest != r.digest
	r.tools, r.digest = tools, digest
	r.mu.Unlock()
	if changed {
		r.changed()
	}
}

func (r *relay) toolSet() ([]bridgeapi.Tool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tools, r.digest
}

// instructions is the platform's context for the MCP initialize answer; it is
// empty until the relay read it.
func (r *relay) instructions() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.agentContext
}

// noteAgentContext keeps the platform's context of an answer that carries it.
func (r *relay) noteAgentContext(answer bridgeapi.SyncResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contextRead = true
	if answer.AgentContext != "" {
		r.agentContext = answer.AgentContext
	}
}

func (r *relay) setProblem(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.problem = err.Error()
}

// statusLine is the binding's state in one line.
func (r *relay) statusLine() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	state := "idle"
	switch {
	case r.lastSync.IsZero():
		state = "starting"
	case time.Since(r.lastSync) > 2*bridgeapi.SyncWait+30*time.Second:
		state = "offline since " + r.lastSync.Format(time.RFC3339)
	case r.progress.Draining != nil:
		state = "settling the work sent before a restart"
	case r.runningTurn != "":
		state = "working"
	}
	if r.interrupt != nil && r.interrupt.By != "" {
		state += " · stop requested by " + r.interrupt.By
	}
	if r.problem != "" {
		state += " (last problem: " + r.problem + ")"
	}
	return fmt.Sprintf("Connected to %s as the agent · %s %s · %s", r.projectName, r.provider.AgentLabel(), r.agentVersion, state)
}

// saveLocked writes the progress; the caller holds mu.
func (r *relay) saveLocked() error {
	return r.binding.SaveProgress(r.progress)
}

var interruptedResult = mcpserver.CallResult{
	Text:    "interrupted: the Project Session stopped this work (Stop or a change of agent) before it ran",
	IsError: true,
}

// detachedResult answers a call that was sent, so may have run, when its
// binding ends: its outcome is unknown here.
var detachedResult = mcpserver.CallResult{
	Text:    "the platform binding ended after this call was sent; it may have run, and its result is not available here",
	IsError: true,
}
