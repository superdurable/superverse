// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package storage

import (
	"context"
	"fmt"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/hosted/identity"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superdurable/superversev2/internal/comm/platform"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// Bucket names the Live scope's bucket. The name is derived from the namespace
// UID, so a replaced namespace never reaches the old bucket. The empty part is
// the Session of the Preview scopes that no longer exist; it keeps the names of
// existing buckets.
type Account struct{ *identity.Account }

func (h *Account) Bucket(k *kube.Infrastructure, project, namespaceUID string) string {
	return h.BucketPrefix + "-" + identity.OperationID(k.Environment.String(), project, infra.ScopeLive.String(), "", namespaceUID)[:16]
}

// Objects returns the scope's bucket once it carries its ownership tags. It
// pins the bucket owner account and the KMS key on every request.
func (h *Account) Objects(ctx context.Context, k *kube.Infrastructure, project, namespaceUID string) (*infra.ApplicationObjects, error) {
	if err := h.verifyBucket(ctx, k, project, namespaceUID); err != nil {
		return nil, err
	}
	return &infra.ApplicationObjects{Client: h.S3, Bucket: h.Bucket(k, project, namespaceUID), KMSKeyARN: h.KMSKeyARN, AccountID: h.AccountID}, nil
}

// verifyBucket checks that the scope's bucket carries its ownership tags.
func (h *Account) verifyBucket(ctx context.Context, k *kube.Infrastructure, project, uid string) error {
	ctx, cancel := identity.CloudDeadline(ctx)
	defer cancel()
	result, err := h.S3.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: aws.String(h.Bucket(k, project, uid)), ExpectedBucketOwner: aws.String(h.AccountID)})
	if err != nil {
		return err
	}
	return identity.VerifyTags(identity.LiveTags(k, project, uid), identity.S3TagMap(result.TagSet))
}

// tagBucket tags the scope's bucket after a create response was lost
// before tagging. Only this account's bucket whose name binds the admitted,
// immutable namespace UID is recoverable; a caller-provided or independently
// named bucket is never adopted.
func (h *Account) tagBucket(ctx context.Context, k *kube.Infrastructure, project, uid string) error {
	_, err := h.S3.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{Bucket: aws.String(h.Bucket(k, project, uid)), ExpectedBucketOwner: aws.String(h.AccountID), Tagging: &types.Tagging{TagSet: identity.S3Tags(identity.LiveTags(k, project, uid))}})
	return err
}

// EnsureStorage creates the project's private, versioned bucket, encrypted
// with the project KMS key through an S3 Bucket Key, whose policy denies plain HTTP, unencrypted writes and other keys.
// No Service runs the storage, so it returns no reference.
func (h *Account) EnsureStorage(ctx context.Context, k *kube.Infrastructure, project, namespaceUID, _ string) (infra.ResourceRef, error) {
	return infra.ResourceRef{}, h.ensureBucket(ctx, k, project, namespaceUID)
}

func (h *Account) ensureBucket(ctx context.Context, k *kube.Infrastructure, project, uid string) error {
	ctx, cancel := identity.CloudDeadline(ctx)
	defer cancel()
	bucket := h.Bucket(k, project, uid)
	owner := aws.String(h.AccountID)
	_, err := h.S3.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &bucket, ExpectedBucketOwner: owner})
	if platform.HasAWSErrorCode(err, "NotFound", "NoSuchBucket") {
		in := &s3.CreateBucketInput{Bucket: &bucket, ObjectOwnership: types.ObjectOwnershipBucketOwnerEnforced}
		if h.Region != "us-east-1" {
			in.CreateBucketConfiguration = &types.CreateBucketConfiguration{LocationConstraint: types.BucketLocationConstraint(h.Region)}
		}
		_, err = h.S3.CreateBucket(ctx, in)
		if err != nil && !platform.HasAWSErrorCode(err, "BucketAlreadyOwnedByYou") {
			return err
		}
	} else if err != nil {
		return err
	}
	err = h.verifyBucket(ctx, k, project, uid)
	if platform.HasAWSErrorCode(err, "NoSuchTagSet") {
		err = h.tagBucket(ctx, k, project, uid)
	}
	if err != nil {
		return err
	}
	if _, err = h.S3.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{Bucket: &bucket, ExpectedBucketOwner: owner, PublicAccessBlockConfiguration: &types.PublicAccessBlockConfiguration{BlockPublicAcls: aws.Bool(true), IgnorePublicAcls: aws.Bool(true), BlockPublicPolicy: aws.Bool(true), RestrictPublicBuckets: aws.Bool(true)}}); err != nil {
		return err
	}
	if _, err = h.S3.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: &bucket, ExpectedBucketOwner: owner, VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}}); err != nil {
		return err
	}
	if _, err = h.S3.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{Bucket: &bucket, ExpectedBucketOwner: owner, ServerSideEncryptionConfiguration: &types.ServerSideEncryptionConfiguration{Rules: []types.ServerSideEncryptionRule{{ApplyServerSideEncryptionByDefault: &types.ServerSideEncryptionByDefault{SSEAlgorithm: types.ServerSideEncryptionAwsKms, KMSMasterKeyID: &h.KMSKeyARN}, BucketKeyEnabled: aws.Bool(true)}}}}); err != nil {
		return err
	}
	// Project configuration writers use explicit KMS; Dex blobs use the fixed
	// bucket default. Deny caller-selected plaintext, other keys and HTTP. The
	// S3 Bucket Key saves a KMS call per object; it makes the bucket ARN the KMS
	// context of new objects, which kmsThroughS3 allows beside object ARNs.
	policy := h.BucketPolicy(bucket)
	if _, err = h.S3.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: &bucket, ExpectedBucketOwner: owner, Policy: &policy}); err != nil {
		return err
	}
	_, err = h.S3.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{Bucket: &bucket, ExpectedBucketOwner: owner, LifecycleConfiguration: infra.OAuthLifecycle(project)})
	return err
}

