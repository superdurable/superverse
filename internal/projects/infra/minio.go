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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/minio/madmin-go/v3"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform"
)

// LocalStorageRegion is the region that the MinIO of a local scope serves.
const LocalStorageRegion = "us-east-1"

// LocalApplicationBucket is the bucket of every local scope's own MinIO.
const LocalApplicationBucket = "application"

// ProjectS3 is an S3 client of a MinIO endpoint; a nil client uses the SDK's.
func ProjectS3(endpoint, access, secret string, client *http.Client) *s3.Client {
	options := s3.Options{BaseEndpoint: aws.String(endpoint), Region: LocalStorageRegion, UsePathStyle: true, Credentials: awscredentials.NewStaticCredentialsProvider(access, secret, "")}
	if client != nil {
		options.HTTPClient = client
	}
	return s3.New(options)
}

// EnsureBucket creates the project's versioned application bucket. A
// MinIO that is still starting sends no HTTP response (status 0 in an SDK
// response error), which is pending; any other answer than a missing bucket,
// such as a denial, is an error.
func EnsureBucket(ctx context.Context, objects *ApplicationObjects, lifecycle *types.BucketLifecycleConfiguration) error {
	bucket := aws.String(objects.Bucket)
	_, err := objects.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: bucket})
	var response interface{ HTTPStatusCode() int }
	switch {
	case err == nil:
	case platform.HasAWSErrorCode(err, "NoSuchKey", "NotFound", "NoSuchBucket"):
		_, err = objects.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: bucket})
		if err != nil && !platform.HasAWSErrorCode(err, "BucketAlreadyOwnedByYou", "BucketAlreadyExists") {
			return fmt.Errorf("create the application bucket: %w", err)
		}
	case !errors.As(err, &response) || response.HTTPStatusCode() == 0:
		return fmt.Errorf("%w: the project's MinIO does not answer yet: %w", ErrPending, err)
	default:
		return fmt.Errorf("read the application bucket: %w", err)
	}
	if _, err = objects.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: bucket, VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}}); err != nil {
		return fmt.Errorf("enable application bucket versioning: %w", err)
	}
	if _, err = objects.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{Bucket: bucket, LifecycleConfiguration: lifecycle}); err != nil {
		return fmt.Errorf("configure the application bucket lifecycle: %w", err)
	}
	return nil
}

// storageAccount returns the workload's MinIO service account, creating it
// under parent with description and policyIfAbsent when it does not exist. A
// lost create response is reconciled by the read that follows the create.
func storageAccount(ctx context.Context, admin *madmin.AdminClient, parent, access, secret, description string, policyIfAbsent []byte) (madmin.InfoServiceAccountResp, error) {
	info, err := admin.InfoServiceAccount(ctx, access)
	if err == nil || !MissingStorageAccount(err) {
		return info, err
	}
	_, createErr := admin.AddServiceAccount(ctx, madmin.AddServiceAccountReq{TargetUser: parent, AccessKey: access, SecretKey: secret, Description: description, Policy: policyIfAbsent})
	info, err = admin.InfoServiceAccount(ctx, access)
	if err != nil {
		return info, errors.Join(createErr, err)
	}
	return info, nil
}

// EnsureStorageAccount creates the workload's MinIO service account under the
// root user and accepts an existing one only with the same parent and policy.
func EnsureStorageAccount(ctx context.Context, admin *madmin.AdminClient, parent, access, secret, description string, policy []byte) error {
	info, err := storageAccount(ctx, admin, parent, access, secret, description, policy)
	if err != nil {
		return err
	}
	if info.ParentUser != parent || info.AccountStatus != accountEnabled || !EqualJSON([]byte(info.Policy), policy) {
		return fmt.Errorf("storage grant is incompatible (status=%s, parentMatched=%t, policyMatched=%t)", info.AccountStatus, info.ParentUser == parent, EqualJSON([]byte(info.Policy), policy))
	}
	return nil
}

// RevokeStorageAccount disables the workload's service account. An account that
// was never created is created deny-only and disabled, so a late issuance
// cannot enable it.
func RevokeStorageAccount(ctx context.Context, admin *madmin.AdminClient, parent, access, secret, description string) error {
	deny := []byte(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":["s3:*"],"Resource":["arn:aws:s3:::*"]}]}`)
	info, err := storageAccount(ctx, admin, parent, access, secret, description, deny)
	if err != nil {
		return err
	}
	if info.ParentUser != parent {
		return errors.New("storage revocation parent mismatch")
	}
	if info.AccountStatus != accountDisabled {
		if err = admin.UpdateServiceAccount(ctx, access, madmin.UpdateServiceAccountReq{NewStatus: accountDisabled}); err != nil {
			return err
		}
	}
	info, err = admin.InfoServiceAccount(ctx, access)
	if err != nil {
		return err
	}
	if info.AccountStatus != accountDisabled {
		return errors.New("storage revocation is unconfirmed")
	}
	return nil
}

// The statuses of a MinIO service account.
const (
	accountEnabled  = "on"
	accountDisabled = "off"
)

func MissingStorageAccount(err error) bool {
	code := madmin.ToErrorResponse(err).Code
	return code == "XMinioInvalidIAMCredentials" || code == "XMinioNoSuchServiceAccount"
}

// EqualJSON compares two policies as JSON values whose lists are sets.
func EqualJSON(a, b []byte) bool {
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	sortPolicySets(av)
	sortPolicySets(bv)
	return string(comm.Must(json.Marshal(av))) == string(comm.Must(json.Marshal(bv)))
}

func sortPolicySets(value any) {
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			sortPolicySets(child)
		}
	case []any:
		for _, child := range v {
			sortPolicySets(child)
		}
		sort.Slice(v, func(i, j int) bool {
			return string(comm.Must(json.Marshal(v[i]))) < string(comm.Must(json.Marshal(v[j])))
		})
	}
}

// The environment variables of an S3 key pair, which also key the Secrets
// that hold one.
const (
	AccessKeyIDVariable     = "AWS_ACCESS_KEY_ID"
	SecretAccessKeyVariable = "AWS_SECRET_ACCESS_KEY" //nolint:gosec // G101: the name of an environment variable, not a credential
)

func RandomSecret() string {
	b := make([]byte, 32)
	comm.Must(rand.Read(b))
	return hex.EncodeToString(b)
}

// GrantPolicy is the MinIO policy of a workload's grant.
func GrantPolicy(s Workload, secretKeys []string) []byte {
	bucket, head, reads, readWrites := s.GrantResources(secretKeys)
	return comm.Must(json.Marshal(storagePolicy{Version: "2012-10-17", Statement: []storageStatement{
		allow([]string{bucket}, "s3:GetBucketVersioning"),
		// MinIO grants exact-key version reads; the SDK enforces the accepted version and digest.
		allow(reads, "s3:GetObjectVersion"),
		allow([]string{head}, "s3:GetObject"),
		allow(readWrites, "s3:GetObject", "s3:GetObjectVersion", "s3:PutObject"),
	}}))
}

// storagePolicy is a MinIO policy document. Its fields, like those of a
// statement, are in the order of their JSON keys, which the grant policies
// have always been written in.
type storagePolicy struct {
	Statement []storageStatement `json:"Statement"`
	Version   string             `json:"Version"`
}

type storageStatement struct {
	Action   []string `json:"Action"`
	Effect   string   `json:"Effect"`
	Resource []string `json:"Resource"`
}

// allow is the statement that allows actions on resources.
func allow(resources []string, actions ...string) storageStatement {
	return storageStatement{Action: actions, Effect: "Allow", Resource: resources}
}
