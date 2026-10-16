// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package identity

import (
	"strings"

	"github.com/google/uuid"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// cloudName names the cloud resources of one identity of a project:
// <workload prefix>-<environment>-<project, first 8>-<hash of the identity>. The
// content-derived suffix keeps names within AWS and Temporal length limits; the
// tags carry the complete owning identity.
func cloudName(k *kube.Infrastructure, project, identity string) string {
	return CloudNamePrefix(k, project) + uuid.NewSHA1(uuid.NameSpaceURL, []byte("superverse-v2/"+k.Environment.String()+"/"+identity)).String()[:12]
}

// CloudNamePrefix is the part of a project's cloud names before the hash of
// their identity.
func CloudNamePrefix(k *kube.Infrastructure, project string) string {
	short := project
	if len(short) > 8 {
		short = short[:8]
	}
	return k.Names().WorkloadPrefix + "-" + k.Environment.String() + "-" + short + "-"
}

// LiveName names the cloud resources of a project's Live scope.
func LiveName(k *kube.Infrastructure, project string) string {
	return cloudName(k, project, project+"-live")
}

// grantName names a workload's grant roles: the Live scope's name, or the
// Preview's own, which Live cleanup never lists. A Preview has one role per
// Sandbox principal, named by previewRoleName.
func grantName(k *kube.Infrastructure, s infra.Workload) string {
	if s.Preview() {
		return cloudName(k, s.ProjectID, infra.PreviewKeysOf(s.ProjectID).GrantIdentity)
	}
	return LiveName(k, s.ProjectID)
}

// OperationID is a stable UUID of its parts, used as a cloud idempotency key
// and name suffix.
func OperationID(parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.Join(parts, "/"))).String()
}
