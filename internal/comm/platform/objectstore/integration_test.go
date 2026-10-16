// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package objectstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

func TestVersionedS3Integration(t *testing.T) {
	endpoint, bucket := os.Getenv("SUPERVERSE_S3_ENDPOINT"), os.Getenv("SUPERVERSE_ARTIFACT_BUCKET")
	if endpoint == "" || bucket == "" {
		t.Fatal("requires isolated Local Kind versioned artifact bucket")
	}
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatal("integration requires explicit loopback S3 endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = &endpoint; o.UsePathStyle = true }), bucket, "", "sv2-test", true)
	if err != nil {
		t.Fatal(err)
	}
	id := flowid.RandomID(12)
	key := "integration-" + id + "/configuration/1"
	// document is the test's stored JSON object.
	type document struct {
		Enabled  bool `json:"enabled"`
		Revision int  `json:"revision,omitempty"`
	}
	ref, err := store.PutJSON(ctx, key, document{Enabled: true, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer deleteVersions(t, store, ref)
	same, err := store.PutJSON(ctx, key, document{Enabled: true, Revision: 1})
	if err != nil || same != ref {
		t.Fatal("repeated write changed version", err)
	}
	if _, err = store.PutJSON(ctx, key, document{Enabled: false}); !errors.Is(err, errImmutableConflict) {
		t.Fatal("immutable conflict not enforced", err)
	}
	var result document
	if err = store.GetJSON(ctx, ref, &result); err != nil || !result.Enabled {
		t.Fatal("exact immutable read failed", err)
	}
	// A reference whose digest differs from the stored version is rejected
	// with both digests, so the failure names what changed.
	changed := ref
	changed.Digest = Digest([]byte("another payload"))
	if _, err = store.Get(ctx, changed); !errors.Is(err, errInvalidObject) || !strings.Contains(err.Error(), ref.Digest) || !strings.Contains(err.Error(), changed.Digest) {
		t.Fatal("a changed digest was not rejected with both digests", err)
	}
	t.Logf("verified immutable versioned artifact %s; cleanup removes only its exact version", ref.Key)

	// Exercise bounded version listing/deletion through the actual S3 API,
	// while proving that another Session and Project retain their own objects.
	project, session := "p"+id, "s"+id
	refs := make([]ObjectRef, 0, 22)
	defer func() { deleteVersions(t, store, refs...) }()
	for index := 0; index < 20; index++ {
		refs = append(refs, putVersion(ctx, t, store, fmt.Sprintf("projects/%s/sessions/%s/preview-source/%040x/definitions.json", project, session, index), map[string]any{"commit": index}))
	}
	for _, scope := range [][2]string{{project, "other" + id}, {"other" + id, session}} {
		refs = append(refs, putVersion(ctx, t, store, "projects/"+scope[0]+"/sessions/"+scope[1]+"/preview-source/retained/definitions.json", map[string]any{"retained": true}))
	}
	prefix := "projects/" + project + "/sessions/" + session + "/preview-source/"
	done, err := store.DeletePrefix(ctx, prefix)
	if err != nil || done {
		t.Fatal("cleanup must report unfinished after its first bounded batch", done, err)
	}
	for attempt := 0; attempt < 4 && !done; attempt++ {
		done, err = store.DeletePrefix(ctx, prefix)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !done {
		t.Fatal("bounded Session cleanup did not converge")
	}
	if _, err = store.Get(ctx, refs[0]); err == nil {
		t.Fatal("deleted Session source version is still readable")
	}
	for _, ref := range refs[20:] {
		if _, err = store.Get(ctx, ref); err != nil {
			t.Fatal("Session cleanup crossed its exact scope", err)
		}
	}
	done = false
	for attempt := 0; attempt < 4 && !done; attempt++ {
		done, err = store.DeleteProjectObjects(ctx, project)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !done {
		t.Fatal("Project cleanup did not reclaim its Session source prefix")
	}
	if _, err = store.Get(ctx, refs[20]); err == nil {
		t.Fatal("Project Session source version remains readable")
	}
	if _, err = store.Get(ctx, refs[21]); err != nil {
		t.Fatal("Project cleanup crossed its exact scope", err)
	}
}

// putVersion writes one JSON object and fails the test when it cannot.
func putVersion(ctx context.Context, t *testing.T, store *Store, key string, value any) ObjectRef {
	t.Helper()
	ref, err := store.PutJSON(ctx, key, value)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

// deleteVersions removes the exact versions the test wrote.
func deleteVersions(t *testing.T, store *Store, refs ...ObjectRef) {
	t.Helper()
	for _, ref := range refs {
		if err := store.Delete(context.Background(), ref); err != nil {
			t.Error(err)
		}
	}
}
