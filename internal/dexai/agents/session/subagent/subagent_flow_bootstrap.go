// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package subagent

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
)

// SubagentFlowConfig is the process configuration of the subagents.
type SubagentFlowConfig struct {
	// Claude is the Claude API client; Mock the client of the in-process mock
	// model, nil when the server does not offer it. A subagent runs on the
	// model its Session ran on when it started.
	Claude modelprovider.ClaudeClientConfig
	Mock   *modelprovider.ClaudeClientConfig
	// Settings are the compaction threshold and the thinking behavior of every
	// segment; their effort is unused.
	Settings agentloop.ModelSettings
	// Efforts is the base effort of each subagent kind; it names every kind.
	Efforts map[agenttasks.SubagentKind]conversation.Effort
}

// SubagentFlowBootstrap assembles SubagentFlow with its process-local
// resources: its own model provider transport and a Sandbox tool runner over
// the pooled workspace capabilities, which the Session's bootstrap owns and
// closes. It holds no durable state and serves no request.
type SubagentFlowBootstrap struct {
	flow       *SubagentFlow
	httpClient *http.Client
}

// NewSubagentFlowBootstrap assembles the Flow; workspaces are the Session's
// shared capabilities, which it borrows and never closes.
func NewSubagentFlowBootstrap(config SubagentFlowConfig, dependency SubagentFlowDependency, diagnostics ProjectDiagnosticsReader, workspaces *workspace.Capabilities, logger *slog.Logger) (*SubagentFlowBootstrap, error) {
	for _, kind := range agenttasks.SubagentKinds {
		if _, isConfigured := config.Efforts[kind]; !isConfigured {
			panic(fmt.Sprintf("the subagent configuration names no effort of the kind %s", kind))
		}
	}
	httpClient := modelprovider.NewProviderHTTPClient()
	model, err := modelprovider.NewModelRouter(httpClient, config.Claude, config.Mock, logger)
	if err != nil {
		return nil, err
	}
	flow := &SubagentFlow{dependency: dependency, repository: workspaces, modelClient: model, diagnostics: diagnostics,
		sandbox: sandboxtools.NewRunner(workspaces.SandboxResolver(), workspaces.Git()), settings: config.Settings, efforts: config.Efforts}
	return &SubagentFlowBootstrap{flow: flow, httpClient: httpClient}, nil
}

// Flow returns the shared definition registered by the server bootstrap.
func (bootstrap *SubagentFlowBootstrap) Flow() *SubagentFlow { return bootstrap.flow }

// Client binds the Flow client to the shared Dex client.
func (bootstrap *SubagentFlowBootstrap) Client(sdk *dex.Client, execution dexpb.FlowServiceClient) *SubagentFlowClient {
	return newSubagentFlowClient(sdk, bootstrap.flow, execution)
}

// Close releases the model transport; the workspace capabilities belong to
// the Session's bootstrap.
func (bootstrap *SubagentFlowBootstrap) Close() error {
	bootstrap.httpClient.CloseIdleConnections()
	return nil
}
