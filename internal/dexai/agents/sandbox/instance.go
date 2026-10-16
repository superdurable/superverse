// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sandbox

import (
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// InstanceGeneration is the constant generation of every instance: the
// Kubernetes label value and the API field. Nothing increments it.
const InstanceGeneration uint64 = 1

// WorkspaceState is whether the workspace volume holds uncommitted changes, as
// the checkout or the last inspection found it. An alias of string, as its
// readers store it; the constants declare the set.
type WorkspaceState = string

const (
	WorkspaceAbsent  WorkspaceState = "ABSENT"
	WorkspaceClean   WorkspaceState = "CLEAN"
	WorkspaceDirty   WorkspaceState = "DIRTY"
	WorkspaceUnknown WorkspaceState = "UNKNOWN"
)

// The safe codes of a workspace request that failed before or around its
// provisioning, which the Session and the SessionWorkspaceFlow record. A
// failed provisioning records the provisioning's own code.
const (
	// WorkspaceFailureTimeout: no outcome of the request arrived within the
	// bounded wait.
	WorkspaceFailureTimeout = "WORKSPACE_TIMEOUT"
	// WorkspaceFailureStart: the request's command or its provisioning could
	// not be started.
	WorkspaceFailureStart = "WORKSPACE_START_FAILED"
	// WorkspaceFailureReclaim: the previous instance, whose Pod may still
	// mount the volume, could not be reclaimed first.
	WorkspaceFailureReclaim = "WORKSPACE_RECLAIM_FAILED"
	// WorkspaceFailureNotProvisioned: no provisioning of the request exists.
	WorkspaceFailureNotProvisioned = "WORKSPACE_NOT_PROVISIONED"
)

// Instance is one Sandbox instance of a Project's Session as its provider
// sees it: the Project's identity, the WorkspaceRequestID that names the
// instance, its constant generation and the resources it runs in.
type Instance struct {
	Identity           Identity
	WorkspaceRequestID string
	Generation         uint64
	Resources          Resources
}

// ResourceRef identifies one object of an instance by exact identity. Its
// Boundary is persisted as Namespace.
type ResourceRef struct {
	Boundary  string `json:"Namespace"`
	Name, UID string
}

// InfraRef is r as an infrastructure reference.
func (r ResourceRef) InfraRef() infra.ResourceRef {
	return infra.ResourceRef{Boundary: r.Boundary, Name: r.Name, UID: r.UID}
}

// Repository is the Project repository a Sandbox checks out. CloneURL is where the Sandbox
// clones it from: the Project's repository host as the Sandbox reaches it.
type Repository struct {
	InstallationID, RepositoryID                     int64
	Owner, Name, CloneURL, DefaultBranch, HeadCommit string
}

// Identity is the Project whose Session an instance belongs to, with the
// repository its workspace checks out.
type Identity struct {
	ProjectID  string
	Repository Repository
}

// Resources are the objects of an instance, by exact identity: its boundary
// (the Project's Sandbox namespace), the identity its Sandbox runs as,
// which is the principal of its Preview grants, the Sandbox itself and the
// workspace volume. Its Service exposes the Preview Dex. Profile is the coding
// profile whose contract the volume's repository and Preview state follow.
// Boundary, Identity and Instance are persisted as Namespace, ServiceAccount
// and Sandbox.
type Resources struct {
	Boundary                             ResourceRef `json:"Namespace"`
	Identity                             ResourceRef `json:"ServiceAccount"`
	Instance                             ResourceRef `json:"Sandbox"`
	Volume                               ResourceRef
	ServiceName, ServiceUID, ImageDigest string
	Profile                              string
}

// Retained are the resources that outlive the Pod: the namespace and the
// workspace volume with its profile. The next instance adopts them.
func (r Resources) Retained() Retained {
	return Retained{Resources: Resources{Boundary: r.Boundary, Volume: r.Volume, Profile: r.Profile}}
}

// WithoutPod are the resources once the instance's Sandbox and its Pods are
// gone: the namespace, the service account and the volume remain.
func (r Resources) WithoutPod() Resources {
	r.Instance, r.ServiceName, r.ServiceUID = ResourceRef{}, "", ""
	return r
}

// Retained are the references that outlive one instance: the Namespace and
// Volume references with the volume's coding profile. The Project's Preview
// data lives in its Live storage under keys that infra.PreviewKeysOf derives,
// so no reference to it is retained.
type Retained struct {
	Resources Resources // Namespace, Volume and Profile only
}

// DexTarget names the Preview Dex inside the instance by the identities it
// recorded.
func (s Instance) DexTarget() infra.SandboxDexTarget {
	r := s.Resources
	return infra.SandboxDexTarget{
		ProjectID: s.Identity.ProjectID, WorkspaceRequestID: s.WorkspaceRequestID, Generation: s.Generation,
		Namespace: r.Boundary.InfraRef(), Sandbox: r.Instance.InfraRef(),
		Service: infra.ResourceRef{Boundary: r.Boundary.Name, Name: r.ServiceName, UID: r.ServiceUID},
	}
}

// Binding is the workspace transport binding of the instance.
func (s Instance) Binding() workspace.Binding {
	return workspace.Binding{
		ProjectID: s.Identity.ProjectID, FlowID: flowid.SessionFlowID(s.Identity.ProjectID),
		Generation:      int64(s.Generation), //nolint:gosec // the constant generation 1 of every instance
		SandboxBoundary: s.Resources.Boundary.Name, SandboxServiceName: s.Resources.ServiceName,
		SandboxUID: s.Resources.Instance.UID, WorkspaceID: s.Resources.Volume.UID,
	}
}

// WorkspaceRepository is the repository of the Project as the workspace
// checks it out, expecting head.
func (id Identity) WorkspaceRepository(head string) git.Repository {
	return git.Repository{
		ProjectID: id.ProjectID, InstallationID: id.Repository.InstallationID, RepositoryID: id.Repository.RepositoryID,
		Owner: id.Repository.Owner, Name: id.Repository.Name, CloneURL: id.Repository.CloneURL, DefaultBranch: id.Repository.DefaultBranch, ExpectedHeadSHA: head,
	}
}
