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
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	madmin "github.com/minio/madmin-go/v3"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
)

// A Trigger application keeps the events it acknowledged in an inbox below its
// scope in the Project's storage, which the Connector SDK writes with
// conditional writes under the application's own grant. Against the real MinIO
// of the local lane, the grant of a Live generation and the grant of the
// Preview each add, read and remove events of their own scope's inboxes through
// the SDK's inbox, and are refused the inboxes of another Project and of the
// other scope.
func TestMinIOGrantAllowsTriggerInbox(t *testing.T) {
	endpoint, u := loopbackMinIO(t)
	parent, password := os.Getenv(AccessKeyIDVariable), os.Getenv(SecretAccessKeyVariable)
	admin, err := madmin.NewWithOptions(u.Host, &madmin.Options{Creds: credentials.NewStaticV4(parent, password, "")})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	root := ProjectS3(endpoint, parent, password, nil)
	bucket := "sv2-inbox-" + RandomSecret()[:10]
	storage := StorageRefs{StorageBucket: bucket, BlobPrefix: ConfigurationPrefix}
	if _, err = root.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeBucket(t, root, bucket) })
	if _, err = root.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{Bucket: aws.String(bucket), VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled}}); err != nil {
		t.Fatal(err)
	}
	preview := PreviewKeysOf("abcd")
	for _, grant := range []Workload{
		{ProjectID: "abcd", WorkloadID: "live", Scope: Scope{Kind: ScopeLive}, Generation: 1},
		{ProjectID: "abcd", WorkloadID: preview.GrantWorkloadID, Scope: preview.Scope, Generation: 1},
	} {
		t.Run(grant.Scope.Kind.String(), func(t *testing.T) {
			grant.Storage = storage
			grant.Configuration = objectstore.ObjectRef{Key: configurationKey(grant.ProjectID, grant.Scope, "configuration/head")}
			access, secret := RandomSecret()[:20], RandomSecret()[:32]
			t.Cleanup(func() {
				if deleteErr := admin.DeleteServiceAccount(context.Background(), access); deleteErr != nil {
					t.Error("test grant cleanup failed", deleteErr)
				}
			})
			if err := EnsureStorageAccount(ctx, admin, parent, access, secret, "", GrantPolicy(grant, nil)); err != nil {
				t.Fatal(err)
			}
			store, err := projectconfig.NewS3ObjectStore(ctx, &projectconfig.S3StoreConfig{Client: ProjectS3(endpoint, access, secret, nil), Bucket: bucket, Prefix: ConfigurationPrefix, AllowUnencrypted: true})
			if err != nil {
				t.Fatal(err)
			}
			inbox := func(project string, kind ScopeKind, binding string) *projectconfig.TriggerInbox {
				inbox, err := projectconfig.NewTriggerInbox(store, projectconfig.Scope{ProjectID: project, Kind: kind.String()},
					projectconfig.TriggerInboxKey{ConnectorID: "gmail", ConnectionName: "mailbox", TriggerName: "messageReceived", BindingName: binding})
				if err != nil {
					t.Fatal(err)
				}
				return inbox
			}
			// The first event creates the inbox, the second replaces it conditionally, the removal replaces it again.
			own := inbox(grant.ProjectID, grant.Scope.Kind, "inbox")
			for _, id := range []string{"event-1", "event-2"} {
				if err = own.Add(ctx, projectconfig.PendingTriggerEvent{ID: id, Event: json.RawMessage(`{"subject":"hello"}`)}); err != nil {
					t.Fatalf("the grant cannot add %s to its scope's trigger inbox: %v", id, err)
				}
			}
			if pending, readErr := own.Pending(ctx); readErr != nil || len(pending) != 2 || pending[0].ID != "event-1" || pending[1].ID != "event-2" {
				t.Fatalf("the grant read %d pending events (%v), not the two it added", len(pending), readErr)
			}
			if err = own.Remove(ctx, "event-1"); err != nil {
				t.Fatalf("the grant cannot remove a consumed event from its scope's trigger inbox: %v", err)
			}
			if pending, readErr := own.Pending(ctx); readErr != nil || len(pending) != 1 || pending[0].ID != "event-2" {
				t.Fatalf("the grant read %d pending events (%v) after the removal, not event-2 alone", len(pending), readErr)
			}
			// Another Project's inbox and the other scope's inbox stay out of reach.
			other := ScopePreview
			if grant.Preview() {
				other = ScopeLive
			}
			for name, denied := range map[string]*projectconfig.TriggerInbox{
				"another Project's": inbox("other", grant.Scope.Kind, "inbox"),
				"the other scope's": inbox(grant.ProjectID, other, "inbox"),
			} {
				if err = denied.Add(ctx, projectconfig.PendingTriggerEvent{ID: "event-3", Event: json.RawMessage(`{}`)}); err == nil {
					t.Fatalf("the grant added an event to %s trigger inbox", name)
				}
			}
		})
	}
}
