// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package config

import (
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
)

// configurationSnapshot is the API view of the target's configuration with
// metadata, before validation decides whether it is READY.
func configurationSnapshot(target configurationTarget, metadata Metadata) *pb.ProjectConfigurationSnapshot {
	snapshot := &pb.ProjectConfigurationSnapshot{
		ProjectId: target.scope.ProjectID, Scope: target.projectScope, Status: pb.ConfigurationStatus_CONFIGURATION_STATUS_NEEDS_REVIEW,
		Revision: metadata.ConfigurationRevision, ReleaseId: target.releaseID, SourceCommit: metadata.SourceCommit, AppManifest: target.manifest,
		AppManifestRevision: metadata.AppManifestRevision, ManifestDigest: metadata.ManifestDigest,
		ConnectorContractDigest: metadata.ConnectorContractDigest, EnvironmentContractDigest: metadata.EnvironmentContractDigest,
	}
	if metadata.ConfigurationRevision == 0 {
		snapshot.Status = pb.ConfigurationStatus_CONFIGURATION_STATUS_DRAFT
	}
	return snapshot
}
