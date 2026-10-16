// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package workspace

import (
	"context"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
)

// bootstrapLifetime bounds the platform's run of the template's bootstrap
// command; the command ledger stops it then.
const bootstrapLifetime = 15 * time.Minute

// Capabilities is the workspace facade the Session Agent and the Sandbox
// provider share: one transport pool and the Git protocol on top of it. It owns
// only disposable connections, never Flow state.
type Capabilities struct {
	resolver *SandboxResolver
	git      *git.GitOperations
}

// NewCapabilities starts the transport pool, which reaches each Sandbox at the
// addresses its runtime resolves; library is the connectors library that the
// Session's develop subagents contribute to, and redirects the lane's module
// redirects of the tool environment.
func NewCapabilities(credentials *git.HostCredentials, library git.ConnectorLibrary, addresses SandboxAddresses, redirects ...sandboxd.ModuleRedirect) *Capabilities {
	return &Capabilities{resolver: newSandboxResolver(addresses, redirects),
		git: &git.GitOperations{Credentials: credentials, CheckDefinitions: checkWorktreeDefinitions, Library: library}}
}

func (c *Capabilities) Close() error                      { return c.resolver.Close() }
func (c *Capabilities) SandboxResolver() *SandboxResolver { return c.resolver }
func (c *Capabilities) Git() *git.GitOperations           { return c.git }

// withSandbox runs fn on the pooled client of binding's Sandbox and returns the borrow.
func withSandbox[T any](ctx context.Context, resolver *SandboxResolver, binding Binding, fn func(*sandboxd.SandboxClient) (T, error)) (T, error) {
	resolution, err := resolver.Bind(ctx, binding)
	if err != nil {
		var zero T
		return zero, err
	}
	defer resolution.Release()
	return fn(resolution.Client)
}

// Reach proves the transport of binding's Sandbox ready now. An error wrapping
// ErrUnreachable means sandboxd does not accept connections yet; no request
// was sent.
func (c *Capabilities) Reach(ctx context.Context, binding Binding) error {
	resolution, err := c.resolver.Bind(ctx, binding)
	if err != nil {
		return err
	}
	resolution.Release()
	return nil
}

// RepositoryContext derives the trusted prompt context from the checked-out
// HEAD of the leased workspace.
func (c *Capabilities) RepositoryContext(ctx context.Context, lease Lease) (string, error) {
	return withSandbox(ctx, c.resolver, lease.Binding, func(client *sandboxd.SandboxClient) (string, error) {
		return c.git.RepositoryContext(ctx, client)
	})
}

// Checkout prepares the repository of a new Sandbox instance.
func (c *Capabilities) Checkout(ctx context.Context, binding Binding, repository git.Repository) (git.CheckoutResult, error) {
	return withSandbox(ctx, c.resolver, binding, func(client *sandboxd.SandboxClient) (git.CheckoutResult, error) {
		return c.git.Checkout(ctx, client, repository)
	})
}

// Inspect reports whether the workspace of an instance holds uncommitted
// changes, and its HEAD.
func (c *Capabilities) Inspect(ctx context.Context, binding Binding) (dirty bool, head string, err error) {
	_, err = withSandbox(ctx, c.resolver, binding, func(client *sandboxd.SandboxClient) (struct{}, error) {
		var inspectErr error
		dirty, head, inspectErr = c.git.Inspect(ctx, client)
		return struct{}{}, inspectErr
	})
	return dirty, head, err
}

// BootstrapStart is how StartBootstrap went: whether HEAD declares a bootstrap
// command, when its ledger run started, or why none was started.
type BootstrapStart struct {
	Declared  bool
	StartedAt time.Time
	Refusal   string
}

// StartBootstrap starts the bootstrap command that HEAD's template manifest
// declares as the foreground command ledger run id in the workspace root, and
// returns without waiting for it. It asks the ledger first, so a repeated call
// answers the run an earlier one started and never starts a second.
func (c *Capabilities) StartBootstrap(ctx context.Context, binding Binding, id sandboxd.ExecutionID) (BootstrapStart, error) {
	return withSandbox(ctx, c.resolver, binding, func(client *sandboxd.SandboxClient) (BootstrapStart, error) {
		command, refusal, err := c.git.DeclaredBootstrap(ctx, client)
		switch {
		case err != nil:
			return BootstrapStart{}, err
		case refusal != "":
			return BootstrapStart{Declared: true, Refusal: refusal}, nil
		case len(command) == 0:
			return BootstrapStart{}, nil
		}
		ledger := client.CommandLedger()
		status, err := ledger.Status(ctx, id, 0, sandboxd.CommandOutputView{})
		if err != nil {
			return BootstrapStart{}, fmt.Errorf("read the bootstrap run: %w", err)
		}
		if status.State == sandboxd.CommandNotFound {
			status, err = ledger.Begin(ctx, sandboxd.CommandRequest{ID: id, WorkingDirectory: sandboxd.WorkspaceRoot, Command: command, Lifetime: bootstrapLifetime})
			if err != nil {
				return BootstrapStart{}, fmt.Errorf("start the bootstrap run: %w", err)
			}
		}
		start := BootstrapStart{Declared: true}
		if status.Refusal != "" {
			start.Refusal = fmt.Sprintf("the command ledger refused the bootstrap run: %s %s", status.Refusal, status.Reason)
		} else if status.StartedAt != nil {
			start.StartedAt = status.StartedAt.UTC()
		}
		return start, nil
	})
}
