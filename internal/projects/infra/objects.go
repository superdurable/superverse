// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package infra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/superdurable/superversev2/internal/platform"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
)

// ApplicationObjects is a scope's verified bucket, holding the application's
// runtime objects: configuration, connections and Flow definitions. Hosted, it
// pins the bucket owner account and the KMS key on every request.
type ApplicationObjects struct {
	*s3.Client
	Bucket, KMSKeyARN, AccountID string
}

// OAuthLifecycle expires the Live scope's temporary OAuth objects after one
// day; no other project data has a lifecycle. The Preview's OAuth objects go
// with its prefix when the Project is deleted.
func OAuthLifecycle(project string) *types.BucketLifecycleConfiguration {
	return &types.BucketLifecycleConfiguration{Rules: []types.LifecycleRule{{ID: aws.String("expired-oauth-temporary-objects"), Status: types.ExpirationStatusEnabled, Filter: &types.LifecycleRuleFilter{Prefix: aws.String(configurationKey(project, Scope{Kind: ScopeLive}, "oauth/"))}, Expiration: &types.LifecycleExpiration{Days: aws.Int32(1)}, NoncurrentVersionExpiration: &types.NoncurrentVersionExpiration{NoncurrentDays: aws.Int32(1)}}}}
}

// Get reads one version of an object, or its current version for an empty
// version, and returns the version it read.
func (o *ApplicationObjects) Get(ctx context.Context, key, version string) ([]byte, string, error) {
	data, version, _, err := o.getWithETag(ctx, key, version)
	return data, version, err
}

func (o *ApplicationObjects) getWithETag(ctx context.Context, key, version string) (data []byte, observedVersion, etag string, err error) {
	input := &s3.GetObjectInput{Bucket: aws.String(o.Bucket), Key: aws.String(key)}
	if o.AccountID != "" {
		input.ExpectedBucketOwner = aws.String(o.AccountID)
	}
	if version != "" {
		input.VersionId = &version
	}
	out, err := o.GetObject(ctx, input)
	if err != nil {
		return nil, "", "", err
	}
	defer func() { _ = out.Body.Close() }()
	if o.KMSKeyARN != "" && (out.ServerSideEncryption != types.ServerSideEncryptionAwsKms || aws.ToString(out.SSEKMSKeyId) != o.KMSKeyARN) {
		return nil, "", "", errors.New("project object encryption identity mismatch")
	}
	data, err = io.ReadAll(io.LimitReader(out.Body, objectstore.MaxObjectBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("read application object %s: %w", key, err)
	}
	if len(data) > objectstore.MaxObjectBytes {
		return nil, "", "", fmt.Errorf("application object %s exceeds %d bytes", key, objectstore.MaxObjectBytes)
	}
	return data, aws.ToString(out.VersionId), aws.ToString(out.ETag), nil
}

// Put writes a JSON object; an immutable one may only be created.
func (o *ApplicationObjects) Put(ctx context.Context, key string, data []byte, immutable bool) (objectstore.ObjectRef, error) {
	return o.PutGuarded(ctx, key, data, immutable, nil)
}

// PutGuarded writes a JSON object after guard, when given, still admits the
// write. Equal content is accepted as already written. A mutable object is
// replaced only under the ETag just read; after an uncertain write the object
// is read again and accepted only with the exact content.
func (o *ApplicationObjects) PutGuarded(ctx context.Context, key string, data []byte, immutable bool, guard func() error) (objectstore.ObjectRef, error) {
	existing, version, etag, readErr := o.getWithETag(ctx, key, "")
	if guard != nil {
		if err := guard(); err != nil {
			return objectstore.ObjectRef{}, err
		}
	}
	missing := platform.HasAWSErrorCode(readErr, "NoSuchKey", "NotFound", "NoSuchBucket")
	switch {
	case readErr == nil && bytes.Equal(existing, data) && objectstore.IsVersion(version):
		return objectstore.ObjectRef{Key: key, Version: version, Digest: objectstore.Digest(data), MediaType: objectstore.MediaTypeJSON}, nil
	case readErr == nil && immutable:
		return objectstore.ObjectRef{}, errors.New("application immutable object conflict")
	case readErr != nil && !missing:
		return objectstore.ObjectRef{}, readErr
	}
	input := &s3.PutObjectInput{Bucket: aws.String(o.Bucket), Key: aws.String(key), ContentType: aws.String(objectstore.MediaTypeJSON), Body: bytes.NewReader(data)}
	if o.AccountID != "" {
		input.ExpectedBucketOwner = aws.String(o.AccountID)
	}
	if o.KMSKeyARN != "" {
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		input.SSEKMSKeyId = aws.String(o.KMSKeyARN)
	}
	if immutable || missing {
		input.IfNoneMatch = aws.String("*")
	} else {
		if etag == "" {
			return objectstore.ObjectRef{}, errors.New("mutable application object lacks an ETag")
		}
		input.IfMatch = &etag
	}
	out, err := o.PutObject(ctx, input)
	if err != nil {
		written, observed, readErr := o.Get(ctx, key, "")
		if readErr != nil || !bytes.Equal(written, data) {
			return objectstore.ObjectRef{}, fmt.Errorf("application object %s publication remains unconfirmed: %w", key, errors.Join(err, readErr))
		}
		version = observed
	} else {
		version = aws.ToString(out.VersionId)
	}
	if !objectstore.IsVersion(version) {
		return objectstore.ObjectRef{}, errors.New("application object storage versioning is required")
	}
	return objectstore.ObjectRef{Key: key, Version: version, Digest: objectstore.Digest(data), MediaType: objectstore.MediaTypeJSON}, nil
}
