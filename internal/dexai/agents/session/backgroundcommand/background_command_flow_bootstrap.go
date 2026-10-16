// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package backgroundcommand

import (
	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
)

// BackgroundCommandFlowBootstrap assembles BackgroundCommandFlow with its
// Sandbox tool runner over the pooled workspace capabilities, which the
// Session's bootstrap owns and closes. It holds no durable state and serves no
// request.
type BackgroundCommandFlowBootstrap struct {
	flow *BackgroundCommandFlow
}

// NewBackgroundCommandFlowBootstrap assembles the Flow; workspaces are the
// Session's shared capabilities, which it borrows and never closes.
func NewBackgroundCommandFlowBootstrap(workspaces *workspace.Capabilities) *BackgroundCommandFlowBootstrap {
	return &BackgroundCommandFlowBootstrap{flow: &BackgroundCommandFlow{sandbox: sandboxtools.NewRunner(workspaces.SandboxResolver(), workspaces.Git())}}
}

// Flow returns the shared definition registered by the server bootstrap.
func (bootstrap *BackgroundCommandFlowBootstrap) Flow() *BackgroundCommandFlow { return bootstrap.flow }

// Client binds the Flow client to the shared Dex client.
func (bootstrap *BackgroundCommandFlowBootstrap) Client(sdk *dex.Client) *BackgroundCommandFlowClient {
	return &BackgroundCommandFlowClient{sdk: sdk}
}
