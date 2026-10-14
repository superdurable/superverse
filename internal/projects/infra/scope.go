// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package infra

import (
	"errors"
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// ScopeKind is the environment a Scope names, spelled as configuration keys,
// the workload environment and the connector SDK spell it.
type ScopeKind struct{ name string }

var (
	// ScopeLive is a project's Live environment.
	ScopeLive = ScopeKind{name: "live"}
	// ScopePreview is the project's Preview: its Session's Sandbox and the
	// application the Preview serves there.
	ScopePreview = ScopeKind{name: "preview"}
)

// String is the spelling of the scope.
func (kind ScopeKind) String() string { return kind.name }

// ParseScopeKind is the ScopeKind that name spells, as a stored row names it.
func ParseScopeKind(name string) (ScopeKind, bool) {
	for _, kind := range []ScopeKind{ScopeLive, ScopePreview} {
		if kind.name == name {
			return kind, true
		}
	}
	return ScopeKind{}, false
}

// Scope is a project's Live environment or its Preview. A project has one of
// each.
type Scope struct {
	Kind ScopeKind
}

// ScopeOf is the Scope that a business API target names: the one mapping of
// the ProjectScope enum. Any value other than Preview or Live yields a Scope
// that Validate rejects.
func ScopeOf(scope pb.ProjectScope) Scope {
	switch scope {
	case pb.ProjectScope_PROJECT_SCOPE_PREVIEW:
		return Scope{Kind: ScopePreview}
	case pb.ProjectScope_PROJECT_SCOPE_LIVE:
		return Scope{Kind: ScopeLive}
	default:
		return Scope{}
	}
}

// ProjectScopeOf is the ProjectScope enum value of a scope's spelling, the
// inverse of ScopeOf; any other spelling is UNSPECIFIED.
func ProjectScopeOf(name string) pb.ProjectScope {
	switch name {
	case ScopePreview.name:
		return pb.ProjectScope_PROJECT_SCOPE_PREVIEW
	case ScopeLive.name:
		return pb.ProjectScope_PROJECT_SCOPE_LIVE
	default:
		return pb.ProjectScope_PROJECT_SCOPE_UNSPECIFIED
	}
}

// Validate admits a project and one of its scopes. Scopes derived from
// requests pass it before any effect.
func (s Scope) Validate(project string) error {
	if !flowid.IsLowerAlphanumeric(project) || len(project) > 32 || !s.valid() {
		return errors.New("invalid project scope")
	}
	return nil
}

func (s Scope) valid() bool {
	return s.Kind == ScopePreview || s.Kind == ScopeLive
}

// Component is a part of a project's Live scope. Its label value marks the
// runtime objects of the part, and a part the scope runs itself, a local
// storage or Temporal or the project Dex, names its objects and the host name
// under which the scope reaches it after itself.
type Component struct{ name string }

// The components of a project's Live scope.
var (
	ComponentInfrastructure = Component{name: "infrastructure"}
	ComponentStorage        = Component{name: "storage"}
	ComponentTemporal       = Component{name: "temporal"}
	ComponentDex            = Component{name: "dex"}
	ComponentApplication    = Component{name: "application"}
	ComponentRoute          = Component{name: "route"}
)

// String is the label value and the name of the component.
func (c Component) String() string { return c.name }

// StorageRefs are the accepted references of a project's configuration
// storage. Setup discovers them and ProjectFlow owns them; configuration, Live
// workload grants and Preview grants need only them.
type StorageRefs struct {
	// Boundary is the project's isolation boundary, its Kubernetes namespace.
	// Its UID names the hosted bucket and binds every cloud tag, so a replaced
	// boundary never reaches them. It is persisted as Namespace.
	Boundary      ResourceRef `json:"Namespace"`
	StorageBucket string
	BlobPrefix    string
	// Storage is the Local Kind MinIO Service; hosted storage has none.
	Storage ResourceRef
}

// ExecutionRefs are the accepted references of a project's Live execution
// environment: its Temporal namespace, the credential that reaches it and the
// project Dex. The first Live deployment discovers them and ProjectFlow owns
// them.
type ExecutionRefs struct {
	// TemporalNamespace is app-<project> in Local Kind, and hosted the
	// project's Temporal Cloud namespace.
	TemporalNamespace string
	// TemporalCredential is the Local Kind Temporal Service, and hosted the
	// Secret that holds the API key, whose Generation counts its rotations;
	// only the next generation in the same boundary may replace it.
	TemporalCredential ResourceRef
	DexService         ResourceRef
}

// Complete reports whether every execution identity is recorded.
func (r ExecutionRefs) Complete() bool {
	return r.TemporalNamespace != "" && r.TemporalCredential.UID != "" && r.DexService.UID != ""
}

// ScopeRefs are the accepted references of a project's whole Live scope, which
// reaching its Dex and destroying the scope need.
type ScopeRefs struct {
	Storage   StorageRefs
	Execution ExecutionRefs
}

// ScopeRef is one configuration scope of a project with the storage its
// owning Flow accepted.
type ScopeRef struct {
	ProjectID string
	Scope     Scope
	Storage   StorageRefs
}

// ResourceRef identifies one accepted object of the runtime, and for a serving
// generation also its generation. Boundary is the isolation boundary the object
// lives in, a Kubernetes namespace, and is empty for a boundary itself. It is
// persisted as Namespace.
type ResourceRef struct {
	Boundary   string `json:"Namespace"`
	Name, UID  string
	Generation uint64
}

// ErrPending reports a resource that is still converging; the owning Flow
// calls again later.
var ErrPending = errors.New("project infrastructure is still reconciling")

// ErrOwnershipConflict retains cleanup responsibility until an operator resolves
// a different owner or UID. It never authorizes adopting the replacement.
var ErrOwnershipConflict = errors.New("project resource ownership conflict")

// ErrAccessDenied rejects a configuration object outside the accepted scope.
var ErrAccessDenied = errors.New("project configuration access denied")

// ErrTemporalCredential reports an accepted Temporal credential that only an
// operator can repair: absent, replaced, unlisted, disabled or owned by
// another identity. An expiring key is rotated, not refused. Waiting does not
// change it.
var ErrTemporalCredential = errors.New("project Temporal credential requires operator rotation")

// ErrGrantTooLarge reports a workload grant whose policy exceeds its
// provider's size limit because the scope's configuration names too many
// secrets. Waiting does not change it; the configuration must name fewer.
var ErrGrantTooLarge = errors.New("project workload grant exceeds its provider's size limit")

// ConfigurationPrefix holds every project's configuration, connections and
// application secrets in its scope bucket.
const ConfigurationPrefix = "project-configuration/"

// scopeKeyPrefix is the key prefix of a scope's objects below
// ConfigurationPrefix: projects/<project>/live/, or the Preview prefix that
// PreviewKeysOf derives.
func scopeKeyPrefix(project string, scope Scope) string {
	if scope.Kind == ScopePreview {
		return strings.TrimPrefix(PreviewKeysOf(project).ConfigurationPrefix, ConfigurationPrefix)
	}
	return "projects/" + project + "/" + ScopeLive.String() + "/"
}

// configurationKey is the bucket key of the scope object at relative, such as
// configuration/head or trigger-inboxes/*.
func configurationKey(project string, scope Scope, relative string) string {
	return ConfigurationPrefix + scopeKeyPrefix(project, scope) + relative
}

// PreviewKeys are the names of a project's Preview: its configuration in the
// project's Live bucket, its storage grants and its prepared sources.
// PreviewKeysOf is their only derivation, so no other code joins a project ID
// into these names.
type PreviewKeys struct {
	// Scope selects the Preview's configuration.
	Scope Scope
	// ConfigurationPrefix holds the Preview's configuration, connections,
	// application secrets and OAuth attempts in the Live bucket.
	ConfigurationPrefix string
	// SourcePrefix holds the prepared sources in platform storage, below the
	// environment prefix.
	SourcePrefix string
	// GrantWorkloadID names the Preview's grant generations:
	// app-<GrantWorkloadID>-<generation>.
	GrantWorkloadID string
	// GrantIdentity identifies the Preview's grants: it names their hosted
	// IAM roles and marks their local storage accounts, so the Project's
	// release finds them.
	GrantIdentity string
}

// PreviewKeysOf derives the names of the Preview of a project.
func PreviewKeysOf(projectID string) PreviewKeys {
	return PreviewKeys{
		Scope:               Scope{Kind: ScopePreview},
		ConfigurationPrefix: ConfigurationPrefix + "projects/" + projectID + "/" + ScopePreview.String() + "/",
		SourcePrefix:        "projects/" + projectID + "/preview-source/",
		GrantWorkloadID:     flowid.PreviewWorkloadID,
		GrantIdentity:       projectID + "-" + ScopePreview.String(),
	}
}

// ScopeKeyPrefixForTest is scopeKeyPrefix for the tests.
func ScopeKeyPrefixForTest(project string, scope Scope) string {
	return scopeKeyPrefix(project, scope)
}

// ConfigurationKeyForTest is configurationKey for the tests.
func ConfigurationKeyForTest(project string, scope Scope, relative string) string {
	return configurationKey(project, scope, relative)
}
