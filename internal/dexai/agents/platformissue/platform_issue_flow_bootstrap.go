// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package platformissue

import (
	"log/slog"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
)

// PlatformIssueFlowBootstrap assembles PlatformIssueFlow with the issue
// repository it files in and the server's logger, and builds its client. It
// holds no durable state, owns no resource and serves no request.
type PlatformIssueFlowBootstrap struct {
	flow        *PlatformIssueFlow
	environment deploymentnames.Environment
}

// NewPlatformIssueFlowBootstrap assembles the Flow of the server's
// environment; issues is the issue repository, or githubissues.NotConfigured.
func NewPlatformIssueFlowBootstrap(issues PlatformIssueFlowDependency, environment deploymentnames.Environment, logger *slog.Logger) *PlatformIssueFlowBootstrap {
	return &PlatformIssueFlowBootstrap{flow: &PlatformIssueFlow{issues: issues, logger: logger}, environment: environment}
}

// Flow returns the shared definition registered by the server bootstrap.
func (bootstrap *PlatformIssueFlowBootstrap) Flow() *PlatformIssueFlow { return bootstrap.flow }

// Client binds the Flow client to the shared Dex client.
func (bootstrap *PlatformIssueFlowBootstrap) Client(sdk *dex.Client) *PlatformIssueFlowClient {
	return newPlatformIssueFlowClient(sdk, bootstrap.flow, bootstrap.environment)
}
