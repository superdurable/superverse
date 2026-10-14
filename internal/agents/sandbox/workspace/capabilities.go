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

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/sandboxd"
)

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
