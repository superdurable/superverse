// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/sandboxd"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
)

// SessionAgentConfig is the process configuration of the Session Agent.
type SessionAgentConfig struct {
	Compatible *modelprovider.CompatibleProviders
	// Model is the model a new Session starts with.
	Model conversation.Model
	// Claude selects the official adapter configuration; Session requests
	// provide their own active credential.
	Claude modelprovider.ClaudeClientConfig
	// Mock is the client of the in-process mock model; nil when the server
	// configuration does not offer it.
	Mock *modelprovider.MockClientConfig
	// Settings are the base effort, the compaction threshold, the
	// thinking prefix-mismatch behavior and the attachment budget of every
	// segment.
	Settings agentloop.ModelSettings
	// AutomaticTurns bound the consecutive turns that task notifications and
	// application-owned delivery failures open before automatic continuation
	// pauses until the user replies.
	AutomaticTurns agentloop.AutomaticTurnBounds
	// SandboxHold is how long the Sandbox stays after the main agent or a
	// Preview command used it; at the deadline the Session has the
	// SessionWorkspaceFlow reclaim the idle Pod, keeping its volume.
	SandboxHold time.Duration
	// PreviewHold is how long the Sandbox stays after a Preview became READY
	// on it.
	PreviewHold time.Duration
	// ConnectorLibrary is where the develop subagents' contributions go, and
	// ModuleRedirects the lane's redirects of the Sandbox tool environment.
	ConnectorLibrary git.ConnectorLibrary
	ModuleRedirects  []sandboxd.ModuleRedirect
}

// SessionAgentFlowBootstrap assembles SessionAgentFlow with its process-local
// resources: the model provider transport, the Sandbox tool runner, the shared
// workspace capabilities (pooled Sandbox connections and repository
// operations) and the instance provider that runs on them, which the
// SessionWorkspaceFlow reclaims through. One owner constructs them for the
// shared Dex registry and closes them. It holds no durable state and serves no request.
type SessionAgentFlowBootstrap struct {
	flow       *SessionAgentFlow
	model      conversation.Model
	workspace  *workspace.Capabilities
	instances  sandbox.InstanceProvider
	httpClient *http.Client
}

// NewSessionAgentFlowBootstrap assembles the Flow; instances builds the
// instance provider on the workspace capabilities it creates, and renditions
// renders the attachment references of its model requests.
func NewSessionAgentFlowBootstrap(config SessionAgentConfig, credentials *git.HostCredentials, dependency SessionAgentFlowDependency, diagnostics ProjectDiagnosticsReader,
	catalog connectorCatalogMatcher, renditions attachmentRenditions, addresses workspace.SandboxAddresses, instances func(*workspace.Capabilities) sandbox.InstanceProvider, logger *slog.Logger,
) (*SessionAgentFlowBootstrap, error) {
	httpClient := modelprovider.NewProviderHTTPClient()
	model, err := modelprovider.NewModelRouter(httpClient, config.Claude, config.Mock, logger, config.Compatible)
	if err != nil {
		return nil, err
	}
	workspaces := workspace.NewCapabilities(credentials, config.ConnectorLibrary, addresses, config.ModuleRedirects...)
	provider := instances(workspaces)
	tools := sandboxtools.NewRunner(workspaces.SandboxResolver(), workspaces.Git())
	return &SessionAgentFlowBootstrap{
		flow:       newSessionAgentFlow(model, tools, diagnostics, dependency, workspaces, catalog, renditions, config.AutomaticTurns, config.SandboxHold, config.PreviewHold, config.Settings),
		model:      config.Model,
		workspace:  workspaces,
		instances:  provider,
		httpClient: httpClient,
	}, nil
}

// newSessionAgentFlow constructs the Flow with every mandatory dependency and
// the server's model settings.
func newSessionAgentFlow(modelClient conversation.ModelClient, sandbox *sandboxtools.Runner, diagnostics ProjectDiagnosticsReader, dependency SessionAgentFlowDependency, repository repositoryContextReader,
	catalog connectorCatalogMatcher, renditions attachmentRenditions, automaticTurns agentloop.AutomaticTurnBounds, sandboxHold, previewHold time.Duration, settings agentloop.ModelSettings,
) *SessionAgentFlow {
	return &SessionAgentFlow{modelClient: modelClient, sandbox: sandbox, diagnostics: diagnostics, dependency: dependency, repository: repository, catalog: catalog, renditions: renditions,
		automaticTurns: automaticTurns, sandboxHold: sandboxHold, previewHold: previewHold, settings: settings}
}

// Flow returns the shared definition registered by the server bootstrap.
func (bootstrap *SessionAgentFlowBootstrap) Flow() *SessionAgentFlow { return bootstrap.flow }

// Client binds the Flow client to the shared Dex client, with the model a new
// Session starts with.
func (bootstrap *SessionAgentFlowBootstrap) Client(sdkClient *dex.Client, execution dexpb.FlowServiceClient) *SessionAgentFlowClient {
	return newSessionAgentFlowClient(sdkClient, bootstrap.flow, execution, bootstrap.model)
}

// Workspace returns the shared capabilities whose connections Close releases.
func (bootstrap *SessionAgentFlowBootstrap) Workspace() *workspace.Capabilities {
	return bootstrap.workspace
}

// Instances returns the instance provider on the shared capabilities, which the
// workspace Flow, the provisioning, the Preview and the deleting Project's
// release use.
func (bootstrap *SessionAgentFlowBootstrap) Instances() sandbox.InstanceProvider {
	return bootstrap.instances
}

func (bootstrap *SessionAgentFlowBootstrap) Close() error {
	bootstrap.httpClient.CloseIdleConnections()
	return bootstrap.workspace.Close()
}
