// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"context"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	sandboxdocker "github.com/superdurable/superversev2/internal/dexai/agents/sandbox/docker"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	deploymentsdocker "github.com/superdurable/superversev2/internal/dexai/delivery/deployments/docker"
	infradocker "github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
)

// newDockerProviders serves project storage, Temporal, Dex and grants, the
// Live applications and the Sandbox instances as containers of the local
// Docker Engine with the local backends, which Local Native runs, and serves
// the gateway its routes. The engine's connections are released through
// owned.
func newDockerProviders(_ context.Context, config serverConfig, owned *resources) (*projectProviders, error) {
	infrastructure, err := infradocker.New(*config.docker)
	if err != nil {
		return nil, err
	}
	owned.add(infrastructure.Close)
	instances := &sandboxdocker.InstanceProvider{Infrastructure: infrastructure, ProfileName: config.sandboxProfileName, ProfilePath: config.sandboxProfilePath, Connections: infrastructure}
	return &projectProviders{
		storage: infrastructure, execution: infrastructure, grants: infrastructure, cleanup: infrastructure, redactor: infrastructure,
		connections: infrastructure, serving: &deploymentsdocker.LiveServing{Infrastructure: infrastructure},
		sandboxAddresses: instances.SandboxAddresses,
		instances: func(workspaces *workspace.Capabilities) sandbox.InstanceProvider {
			instances.Workspace = workspaces
			return instances
		},
		routes: infrastructure.Routes(),
	}, nil
}
