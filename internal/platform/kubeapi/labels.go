// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kubeapi

import "github.com/superdurable/superversev2/internal/platform/deploymentnames"

// The label keys that mark who owns an object of the platform. The providers
// stamp them and verify them before they adopt or remove an object. The
// Docker runtime marks its objects with the same keys, which
// deploymentnames declares once for both.
const (
	ManagedByLabel        = deploymentnames.ManagedByLabel
	PartOfLabel           = deploymentnames.PartOfLabel
	ProjectLabel          = deploymentnames.ProjectLabel
	ComponentLabel        = deploymentnames.ComponentLabel
	ScopeLabel            = deploymentnames.ScopeLabel
	WorkspaceRequestLabel = deploymentnames.WorkspaceRequestLabel
	SandboxProfileLabel   = deploymentnames.SandboxProfileLabel
	WorkloadIDLabel       = deploymentnames.WorkloadIDLabel
	GenerationLabel       = deploymentnames.GenerationLabel
)
