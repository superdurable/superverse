// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deploymentnames

// The label keys that mark who owns a runtime object of the platform: a
// Kubernetes object, or a Docker container, network or volume. The runtimes
// stamp them and verify them before they adopt or remove an object, so both
// runtimes share one spelling of each key.
const (
	// ManagedByLabel and PartOfLabel are the Kubernetes recommended labels that
	// name who manages an object and the application it is part of.
	ManagedByLabel = "app.kubernetes.io/managed-by"
	PartOfLabel    = "app.kubernetes.io/part-of"
	// ProjectLabel and ComponentLabel carry the project and the component of the
	// project's Live scope that own an object.
	ProjectLabel   = "superdurable.io/project-id"
	ComponentLabel = "superdurable.io/component"
	// ScopeLabel marks the objects of a project's Preview scope with the
	// scope's name, preview: the Sandbox objects of its Session and the
	// credentials of its Preview grants. No Live object has one.
	ScopeLabel = "superdurable.io/scope"
	// WorkspaceRequestLabel carries the WorkspaceRequestID of the Sandbox
	// instance that owns an object.
	WorkspaceRequestLabel = "superdurable.io/workspace-request-id"
	// SandboxProfileLabel carries the coding profile of a Sandbox's objects.
	SandboxProfileLabel = "superdurable.io/sandbox-profile"
	// WorkloadIDLabel carries the workload ID: a deployment ID, or a Preview's
	// grant workload ID. Its key keeps the historical spelling of credential
	// Secrets.
	WorkloadIDLabel = "superdurable.io/deployment-id"
	// GenerationLabel carries the generation of the workload that owns an object.
	GenerationLabel = "superdurable.io/generation"
)