// destroyBucket purges every object version, delete marker and
// multipart upload of the scope's bucket in bounded batches, then deletes the
// bucket. It returns infra.ErrPending until the bucket is gone.
func (h *Account) DestroyBucket(ctx context.Context, k *kube.Infrastructure, project, uid string) error {
	ctx, cancel := identity.CloudDeadline(ctx)
	defer cancel()
	bucket := h.Bucket(k, project, uid)
	owner := aws.String(h.AccountID)
	err := h.verifyBucket(ctx, k, project, uid)
	if platform.HasAWSErrorCode(err, "NoSuchBucket") {
		return nil
	}
	if platform.HasAWSErrorCode(err, "NoSuchTagSet") {
		err = h.tagBucket(ctx, k, project, uid)
	}
	if err != nil {
		return err
	}
	page, err := h.S3.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: &bucket, ExpectedBucketOwner: owner, MaxKeys: aws.Int32(500)})
	if err != nil {
		return err
	}
	objects := []types.ObjectIdentifier{}
	for _, v := range page.Versions {
		objects = append(objects, types.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
	}
	for _, v := range page.DeleteMarkers {
		objects = append(objects, types.ObjectIdentifier{Key: v.Key, VersionId: v.VersionId})
	}
	if len(objects) > 0 {
		deleted, deleteErr := h.S3.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: &bucket, ExpectedBucketOwner: owner, Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)}})
		if deleteErr != nil {
			return deleteErr
		}
		if len(deleted.Errors) > 0 {
			first := deleted.Errors[0]
			return fmt.Errorf("%d project object versions remain during cleanup; first %s: %s %s", len(deleted.Errors), aws.ToString(first.Key), aws.ToString(first.Code), aws.ToString(first.Message))
		}
		return fmt.Errorf("%w: deleted %d object versions of bucket %s", infra.ErrPending, len(objects), bucket)
	}
	// Incomplete uploads cannot be silently left behind or hidden by a lifecycle.
	uploads, err := h.S3.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: &bucket, ExpectedBucketOwner: owner, MaxUploads: aws.Int32(100)})
	if err != nil {
		return err
	}
	for _, u := range uploads.Uploads {
		// An upload completed or aborted since the listing is gone already.
		_, err = h.S3.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: &bucket, Key: u.Key, UploadId: u.UploadId, ExpectedBucketOwner: owner})
		if err != nil && !platform.HasAWSErrorCode(err, "NoSuchUpload") {
			return fmt.Errorf("abort multipart upload of %s: %w", aws.ToString(u.Key), err)
		}
	}
	if len(uploads.Uploads) > 0 {
		return fmt.Errorf("%w: aborted %d multipart uploads of bucket %s", infra.ErrPending, len(uploads.Uploads), bucket)
	}
	_, err = h.S3.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: &bucket, ExpectedBucketOwner: owner})
	if err != nil && !platform.HasAWSErrorCode(err, "NoSuchBucket") {
		return err
	}
	// The next call proves the bucket absent before cleanup completes.
	return fmt.Errorf("%w: bucket %s is being deleted", infra.ErrPending, bucket)
}
