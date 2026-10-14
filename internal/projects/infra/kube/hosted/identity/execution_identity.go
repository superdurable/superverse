// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package identity

import (
	"context"

	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// EnsureDexIdentity converges the IAM role the project Dex assumes through Pod
// Identity, which reaches only the project's bucket and, through S3, its KMS
// key, and binds the role to the dex service account. No credential of the
// platform enters the project.
func (h *Account) EnsureDexIdentity(ctx context.Context, k *kube.Infrastructure, project string, storage infra.StorageRefs) error {
	policy := h.dexPolicy("arn:aws:s3:::" + storage.StorageBucket)
	tags := LiveTags(k, project, storage.Boundary.UID)
	account := kube.ComponentDex.String()
	arn, err := h.ensureRole(ctx, k, tags, roleName(LiveName(k, project), account), storage.Boundary.Name, account, policy)
	if err != nil {
		return err
	}
	return h.ensureAssociation(ctx, tags, tags, storage.Boundary.Name, account, arn, "")
}

// dexPolicy is the scope policy of the project Dex role: it reaches the bucket
// with the ARN bucket and, through S3, the project KMS key.
func (h *Account) dexPolicy(bucket string) string {
	objects := bucket + "/*"
	return policyJSON(
		policyStatement{Effect: effectAllow, Action: []policyAction{actionS3GetBucketVersioning, actionS3ListBucket, actionS3ListBucketVersions}, Resource: bucket},
		policyStatement{Effect: effectAllow, Action: []policyAction{actionS3GetObject, actionS3GetObjectVersion, actionS3PutObject, actionS3DeleteObject, actionS3DeleteObjectVersion}, Resource: objects},
		h.kmsThroughS3(bucket, objects),
	)
}
