// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package infra

import (
	"context"
	"regexp"

	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"google.golang.org/grpc"
)

// The project infrastructure is consumed through five roles and Redact. Each
// consumer holds only the roles it uses; one implementation, the Kubernetes
// runtime in package kube, provides all of them. A method returns ErrPending
// while a resource is still converging, and the owning Flow calls again.

// Storage is a project's configuration storage: its namespace, its
// versioned bucket and the objects the Connector SDK keeps there.
type Storage interface {
	// EnsureStorage converges the project's boundary and storage backend and
	// returns the references it reached, also with an error, so partially
	// provisioned resources stay owned for cleanup. A reference the call does
	// not reach stays the accepted one.
	EnsureStorage(ctx context.Context, project string, accepted StorageRefs) (StorageRefs, error)
	// Complete reports whether every storage identity of refs is recorded.
	Complete(refs StorageRefs) bool
	// VerifyProjectStorage checks the accepted references and that the
	// boundary is still the accepted one.
	VerifyProjectStorage(ctx context.Context, project string, storage StorageRefs) error
	// LiveStorage are the references of the project's Live storage from the
	// boundary UID and bucket a Flow pinned. A Preview reads its configuration
	// there.
	LiveStorage(project, namespaceUID, bucket string) StorageRefs
	// ReadConfiguration reads the exact accepted version of the scope's
	// configuration head.
	ReadConfiguration(ctx context.Context, scope ScopeRef, ref objectstore.ObjectRef) ([]byte, error)
	// ConfigurationObjects is the Connector SDK object store of the scope,
	// confined to the scope's own configuration prefix.
	ConfigurationObjects(ctx context.Context, scope ScopeRef) (connectorconfig.ObjectStore, error)
	// S3 returns the verified bucket of a scope, which holds the application's
	// runtime objects.
	S3(ctx context.Context, project, namespaceUID string) (*ApplicationObjects, error)
}

// Execution is a project's Live execution environment: its Temporal namespace
// and its Dex.
type Execution interface {
	// EnsureTemporal converges the project's Temporal namespace and the
	// credential that reaches it, and ends with one data-plane check: an
	// answer other than DataPlaneReady is ErrPending, and a credential only an
	// operator can repair is ErrTemporalCredential.
	EnsureTemporal(ctx context.Context, project string, storage StorageRefs, accepted ExecutionRefs) (ExecutionRefs, error)
	// CheckTemporalDataPlane asks the data plane of the accepted Temporal
	// access once, changing nothing.
	CheckTemporalDataPlane(ctx context.Context, project string, storage StorageRefs, accepted ExecutionRefs) (DataPlaneAnswer, error)
	// EnsureExecutionAccess converges what the project's Dex and applications
	// need to run on its accepted storage.
	EnsureExecutionAccess(ctx context.Context, project string, storage StorageRefs) error
	// StartProjectDex converges the project's Live Dex on its accepted storage
	// and Temporal access.
	StartProjectDex(ctx context.Context, project string, storage StorageRefs, accepted ExecutionRefs) (ExecutionRefs, error)
}

// Grants gives one workload generation, a Live deployment's or the project's
// Preview, access to the project's storage.
type Grants interface {
	// IssueWorkload grants the generation read access to its accepted
	// configuration, its head and its secrets and write access to its scope's
	// Trigger inboxes. A revoked generation is never issued again: its
	// identity stays as a fence, or the role it shares records it.
	IssueWorkload(ctx context.Context, workload Workload) error
	// GrantEnvironment reconciles a current grant and returns the process
	// environment through which the workload reaches its storage and
	// configuration.
	GrantEnvironment(ctx context.Context, workload Workload) (map[string]string, error)
	// RevokeWorkload permanently disables the generation's storage identity.
	RevokeWorkload(ctx context.Context, workload Workload) error
}

