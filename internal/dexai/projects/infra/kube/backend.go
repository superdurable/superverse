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

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"google.golang.org/grpc"
)

// Backend is what differs between the environments the Kubernetes runtime
// serves. A project's Live scope is the same set of Kubernetes objects in its
// own namespace everywhere, but its storage, its Temporal and the way a
// workload reaches them are not: Local Kind runs a MinIO and a Temporal
// start-dev in the namespace (package local), hosted environments use an S3
// bucket, a Temporal Cloud namespace and IAM roles of the project cloud account
// (package hosted). The runtime asks the Backend wherever the environments
// differ and holds no other environment check.
//
// Every method that builds on Kubernetes receives the Infrastructure whose
// objects it manages. A Backend holds no state of a project.
type Backend interface {
	// Storage.

	// Bucket names the project's storage bucket. Hosted, the name derives from
	// the namespace UID, so a replaced namespace never reaches the old bucket.
	Bucket(k *Infrastructure, project, namespaceUID string) string
	// BucketBoundToNamespace reports whether Bucket derives the name from the
	// namespace UID, which S3 then verifies is still the accepted one before it
	// opens the bucket.
	BucketBoundToNamespace() bool
	// Objects returns the verified bucket of a project whose namespace the
	// caller has verified.
	Objects(ctx context.Context, k *Infrastructure, project, namespaceUID string) (*infra.ApplicationObjects, error)
	// EnsureStorage converges the storage backend on the project's namespace.
	// It returns the reference of the Service that runs the storage, which
	// only an environment that runs it has, also while the storage becomes
	// ready; acceptedServiceUID is the one an earlier call recorded.
	EnsureStorage(ctx context.Context, k *Infrastructure, project, namespaceUID, acceptedServiceUID string) (infra.ResourceRef, error)
	// StorageComplete reports whether refs holds the identity of the Service
	// that runs the storage, if the environment runs one.
	StorageComplete(refs infra.StorageRefs) bool
	// ExtraEgress is the egress a component's network policy admits beyond the
	// traffic every environment shares.
	ExtraEgress(k *Infrastructure, component Component) []any

	// Execution.

	// TemporalNamespace names the project's Temporal namespace.
	TemporalNamespace(k *Infrastructure, project string) string
	// EnsureTemporal converges the project's Temporal namespace, named
	// namespace, and the credential that reaches it, and returns the access
	// with the reference of the credential as soon as it exists. credential
	// is the one the owning Flow accepted. An accepted credential that is gone
	// or replaced is infra.ErrTemporalCredential.
	EnsureTemporal(ctx context.Context, k *Infrastructure, project string, storage infra.StorageRefs, namespace string, credential infra.ResourceRef) (infra.TemporalAccess, infra.ResourceRef, error)
	// ReadTemporalAccess reads the access of the accepted namespace and
	// credential without changing anything.
	ReadTemporalAccess(ctx context.Context, k *Infrastructure, project string, storage infra.StorageRefs, namespace string, credential infra.ResourceRef) (infra.TemporalAccess, error)
	// DialTemporal opens the connection of one data-plane check. The context it
	// returns carries the call credentials; the function closes the connection.
	DialTemporal(ctx context.Context, k *Infrastructure, project string, storage infra.StorageRefs, credential infra.ResourceRef, access infra.TemporalAccess) (context.Context, *grpc.ClientConn, func() error, error)
	// SharedApplicationServiceAccount reports whether every Live generation
	// runs as the one application service account that EnsureExecutionAccess
	// creates, rather than as an account of its own that its grant creates
	// and its cleanup removes.
	SharedApplicationServiceAccount() bool
	// EnsureDexIdentity converges the identity the project Dex assumes to
	// reach the storage, if the environment needs one.
	EnsureDexIdentity(ctx context.Context, k *Infrastructure, project string, storage infra.StorageRefs) error
	// DexSpec is the project Dex on the project's storage and Temporal access.
	DexSpec(ctx context.Context, k *Infrastructure, project string, storage infra.StorageRefs, access infra.TemporalAccess) (ProjectDexSpec, error)

	// Grants.

	// IssueWorkload and RevokeWorkload grant and fence one workload
	// generation's access to the storage: a Live generation's, or a Preview's
	// bound to its Sandbox principal.
	IssueWorkload(ctx context.Context, k *Infrastructure, w infra.Workload) error
	RevokeWorkload(ctx context.Context, k *Infrastructure, w infra.Workload) error
	// GrantEnvironment issues the grant and returns the part of the workload's
	// process environment through which it reaches the storage.
	GrantEnvironment(ctx context.Context, k *Infrastructure, w infra.Workload) (map[string]string, error)
	// WorkloadServiceAccount is the service account a Live generation's Pods
	// run as.
	WorkloadServiceAccount(k *Infrastructure, w infra.Workload) string

	// Cleanup.

	// DestroyScope removes the Live scope's infrastructure, returning
	// infra.ErrPending until everything is gone.
	DestroyScope(ctx context.Context, k *Infrastructure, project string, refs infra.ScopeRefs) error
	// DestroyPreviewGrants removes every grant of the project's Preview,
	// fences included, returning infra.ErrPending until they are gone.
	// namespace is the project's Live namespace, nil when it is absent;
	// principalNamespace is the one the grants bind.
	DestroyPreviewGrants(ctx context.Context, k *Infrastructure, project string, storage infra.StorageRefs, namespace kubeapi.Object, principalNamespace string) error

	// Redaction and public routes.

	// RedactionPatterns name the project's environment-specific resources for
	// Redact.
	RedactionPatterns(k *Infrastructure, project string) []infra.Redaction
	// ApplicationIngressPeer is the peer from which the network policy of an
	// application admits traffic: nil admits any.
	ApplicationIngressPeer(k *Infrastructure) kubeapi.Object
	// ApplicationRuntimeClass is the runtime class of the application Pods, or
	// "" for the cluster's default.
	ApplicationRuntimeClass() string
	// ProbeBase is the URL through which the platform probes a route whose
	// public URL is publicURL.
	ProbeBase(routes infra.PublicRoutes, publicURL string) string
}
