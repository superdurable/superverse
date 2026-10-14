// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// The Project's Preview keeps its configuration and storage grant in the
// project's Live storage, which is all it needs: no project Dex or Temporal
// runs. Against the real Local Kind Live MinIO and Kubernetes API, its grant
// reads and writes only the Preview's prefix and never its connections, a
// replaced principal is refused, and the Preview's cleanup removes the grant
// and the Preview's configuration while the Live configuration stays readable.
func TestKindPreviewStorageGrantAndCleanup(t *testing.T) {
	fixture := newKindProjectFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	live := fixture.prepareStorage(ctx)
	liveConfiguration := fixture.verifyConfigurationBeforeRelease(ctx, live)
	preview := fixture.previewOf(live)
	previewConfiguration := fixture.verifyConfigurationBeforeRelease(ctx, preview)
	keys := infra.PreviewKeysOf(fixture.project)
	principal := fixture.createPrincipal(ctx)

	grant := infra.Workload{
		ProjectID: fixture.project, WorkloadID: keys.GrantWorkloadID, Scope: keys.Scope, Generation: 1, ConfigurationRevision: 1,
		Storage: live.storage, Configuration: previewConfiguration, GrantID: infra.WorkloadName(keys.GrantWorkloadID, 1), Principal: &principal,
	}
	var environment map[string]string
	var err error
	for environment, err = fixture.k.GrantEnvironment(ctx, grant); errors.Is(err, infra.ErrPending); environment, err = fixture.k.GrantEnvironment(ctx, grant) {
		fixture.awaitConvergence(ctx, "Preview grant")
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, session := environment["DEX_PROJECT_SESSION_ID"]; environment["DEX_PROJECT_SCOPE"] != "preview" || session || environment["DEX_PROJECT_STORAGE_BUCKET"] != live.storage.StorageBucket {
		t.Fatal("the Preview grant environment does not name the Project's Preview scope in the Live storage alone")
	}
	client := fixture.grantedStorage(ctx, live, environment)
	bucket := aws.String(live.storage.StorageBucket)
	if _, err = client.GetObject(ctx, &s3.GetObjectInput{Bucket: bucket, Key: aws.String(previewConfiguration.Key), VersionId: aws.String(previewConfiguration.Version)}); err != nil {
		t.Fatal("the Preview grant cannot read its accepted configuration", err)
	}
	_, err = client.GetObject(ctx, &s3.GetObjectInput{Bucket: bucket, Key: aws.String(liveConfiguration.Key), VersionId: aws.String(liveConfiguration.Version)})
	requireForbidden(t, "read "+liveConfiguration.Key, err)
	// Applications read connection credentials through the credential broker, never from storage.
	for _, denied := range []string{keys.ConfigurationPrefix + "connections/kind-test/head", "project-configuration/projects/" + fixture.project + "/live/connections/kind-test/head"} {
		_, err = client.PutObject(ctx, &s3.PutObjectInput{Bucket: bucket, Key: aws.String(denied), Body: strings.NewReader("denied")})
		requireForbidden(t, "write "+denied, err)
	}

	// A Trigger application keeps the events it acknowledged in an inbox of its
	// scope, which the Connector SDK writes under this grant; the Live scope's
	// inbox stays out of reach.
	store, err := projectconfig.NewS3ObjectStore(ctx, &projectconfig.S3StoreConfig{Client: client, Bucket: live.storage.StorageBucket, Prefix: infra.ConfigurationPrefix, AllowUnencrypted: true})
	if err != nil {
		t.Fatal(err)
	}
	inbox := func(kind infra.ScopeKind) *projectconfig.TriggerInbox {
		inbox, inboxErr := projectconfig.NewTriggerInbox(store, projectconfig.Scope{ProjectID: fixture.project, Kind: kind.String()},
			projectconfig.TriggerInboxKey{ConnectorID: "gmail", ConnectionName: "mailbox", TriggerName: "messageReceived", BindingName: "inbox"})
		if inboxErr != nil {
			t.Fatal(inboxErr)
		}
		return inbox
	}
	event := projectconfig.PendingTriggerEvent{ID: "event-1", Event: json.RawMessage(`{"subject":"hello"}`)}
	if err = inbox(infra.ScopePreview).Add(ctx, event); err != nil {
		t.Fatal("the Preview grant cannot add an event to its trigger inbox", err)
	}
	if pending, readErr := inbox(infra.ScopePreview).Pending(ctx); readErr != nil || len(pending) != 1 || pending[0].ID != event.ID {
		t.Fatalf("the Preview grant read %d pending events (%v), not the one it added", len(pending), readErr)
	}
	if err = inbox(infra.ScopePreview).Remove(ctx, event.ID); err != nil {
		t.Fatal("the Preview grant cannot remove a consumed event from its trigger inbox", err)
	}
	if err = inbox(infra.ScopeLive).Add(ctx, event); err == nil {
		t.Fatal("the Preview grant added an event to the Live scope's trigger inbox")
	}

	// A grant whose principal was replaced is refused before any effect.
	for _, replaced := range []infra.Principal{
		{Boundary: infra.ResourceRef{Name: principal.Boundary.Name, UID: "replaced-namespace"}, Identity: principal.Identity},
		{Boundary: principal.Boundary, Identity: infra.ResourceRef{Boundary: principal.Identity.Boundary, Name: principal.Identity.Name, UID: "replaced-service-account"}},
	} {
		next := grant
		next.Generation, next.GrantID, next.Principal = 2, infra.WorkloadName(keys.GrantWorkloadID, 2), &replaced
		if err = fixture.k.IssueWorkload(ctx, next); !errors.Is(err, infra.ErrOwnershipConflict) {
			t.Fatalf("a grant to a replaced principal was not refused: %v", err)
		}
	}

	if err = fixture.k.RevokeWorkload(ctx, grant); err != nil {
		t.Fatal(err)
	}
	if _, err = client.GetObject(ctx, &s3.GetObjectInput{Bucket: bucket, Key: aws.String(previewConfiguration.Key), VersionId: aws.String(previewConfiguration.Version)}); err == nil {
		t.Fatal("a revoked Preview grant still reads its configuration")
	}

	// The Preview's cleanup removes the grant, fence included, and the
	// Preview's configuration; a repeated call finds nothing left.
	for range 2 {
		for err = fixture.k.DestroyPreviewGrants(ctx, fixture.project, live.storage, principal.Boundary.Name); errors.Is(err, infra.ErrPending); err = fixture.k.DestroyPreviewGrants(ctx, fixture.project, live.storage, principal.Boundary.Name) {
			fixture.awaitConvergence(ctx, "Preview grant cleanup")
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	credential, code, err := fixture.k.API.Call(ctx, http.MethodGet, fixture.k.NamespacePath(fixture.project)+"/secrets/"+grant.Name()+"-storage", nil)
	if err != nil || code != http.StatusNotFound {
		t.Fatalf("the Preview's cleanup kept the grant credential %s: %v", kubeapi.UID(credential), err)
	}
	for range 2 {
		for err = fixture.k.DestroyPreviewConfiguration(ctx, fixture.project, live.storage); errors.Is(err, infra.ErrPending); err = fixture.k.DestroyPreviewConfiguration(ctx, fixture.project, live.storage) {
			fixture.awaitConvergence(ctx, "Preview configuration cleanup")
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = fixture.k.ReadConfiguration(ctx, preview.ref(fixture.project), previewConfiguration); err == nil {
		t.Fatal("the Preview's cleanup left the Preview configuration readable")
	}
	if _, err = fixture.k.ReadConfiguration(ctx, live.ref(fixture.project), liveConfiguration); err != nil {
		t.Fatalf("the Preview's cleanup crossed into %s: %v", liveConfiguration.Key, err)
	}
	t.Logf("verified Preview grant %s on Live bucket %s, principal %s/%s, and the Preview's cleanup", grant.Name(), live.storage.StorageBucket, principal.Boundary.UID, principal.Identity.UID)
}

// createPrincipal creates, with the test kubeconfig, the Sandbox namespace,
// delegated session-manager RoleBinding, and service account the Project's
// Preview grant binds, with the Preview's owner labels, and removes them at the
// end of the test.
func (f *kindProjectFixture) createPrincipal(ctx context.Context) infra.Principal {
	f.test.Helper()
	labels := kubeapi.Object{kubeapi.ManagedByLabel: f.k.Names().Product}
	for key, value := range infrakube.PreviewLabels(f.project) {
		labels[key] = value
	}
	name := f.k.Names().SessionNamespace(f.project)
	namespace := f.kubectlCreate(ctx, kubeapi.New("v1", kubeapi.KindNamespace, kubeapi.Metadata("", name, labels), nil))
	f.test.Cleanup(func() {
		// Cleanup runs after the test's context ended.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := exec.CommandContext(cleanup, "kubectl", "--kubeconfig", f.kubeconfig, "--context", "kind-superverse-v2", "delete", "namespace", name, "--wait=false").Run(); err != nil {
			f.test.Errorf("delete the principal namespace %s: %v", name, err)
		}
	})
	managerRole := f.k.Names().Product + "-session-manager"
	f.kubectlCreate(ctx, kubeapi.BindClusterRole(
		kubeapi.KindRoleBinding,
		kubeapi.Metadata(name, managerRole, labels),
		managerRole,
		kubeapi.ServiceAccountSubject(f.k.Names().PlatformNamespace, "superverse-server"),
	))
	account := f.kubectlCreate(ctx, kubeapi.ServiceAccount(kubeapi.Metadata(name, "coding-sandbox", labels)))
	return infra.Principal{Boundary: infrakube.ResourceReference(namespace), Identity: infrakube.ResourceReference(account)}
}

// kubectlCreate creates object with the test kubeconfig and returns it.
func (f *kindProjectFixture) kubectlCreate(ctx context.Context, object map[string]any) kubeapi.Object {
	f.test.Helper()
	encoded, err := json.Marshal(object)
	if err != nil {
		f.test.Fatal(err)
	}
	command := exec.CommandContext(ctx, "kubectl", "--kubeconfig", f.kubeconfig, "--context", "kind-superverse-v2", "create", "-o", "json", "-f", "-")
	command.Stdin = bytes.NewReader(encoded)
	output, err := command.Output()
	if err != nil {
		f.test.Fatalf("create a test %v: %v", object["kind"], err)
	}
	var created kubeapi.Object
	if err = json.Unmarshal(output, &created); err != nil || kubeapi.UID(created) == "" {
		f.test.Fatalf("invalid created %v: %v", object["kind"], err)
	}
	return created
}

// grantedStorage is an S3 client of the Live MinIO that authenticates with a
// grant's credentials and connects through a port-forward of its Service.
func (f *kindProjectFixture) grantedStorage(_ context.Context, _ *kindProjectScope, environment map[string]string) *s3.Client {
	f.test.Helper()
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(environment["DEX_PROJECT_STORAGE_ENDPOINT"]), Region: environment["AWS_REGION"], UsePathStyle: true, HTTPClient: f.k.ServiceHTTP(),
		Credentials: awscredentials.NewStaticCredentialsProvider(environment["AWS_ACCESS_KEY_ID"], environment["AWS_SECRET_ACCESS_KEY"], ""),
	})
}

// requireForbidden fails the test unless MinIO answered the operation 403.
func requireForbidden(t *testing.T, operation string, err error) {
	t.Helper()
	var response *smithyhttp.ResponseError
	if !errors.As(err, &response) || response.HTTPStatusCode() != http.StatusForbidden {
		t.Fatalf("the Preview grant was not forbidden to %s: %v", operation, err)
	}
}
