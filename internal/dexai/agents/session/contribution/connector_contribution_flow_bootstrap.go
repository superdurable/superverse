// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package contribution

import "github.com/superdurable/dex/sdk-go/dex"

// ConnectorContributionFlowBootstrap assembles ConnectorContributionFlow with
// the Session Service it reports through and the connectors library that
// opens its pull requests and publishes its release candidates. It holds no
// durable state and serves no request.
type ConnectorContributionFlowBootstrap struct {
	flow *ConnectorContributionFlow
}

// NewConnectorContributionFlowBootstrap assembles the Flow.
func NewConnectorContributionFlowBootstrap(dependency ConnectorContributionFlowDependency, pullRequests ConnectorPullRequests,
	releaseCandidates ReleaseCandidates, checks ConnectorChecks, redactor ConnectorCheckRedactor) *ConnectorContributionFlowBootstrap {
	return &ConnectorContributionFlowBootstrap{flow: &ConnectorContributionFlow{dependency: dependency, pullRequests: pullRequests, releaseCandidates: releaseCandidates, checks: checks, redactor: redactor}}
}

// Flow returns the shared definition registered by the server bootstrap.
func (bootstrap *ConnectorContributionFlowBootstrap) Flow() *ConnectorContributionFlow {
	return bootstrap.flow
}

// Client binds the Flow client to the shared Dex client.
func (bootstrap *ConnectorContributionFlowBootstrap) Client(sdk *dex.Client) *ConnectorContributionFlowClient {
	return newConnectorContributionFlowClient(sdk, bootstrap.flow)
}
