// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package relay

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/superdurable/superversev2/cmd/superverse-bridge/mcpserver"
	"github.com/superdurable/superversev2/cmd/superverse-bridge/platform"
	"github.com/superdurable/superversev2/cmd/superverse-bridge/state"
	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
)

// Supervisor runs the relays of every binding in the state directory and
// answers the mcp processes over the local socket.
type Supervisor struct {
	dir    *state.Dir
	orca   Orca
	bridge bridgeapi.BridgeInfo
	logger *slog.Logger
	// attach hands Run a binding that connect stored while serve ran.
	attach chan attachRequest

	mu      sync.Mutex
	relays  map[string]*relay
	changed chan struct{}
}

type attachRequest struct {
	binding *state.Binding
	started chan error
}

var _ mcpserver.Backend = (*Supervisor)(nil)

// NewSupervisor returns the supervisor of dir's bindings.
func NewSupervisor(dir *state.Dir, orca Orca, bridge bridgeapi.BridgeInfo, logger *slog.Logger) *Supervisor {
	return &Supervisor{
		dir: dir, orca: orca, bridge: bridge, logger: logger, attach: make(chan attachRequest),
		relays: map[string]*relay{}, changed: make(chan struct{}),
	}
}

// Run starts every stored binding, and each one attached later, and runs
// until ctx ends.
func (supervisor *Supervisor) Run(ctx context.Context) error {
	var wait sync.WaitGroup
	defer wait.Wait()
	bindings, err := supervisor.dir.Bindings()
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if err := supervisor.start(ctx, &wait, binding); err != nil {
			supervisor.logger.ErrorContext(ctx, "start a binding", "connection", binding.Record.ConnectionID, "error", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case request := <-supervisor.attach:
			request.started <- supervisor.start(ctx, &wait, request.binding)
		}
	}
}

func (supervisor *Supervisor) start(ctx context.Context, wait *sync.WaitGroup, binding *state.Binding) error {
	token, err := binding.Token()
	if err != nil {
		return err
	}
	client, err := platform.New(binding.Record.Endpoint, token, supervisor.bridge, supervisor.logger)
	if err != nil {
		return err
	}
	r, err := newRelay(binding, client, supervisor.orca, supervisor.logger, supervisor.notify)
	if err != nil {
		return err
	}
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if _, running := supervisor.relays[r.connectionID]; running {
		return fmt.Errorf("binding %s already runs", r.connectionID)
	}
	for _, other := range supervisor.relays {
		if other.sessionID == r.sessionID {
			return fmt.Errorf("the Orca session of binding %s is already bound by %s", r.connectionID, other.connectionID)
		}
	}
	supervisor.relays[r.connectionID] = r
	wait.Go(func() {
		r.run(ctx)
		supervisor.mu.Lock()
		delete(supervisor.relays, r.connectionID)
		supervisor.mu.Unlock()
		supervisor.notify()
	})
	return nil
}

// notify wakes every tool watch.
func (supervisor *Supervisor) notify() {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	close(supervisor.changed)
	supervisor.changed = make(chan struct{})
}

// relayOf is the relay of an Orca session; start admits at most one.
func (supervisor *Supervisor) relayOf(session string) *relay {
	supervisor.mu.Lock()
	defer supervisor.mu.Unlock()
	if session == "" {
		return nil
	}
	for _, r := range supervisor.relays {
		if r.sessionID == session {
			return r
		}
	}
	return nil
}

// Tools is the tool set of a bound session; an unbound session has none.
func (supervisor *Supervisor) Tools(session string) ([]bridgeapi.Tool, string) {
	if r := supervisor.relayOf(session); r != nil {
		return r.toolSet()
	}
	return nil, ""
}

// Instructions is the platform's agent context of a bound session, empty
// until its relay read it.
func (supervisor *Supervisor) Instructions(session string) string {
	if r := supervisor.relayOf(session); r != nil {
		return r.instructions()
	}
	return ""
}

// Call reports a tool call of a bound session and waits for its result.
func (supervisor *Supervisor) Call(ctx context.Context, session, name string, arguments json.RawMessage) (mcpserver.CallResult, error) {
	r := supervisor.relayOf(session)
	if r == nil {
		return mcpserver.CallResult{Text: "this Codex session is not connected to Superverse", IsError: true}, nil
	}
	return r.call(ctx, name, arguments)
}

// WatchTools reports the session's tool set digest now and on each change.
func (supervisor *Supervisor) WatchTools(ctx context.Context, session string, changed func(string) error) error {
	sent := "-"
	for {
		supervisor.mu.Lock()
		wake := supervisor.changed
		supervisor.mu.Unlock()
		if _, digest := supervisor.Tools(session); digest != sent {
			if err := changed(digest); err != nil {
				return err
			}
			sent = digest
		}
		select {
		case <-ctx.Done():
			return nil
		case <-wake:
		}
	}
}

// Status is one line per binding.
func (supervisor *Supervisor) Status() []string {
	supervisor.mu.Lock()
	relays := make([]*relay, 0, len(supervisor.relays))
	for _, r := range supervisor.relays {
		relays = append(relays, r)
	}
	supervisor.mu.Unlock()
	slices.SortFunc(relays, func(a, b *relay) int { return cmp.Compare(a.projectName, b.projectName) })
	lines := make([]string, 0, len(relays))
	for _, r := range relays {
		lines = append(lines, r.statusLine())
	}
	if len(lines) == 0 {
		lines = append(lines, "No Project is connected. Run superverse-bridge connect, or connect from the Orca plugin.")
	}
	return lines
}

// Disconnect revokes a binding's connection, then ends and removes it.
func (supervisor *Supervisor) Disconnect(ctx context.Context, connectionID string) error {
	supervisor.mu.Lock()
	r := supervisor.relays[connectionID]
	supervisor.mu.Unlock()
	if r == nil {
		return fmt.Errorf("no running binding has connection %s", connectionID)
	}
	if err := Revoke(ctx, r.client); err != nil {
		return err
	}
	r.stop(&detachment{})
	return nil
}

// Attach starts a binding that connect stored while serve ran.
func (supervisor *Supervisor) Attach(ctx context.Context, connectionID string) error {
	bindings, err := supervisor.dir.Bindings()
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.Record.ConnectionID != connectionID {
			continue
		}
		request := attachRequest{binding: binding, started: make(chan error, 1)}
		select {
		case supervisor.attach <- request:
			return <-request.started
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("no stored binding has connection %s", connectionID)
}

// Revoke revokes a connection's own credential. A connection that is
// already replaced or unauthorized has nothing left to revoke.
func Revoke(ctx context.Context, client *platform.Client) error {
	err := client.Disconnect(ctx)
	var ended *detachment
	if errors.As(detachOn(err), &ended) {
		return nil
	}
	return err
}
