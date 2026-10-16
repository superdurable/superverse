// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sessionworkspace

import (
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
)

// SessionWorkspaceFlowBootstrap assembles SessionWorkspaceFlow with the
// process-local resources it reclaims through, the instance provider and the
// pooled workspace transport, and builds its client. The Session Agent Flow
// bootstrap owns and closes both resources; this bootstrap holds no durable
// state and serves no request.
type SessionWorkspaceFlowBootstrap struct {
	flow *SessionWorkspaceFlow
}

// NewSessionWorkspaceFlowBootstrap assembles the Flow; dependency starts and
// reads its provisionings, asks the Preview to release an instance and
// reports to the Session.
func NewSessionWorkspaceFlowBootstrap(provider sandbox.InstanceProvider, workspaces *workspace.Capabilities, dependency SessionWorkspaceFlowDependency) *SessionWorkspaceFlowBootstrap {
	return &SessionWorkspaceFlowBootstrap{flow: &SessionWorkspaceFlow{dependency: dependency, reclaimer: newReclaimer(provider, workspaces)}}
}

// Flow returns the shared definition registered by the server bootstrap.
func (bootstrap *SessionWorkspaceFlowBootstrap) Flow() *SessionWorkspaceFlow { return bootstrap.flow }

// Client binds the Flow client to the shared Dex client and the engine status
// reader.
func (bootstrap *SessionWorkspaceFlowBootstrap) Client(sdk *dex.Client, execution dexpb.FlowServiceClient) *SessionWorkspaceFlowClient {
	return newSessionWorkspaceFlowClient(sdk, bootstrap.flow, execution)
}