// Cleanup removes what a project and its Preview left in the infrastructure.
type Cleanup interface {
	// DestroyScope removes the Live scope's infrastructure once its owning Flow
	// has stopped its workloads.
	DestroyScope(ctx context.Context, project string, refs ScopeRefs) error
	// DestroyPreviewConfiguration deletes every object version below the
	// project's Preview prefix in the Live storage.
	DestroyPreviewConfiguration(ctx context.Context, project string, storage StorageRefs) error
	// DestroyPreviewGrants deletes every storage grant issued to the project's
	// Preview, revoked fences included. principalNamespace is the Sandbox
	// boundary its grants were bound to.
	DestroyPreviewGrants(ctx context.Context, project string, storage StorageRefs, principalNamespace string) error
}

// Connections opens the private connections of the platform server to the
// workloads of a project and to its Sandbox: no browser endpoint, credential
// or caller dial option is used.
type Connections interface {
	// DialProjectDex opens one private gRPC connection to the project's Live
	// Dex after verifying the accepted references and Dex readiness. The
	// returned function closes the connection, and so does the end of ctx.
	DialProjectDex(ctx context.Context, project string, refs ScopeRefs) (*grpc.ClientConn, func() error, error)
	// CheckWorker proves the current ownership of a Live generation's
	// application Worker and that its gRPC endpoint accepts a connection.
	CheckWorker(ctx context.Context, target WorkerTarget) error
	// DialSandboxDex opens one private gRPC connection to the Preview Dex of a
	// Sandbox instance after verifying the recorded identities of the instance.
	DialSandboxDex(ctx context.Context, target SandboxDexTarget) (*grpc.ClientConn, func() error, error)
	// ProbeSandboxDex runs the checks of DialSandboxDex without keeping the
	// connection.
	ProbeSandboxDex(ctx context.Context, target SandboxDexTarget) error
	// WorkerAddress is the address at which the project's Dex reaches the Worker
	// of the current Live generation: LiveWorkerEndpoint on the Worker port.
	WorkerAddress(project string) string
}

// Redactor replaces the project's internal resource names in text shown
// outside the platform, such as a failure message, with placeholders.
type Redactor interface {
	Redact(project, text string) string
}

// Redaction replaces every match of an internal resource name with a
// placeholder.
type Redaction struct {
	Name        *regexp.Regexp
	Placeholder string
}

// workloadNames match the generation names WorkloadName derives, which also
// prefix their credentials.
var workloadNames = regexp.MustCompile(`\bapp-[a-z0-9](?:[a-z0-9-]*[a-z0-9])?-[0-9]+\b`)

// ScopeRedactions name the internal resources every runtime gives a
// project's Live scope: the scope itself, a namespace or a network, the
// workload generations, and the Temporal namespace of a local backend.
func ScopeRedactions(scope, project string) []Redaction {
	return []Redaction{
		{Name: regexp.MustCompile(`\b` + regexp.QuoteMeta(scope) + `\b`), Placeholder: "<project-namespace>"},
		{Name: workloadNames, Placeholder: "<workload>"},
		{Name: regexp.MustCompile(`\b` + regexp.QuoteMeta(LocalTemporalNamespace(project)) + `\b`), Placeholder: "<temporal-namespace>"},
	}
}

// Redact replaces every match of each redaction in text, in order.
func Redact(text string, redactions []Redaction) string {
	for _, redaction := range redactions {
		text = redaction.Name.ReplaceAllLiteralString(text, redaction.Placeholder)
	}
	return text
}

// WorkerTarget contains only typed references read from the authorized current
// Live Deployment. It cannot select a URL, port, or health bypass.
type WorkerTarget struct {
	ProjectID                      string
	Namespace, Controller, Service ResourceRef
	DeploymentID                   string
	Generation                     uint64
}

// SandboxDexTarget names the Preview Dex inside one Sandbox instance by the
// identities its provisioning recorded. It cannot select a URL or port: the
// Dex listens on DexPort behind the Sandbox's own Service.
type SandboxDexTarget struct {
	ProjectID, WorkspaceRequestID string
	Generation                    uint64
	Namespace, Sandbox, Service   ResourceRef
}
