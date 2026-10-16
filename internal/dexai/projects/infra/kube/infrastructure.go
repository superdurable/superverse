// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// Infrastructure is the Kubernetes runtime of the projects' Live scopes, which
// bootstrap builds from settings it validated. It provisions, reaches and
// removes a project's namespace, Dex, storage and Temporal, and grants its
// workloads the storage, for every environment; the Backend supplies what the
// environments differ in. The Project's Preview owns no storage of its own:
// its configuration and storage grants live in the Project's Live storage.
type Infrastructure struct {
	API         *kubeapi.Client
	Environment deploymentnames.Environment
	DexImage    string
	// Dial connects to the address of an in-cluster Service. It is nil in the
	// server, which runs in the cluster and dials the address itself;
	// integration tests on the host set it to reach the cluster's Services.
	Dial func(ctx context.Context, address string) (net.Conn, error)
	// Backend is the environment of the project infrastructure; bootstrap
	// never sets it to a typed nil.
	Backend Backend
}

// Redact replaces the project's internal resource names in text shown outside
// the platform, such as a failure message, with placeholders: its namespace,
// its workload generations, its Local Kind Temporal namespace and the names
// of the Backend, such as the cloud names and the bucket a cloud account
// derives. Redact owns the list: a change that introduces another internal
// name adds its pattern to infra.ScopeRedactions or to the Backend's
// RedactionPatterns.
func (k *Infrastructure) Redact(project, text string) string {
	return infra.Redact(text, append(infra.ScopeRedactions(k.ProjectNamespace(project), project), k.Backend.RedactionPatterns(k, project)...))
}

func (k *Infrastructure) Names() deploymentnames.Names { return deploymentnames.MustFor(k.Environment) }

// ProjectNamespace is the namespace of the project's Live scope.
func (k *Infrastructure) ProjectNamespace(project string) string {
	return k.Names().ProjectNamespace(project)
}

// ResourceReference returns the identity of an observed object.
func ResourceReference(o kubeapi.Object) infra.ResourceRef {
	namespace, name, uid := kubeapi.Ref(o)
	return infra.ResourceRef{Boundary: namespace, Name: name, UID: uid}
}

// PreviewLabels mark the objects of a project's Preview scope: its Sandbox
// objects and the credentials of its grants.
func PreviewLabels(project string) kubeapi.Object {
	return kubeapi.Object{kubeapi.ProjectLabel: project, kubeapi.ScopeLabel: infra.ScopePreview.String()}
}

// Component is a part of a project's Live scope, which infra declares. Its
// owner labels mark the resources of the part, and a part that the scope runs
// itself, a Local Kind backend or the project Dex, names its service account,
// Deployment and Service after itself.
type Component = infra.Component

// The components of a project's Live scope.
var (
	ComponentInfrastructure = infra.ComponentInfrastructure
	ComponentStorage        = infra.ComponentStorage
	ComponentTemporal       = infra.ComponentTemporal
	ComponentDex            = infra.ComponentDex
	ComponentApplication    = infra.ComponentApplication
	ComponentRoute          = infra.ComponentRoute
)

// LabelSelectorForTest selects the component's resources by their component label.
func LabelSelectorForTest(c Component) string { return kubeapi.ComponentLabel + "=" + c.String() }

// OwnerLabels mark a resource as one component of a project's infrastructure.
func (k *Infrastructure) OwnerLabels(project string, component Component) kubeapi.Object {
	return kubeapi.Object{kubeapi.ManagedByLabel: k.Names().Product, kubeapi.PartOfLabel: k.Names().Product + "-live", kubeapi.ProjectLabel: project, kubeapi.ComponentLabel: component.String()}
}

// Metadata names a component resource in the project's Live namespace.
func (k *Infrastructure) Metadata(project, name string, component Component) kubeapi.Object {
	return kubeapi.Metadata(k.ProjectNamespace(project), name, k.OwnerLabels(project, component))
}

// VerifyOwner rejects an object that is not the project component's own: it
// must carry every owner label and no scope label.
func (k *Infrastructure) VerifyOwner(o kubeapi.Object, project string, component Component) error {
	return VerifyLabels(o, k.OwnerLabels(project, component))
}

// VerifyLabels requires every wanted label on o, and a scope label exactly
// when want names one, so a Preview's resource is never adopted as a Live one
// or the reverse.
func VerifyLabels(o, want kubeapi.Object) error {
	labels := kubeapi.Labels(o)
	owned := kubeapi.UID(o) != "" && kubeapi.String(labels, kubeapi.ScopeLabel) == kubeapi.String(want, kubeapi.ScopeLabel)
	for key, value := range want {
		owned = owned && labels[key] == value
	}
	if !owned {
		return fmt.Errorf("%w: resource labels", infra.ErrOwnershipConflict)
	}
	return nil
}

// GetOwned reads the object at path and returns it only when it is the
// component's own; an absent object is nil, and the caller decides what
// absence means.
func (k *Infrastructure) GetOwned(ctx context.Context, path, project string, component Component) (kubeapi.Object, error) {
	o, code, err := k.API.Call(ctx, http.MethodGet, path, nil)
	if err != nil || code == http.StatusNotFound {
		return nil, err
	}
	if err = k.VerifyOwner(o, project, component); err != nil {
		return nil, err
	}
	return o, nil
}

// NamespacePath is the core API path of the project's Live namespace.
func (k *Infrastructure) NamespacePath(project string) string {
	return "/api/v1/namespaces/" + k.ProjectNamespace(project)
}
