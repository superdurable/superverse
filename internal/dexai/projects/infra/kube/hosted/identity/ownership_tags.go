// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package identity

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// The tag keys of the cloud resources of a project. A resource carries the tags
// of its owner, so ownership is proven from the resource itself.
const (
	tagManagedBy                  = "managed-by"
	tagEnvironment                = "environment"
	tagProjectID                  = "project-id"
	tagScope                      = "scope"
	tagNamespaceUID               = "namespace-uid"
	tagPrincipalNamespaceUID      = "principal-namespace-uid"
	tagPrincipalServiceAccountUID = "principal-service-account-uid"
	// tagRevokedAt dates a revoked Live role, which stays as a fence.
	tagRevokedAt = "revoked-at"
	// tagGrantGeneration and tagRevokedGeneration are the highest grant
	// generations a Preview's principal role was issued and revoked for.
	tagGrantGeneration   = "grant-generation"
	tagRevokedGeneration = "revoked-generation"
	// tagScopeWritten records <generation>@<RFC 3339 time> of the last write
	// of a Preview principal role's scope policy for an issuance.
	tagScopeWritten = "scope-written"
)

// scopeTags bind a cloud resource to a project's scope and to the namespace UID
// it was created for, so a replaced namespace never adopts it.
func scopeTags(k *kube.Infrastructure, project string, scope infra.ScopeKind, namespaceUID string) map[string]string {
	return map[string]string{tagManagedBy: k.Names().Product, tagEnvironment: k.Environment.String(), tagProjectID: project, tagScope: scope.String(), tagNamespaceUID: namespaceUID}
}

// LiveTags bind a cloud resource to a project's Live scope.
func LiveTags(k *kube.Infrastructure, project, namespaceUID string) map[string]string {
	return scopeTags(k, project, infra.ScopeLive, namespaceUID)
}

// previewTags bind a Preview's grant identities to the project's Preview
// scope and to the namespace UID of the Live storage they reach.
func previewTags(k *kube.Infrastructure, project, namespaceUID string) map[string]string {
	return scopeTags(k, project, infra.ScopePreview, namespaceUID)
}

// grantTags are the tags of a workload generation's role and Pod Identity
// association. A Preview's also bind the principal it was issued to.
func grantTags(k *kube.Infrastructure, s infra.Workload) map[string]string {
	if !s.Preview() {
		return LiveTags(k, s.ProjectID, s.Storage.Boundary.UID)
	}
	tags := previewTags(k, s.ProjectID, s.Storage.Boundary.UID)
	tags[tagPrincipalNamespaceUID], tags[tagPrincipalServiceAccountUID] = s.Principal.Boundary.UID, s.Principal.Identity.UID
	return tags
}

// ownerTags are the tags that prove a workload's grant identities its own:
// the grant tags without the principal, so revocation and cleanup work after
// the principal is gone.
func ownerTags(k *kube.Infrastructure, s infra.Workload) map[string]string {
	if !s.Preview() {
		return LiveTags(k, s.ProjectID, s.Storage.Boundary.UID)
	}
	return previewTags(k, s.ProjectID, s.Storage.Boundary.UID)
}

// VerifyTags requires every wanted tag.
func VerifyTags(want, tags map[string]string) error {
	for name, value := range want {
		if tags[name] != value {
			return fmt.Errorf("%w: cloud tag %s", infra.ErrOwnershipConflict, name)
		}
	}
	return nil
}

func S3Tags(tags map[string]string) []s3types.Tag {
	result := make([]s3types.Tag, 0, len(tags))
	for key, value := range tags {
		result = append(result, s3types.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return result
}

func S3TagMap(tags []s3types.Tag) map[string]string {
	result := make(map[string]string, len(tags))
	for _, tag := range tags {
		result[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return result
}

func iamTags(tags map[string]string) []iamtypes.Tag {
	result := make([]iamtypes.Tag, 0, len(tags))
	for key, value := range tags {
		result = append(result, iamtypes.Tag{Key: aws.String(key), Value: aws.String(value)})
	}
	return result
}

func iamTagMap(tags []iamtypes.Tag) map[string]string {
	result := make(map[string]string, len(tags))
	for _, tag := range tags {
		result[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return result
}
