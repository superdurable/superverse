// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package provision

import (
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
)

// SandboxProvisionFlowBootstrap assembles SandboxProvisionFlow with the
// process-local resources it provisions through, the instance provider and the
// pooled workspace transport, and builds its client. The Session Agent Flow
// bootstrap owns and closes both resources; this bootstrap holds no durable
// state and serves no request.
type SandboxProvisionFlowBootstrap struct {
	flow  *SandboxProvisionFlow
	names deploymentnames.Names
}

// NewSandboxProvisionFlowBootstrap assembles the Flow; dependency reports its
// outcomes to the Sessions.
func NewSandboxProvisionFlowBootstrap(provider sandbox.InstanceProvider, workspaces *workspace.Capabilities, names deploymentnames.Names, dependency SandboxProvisionFlowDependency) *SandboxProvisionFlowBootstrap {
	return &SandboxProvisionFlowBootstrap{flow: &SandboxProvisionFlow{provider: provider, workspace: workspaces, dependency: dependency}, names: names}
}

// Flow returns the shared definition registered by the server bootstrap.
func (bootstrap *SandboxProvisionFlowBootstrap) Flow() *SandboxProvisionFlow { return bootstrap.flow }

// Client binds the Flow client to the shared Dex client and the engine status
// reader.
func (bootstrap *SandboxProvisionFlowBootstrap) Client(sdk *dex.Client, execution dexpb.FlowServiceClient) *SandboxProvisionFlowClient {
	return newSandboxProvisionFlowClient(sdk, bootstrap.flow, execution, bootstrap.names)
}
