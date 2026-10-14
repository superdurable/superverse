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
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	madmin "github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/superdurable/superversev2/internal/platform"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
)

// loopbackHost is the only host a test reaches its Local Kind fixtures on.
const loopbackHost = "127.0.0.1"

// loopbackMinIO is the endpoint of the isolated Local Kind MinIO, which a test
// reaches only on the loopback address.
func loopbackMinIO(t *testing.T) (endpoint string, u *url.URL) {
	t.Helper()
	endpoint = os.Getenv("SUPERVERSE_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Fatal("requires isolated Local Kind MinIO")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() != loopbackHost {
		t.Fatal("requires loopback MinIO fixture")
	}
	return endpoint, u
}

// removeBucket deletes every version of the bucket's objects and the bucket
// itself with the root client.
func removeBucket(t *testing.T, root *s3.Client, bucket string) {
	t.Helper()
	versions, err := root.ListObjectVersions(context.Background(), &s3.ListObjectVersionsInput{Bucket: aws.String(bucket)})
	if err != nil {
		t.Error(err)
		return
	}
	for _, v := range versions.Versions {
		if _, err = root.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: v.Key, VersionId: v.VersionId}); err != nil {
			t.Error(err)
		}
	}
	if _, err = root.DeleteBucket(context.Background(), &s3.DeleteBucketInput{Bucket: aws.String(bucket)}); err != nil {
		t.Error(err)
	}
}

