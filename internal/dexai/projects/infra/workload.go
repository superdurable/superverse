// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package infra

import (
	"strings"

	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
)

// Workload is one generation of a Live deployment or of the project's Preview
// that is granted access to the project's storage. WorkloadID is the
// deployment ID, or the Preview's grant workload ID from PreviewKeysOf.
type Workload struct {
	ProjectID, WorkloadID string
	// Scope is the configuration scope of the workload.
	Scope                             Scope
	Generation, ConfigurationRevision uint64
	Storage                           StorageRefs
	Configuration                     objectstore.ObjectRef
	GrantID                           string
	Revoked                           bool
	// Principal is the Sandbox identity a Preview grant binds. A Live
	// generation runs as its own service account and has none.
	Principal *Principal
}

// Principal is the identity of a Preview grant: the project's Sandbox
// boundary and the identity its workloads run as, by UID. They are persisted as
// Namespace and ServiceAccount.
type Principal struct {
	Boundary ResourceRef `json:"Namespace"`
	Identity ResourceRef `json:"ServiceAccount"`
}

// Name names the generation's resources: app-<workload>-<generation>.
func (s Workload) Name() string { return WorkloadName(s.WorkloadID, s.Generation) }

// Preview reports whether the workload is the project's Preview.
func (s Workload) Preview() bool { return s.Scope.Kind == ScopePreview }

// ScopeRef is the configuration scope of the workload with the storage it
// reads.
func (s Workload) ScopeRef() ScopeRef {
	return ScopeRef{ProjectID: s.ProjectID, Scope: s.Scope, Storage: s.Storage}
}

// Environment is the part of a granted workload's process environment that
// names its project, scope and accepted configuration: the application
// contract every runtime and backend shares. How the workload reaches the
// storage is the backend's part.
func (s Workload) Environment() map[string]string {
	return map[string]string{
		"DEX_PROJECT_ID": s.ProjectID, "DEX_PROJECT_SCOPE": s.Scope.Kind.String(),
		"DEX_PROJECT_STORAGE_BUCKET": s.Storage.StorageBucket, "DEX_PROJECT_STORAGE_PREFIX": s.Storage.BlobPrefix,
		"DEX_PROJECT_CONFIG_KEY":     strings.TrimPrefix(s.Configuration.Key, s.Storage.BlobPrefix),
		"DEX_PROJECT_CONFIG_VERSION": s.Configuration.Version, "DEX_PROJECT_CONFIG_DIGEST": s.Configuration.Digest,
	}
}

// AccountDescription marks the MinIO service account of a Preview grant with
// its Preview and generation, so the Project's release finds the account
// without another index; a Live account is unmarked.
func (s Workload) AccountDescription() string {
	if !s.Preview() {
		return ""
	}
	return PreviewKeysOf(s.ProjectID).GrantIdentity + "/" + s.Name()
}

// MarkedGrant is the grant generation, app-<workload>-<generation>, whose
// MinIO service account description marks it as one of the Preview of keys.
func (keys PreviewKeys) MarkedGrant(description string) (string, bool) {
	return strings.CutPrefix(description, keys.GrantIdentity+"/")
}

// Current reports whether the workload's grant is the current one of its
// generation, which only a current grant's environment may be issued for.
func (s Workload) Current() bool {
	return !s.Revoked && s.Generation != 0 && s.GrantID == s.Name()
}

// GrantResources are the S3 resources of a workload's grant: the Live bucket;
// the configuration head, whose current version the Connector SDK also reads
// to renew a credential broker's scope token; the exact configuration and
// secret objects it reads; and the objects of its scope that it reads and
// writes, the inboxes of its Trigger bindings, which the Connector SDK writes
// under the application's grant. Connection credentials are not among them:
// applications read those through the credential broker.
func (s Workload) GrantResources(secretKeys []string) (bucket, head string, reads, readWrites []string) {
	bucket = "arn:aws:s3:::" + s.Storage.StorageBucket
	head = bucket + "/" + s.Configuration.Key
	reads = []string{head}
	for _, key := range secretKeys {
		reads = append(reads, bucket+"/"+key)
	}
	readWrites = []string{bucket + "/" + configurationKey(s.ProjectID, s.Scope, "trigger-inboxes/*")}
	return bucket, head, reads, readWrites
}