func TestMinIOGrantRevocationFencesLateIssuance(t *testing.T) {
	endpoint, u := loopbackMinIO(t)
	parent, password := os.Getenv(AccessKeyIDVariable), os.Getenv(SecretAccessKeyVariable)
	admin, err := madmin.NewWithOptions(u.Host, &madmin.Options{Creds: credentials.NewStaticV4(parent, password, "")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	access, secret := RandomSecret()[:20], RandomSecret()[:32]
	t.Cleanup(func() {
		if deleteErr := admin.DeleteServiceAccount(context.Background(), access); deleteErr != nil {
			t.Error("test grant cleanup failed", deleteErr)
		}
	})
	s := Workload{ProjectID: "abcd", WorkloadID: "test", Scope: Scope{Kind: ScopeLive}, Generation: 1, Storage: StorageRefs{StorageBucket: "sv2-grant-" + RandomSecret()[:10], BlobPrefix: "project-configuration/"}, Configuration: objectstore.ObjectRef{Key: "project-configuration/projects/abcd/live/configuration/head"}}
	root := ProjectS3(endpoint, parent, password, nil)
	if _, err = root.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.Storage.StorageBucket)}); err != nil {
		t.Fatal(err)
	}
	if _, err = root.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(s.Storage.StorageBucket), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}}); err != nil {
		t.Fatal(err)
	}
	first, err := root.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(s.Configuration.Key), Body: strings.NewReader("accepted")})
	if err != nil {
		t.Fatal(err)
	}
	s.Configuration.Version = aws.ToString(first.VersionId)
	_, err = root.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(s.Configuration.Key), Body: strings.NewReader("later")})
	if err != nil {
		t.Fatal(err)
	}
	key := s.Storage.BlobPrefix + "projects/abcd/live/trigger-inboxes/example/head"
	connectionKey := s.Storage.BlobPrefix + "projects/abcd/live/connections/example/head"
	t.Cleanup(func() { removeBucket(t, root, s.Storage.StorageBucket) })
	if _, err = root.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(connectionKey), Body: strings.NewReader("credential")}); err != nil {
		t.Fatal(err)
	}
	secretKey := s.Storage.BlobPrefix + "projects/abcd/live/app-secrets/SIGNING_SECRET/" + strings.Repeat("a", 32)
	secretObject, err := root.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(secretKey), Body: strings.NewReader("test-secret")})
	if err != nil {
		t.Fatal(err)
	}
	tokenKey := s.Storage.BlobPrefix + "projects/abcd/live/app-secrets/DEX_CREDENTIAL_BROKER_TOKEN/" + strings.Repeat("c", 32)
	tokenObject, err := root.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(tokenKey), Body: strings.NewReader("scope-token")})
	if err != nil {
		t.Fatal(err)
	}
	// The keys a configuration with an application secret and a credential broker references, relative to the blob prefix.
	document := `{"environment":{"SIGNING_SECRET":{"secretRef":{"key":"` + strings.TrimPrefix(secretKey, s.Storage.BlobPrefix) + `"}},"PLAIN":{"value":"x"}},` +
		`"credentialBroker":{"endpoint":"http://server:8086/internal/connection-credentials/read","tokenSecretRef":{"key":"` + strings.TrimPrefix(tokenKey, s.Storage.BlobPrefix) + `"}}}`
	secretKeys, err := ApplicationSecretKeys([]byte(document), s.ScopeRef())
	if err != nil || len(secretKeys) != 2 {
		t.Fatalf("the referenced secrets are %v: %v", secretKeys, err)
	}
	outside := strings.Replace(document, "/DEX_CREDENTIAL_BROKER_TOKEN/", "/SIGNING_SECRET/", 1)
	if _, err = ApplicationSecretKeys([]byte(outside), s.ScopeRef()); err == nil {
		t.Fatal("a broker token outside its secret name was granted")
	}
	policy := GrantPolicy(s, secretKeys)
	if err = EnsureStorageAccount(ctx, admin, parent, access, secret, "", policy); err != nil {
		info, _ := admin.InfoServiceAccount(ctx, access)
		t.Logf("safe policy comparison: actual=%s expected=%s", info.Policy, policy)
		t.Fatal(err)
	}
	client := ProjectS3(endpoint, access, secret, nil)
	accepted, e := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(s.Configuration.Key), VersionId: first.VersionId})
	if e != nil {
		t.Fatal("accepted configuration version denied", e)
	}
	body, e := io.ReadAll(accepted.Body)
	_ = accepted.Body.Close()
	if e != nil || string(body) != "accepted" {
		t.Fatalf("accepted configuration changed: body=%q error=%v", body, e)
	}
	pinnedSecret, e := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(secretKey), VersionId: secretObject.VersionId})
	if e != nil {
		t.Fatal("accepted secret object denied", e)
	}
	_ = pinnedSecret.Body.Close()
	brokerToken, e := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(tokenKey), VersionId: tokenObject.VersionId})
	if e != nil {
		t.Fatal("the credential broker's scope token denied", e)
	}
	_ = brokerToken.Body.Close()
	// The Connector SDK reads the head's current version to renew the broker token.
	current, e := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(s.Configuration.Key)})
	if e != nil {
		t.Fatal("the current configuration head denied", e)
	}
	body, e = io.ReadAll(current.Body)
	_ = current.Body.Close()
	if e != nil || string(body) != "later" {
		t.Fatalf("the current configuration head: body=%q error=%v", body, e)
	}
	if _, e = client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(connectionKey)}); !isForbidden(e) {
		t.Fatalf("a connection object was readable: %v", e)
	}
	for _, denied := range []string{
		strings.Replace(secretKey, strings.Repeat("a", 32), strings.Repeat("b", 32), 1),
		strings.Replace(secretKey, "/live/", "/preview/session/", 1),
		strings.Replace(secretKey, "/abcd/", "/other/", 1),
	} {
		_, e := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(denied), VersionId: secretObject.VersionId})
		if !isForbidden(e) {
			t.Fatalf("out-of-scope secret read was not forbidden: %s error=%v", denied, e)
		}
	}
	for _, denied := range []string{
		secretKey,
		s.Configuration.Key,
		"project-configuration/projects/abcd/live/app-manifest/head",
		"project-configuration/projects/abcd/live/oauth/authorization/head",
		"project-configuration/projects/abcd/preview/session/trigger-inboxes/example/head",
		connectionKey,
	} {
		if _, e := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(denied), Body: strings.NewReader("denied")}); e == nil {
			t.Fatalf("out-of-scope write allowed: %s", denied)
		}
	}

	if _, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(key), Body: strings.NewReader("scoped")}); err != nil {
		t.Fatal("scoped write failed", err)
	}
	if _, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String("project-configuration/projects/other/live/trigger-inboxes/example/head"), Body: strings.NewReader("denied")}); err == nil {
		t.Fatal("cross-project write accepted")
	}
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() { _ = EnsureStorageAccount(ctx, admin, parent, access, secret, "", policy) })
	}
	if err = RevokeStorageAccount(ctx, admin, parent, access, secret, ""); err != nil {
		t.Fatal(err)
	}
	group.Wait()
	if err = EnsureStorageAccount(ctx, admin, parent, access, secret, "", policy); err == nil {
		t.Fatal("revoked generation was reissued")
	}
	if _, err = client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.Storage.StorageBucket), Key: aws.String(key)}); err == nil {
		t.Fatal("revoked identity still reads its Trigger inbox")
	}
	if err = RevokeStorageAccount(ctx, admin, parent, access, secret, ""); err != nil {
		t.Fatal("repeat revocation failed", err)
	}
}

// isForbidden reports an S3 answer of HTTP 403.
func isForbidden(err error) bool {
	var responseError *smithyhttp.ResponseError
	return errors.As(err, &responseError) && responseError.HTTPStatusCode() == 403
}

// A denied bucket read fails at once instead of waiting as pending until the
// provisioning deadline, while a MinIO that sends no response yet is pending.
func TestMinIOLocalBucketDenialIsNotPending(t *testing.T) {
	endpoint, _ := loopbackMinIO(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	lifecycle := OAuthLifecycle("abcd")
	denied := &ApplicationObjects{Client: ProjectS3(endpoint, "sv2-unknown-"+RandomSecret()[:8], RandomSecret()[:32], nil), Bucket: "sv2-denied-" + RandomSecret()[:10]}
	err := EnsureBucket(ctx, denied, lifecycle)
	if err == nil || errors.Is(err, ErrPending) || !platform.IsHTTPAccessDenied(err) {
		t.Fatalf("denied bucket read was not a conclusive access error: %v", err)
	}
	silent := &ApplicationObjects{Client: ProjectS3("http://127.0.0.1:1", "sv2-unknown", RandomSecret()[:32], nil), Bucket: denied.Bucket}
	if err = EnsureBucket(ctx, silent, lifecycle); !errors.Is(err, ErrPending) {
		t.Fatalf("unanswered bucket read was not pending: %v", err)
	}
}
