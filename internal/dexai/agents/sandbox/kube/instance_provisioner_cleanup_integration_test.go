// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package kube

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	infrakube "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi/kubeapitest"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// Injects only actual Kubernetes impersonation headers. All responses, RBAC
// decisions and namespace lifecycle transitions come from the real API server.
type codingCleanupImpersonation struct{ user string }

func (t codingCleanupImpersonation) RoundTrip(r *http.Request) (*http.Response, error) {
	request := r.Clone(r.Context())
	request.Header.Set("Impersonate-User", t.user)
	return http.DefaultTransport.RoundTrip(request)
}

// Requires an operator-owned kubectl proxy to the isolated Local Kind cluster.
// It creates only its random namespace and exact-name RBAC resources. No server
// or test double is started. Missing real dependencies are an explicit failure.
func TestCodingNamespaceCleanupKubernetesIntegration(t *testing.T) {
	endpoint := os.Getenv("SUPERVERSE_TEST_KUBERNETES_API")
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
		t.Fatal("requires an owned real Local Kind API proxy in SUPERVERSE_TEST_KUBERNETES_API")
	}
	project := flowid.RandomID(12)
	api := &kubeapi.Client{ServerURL: endpoint, HTTP: http.DefaultClient}
	provisioner := &InstanceProvisioner{Infrastructure: &infrakube.Infrastructure{API: api, Environment: deploymentnames.EnvironmentTest}}
	id := sandbox.Identity{ProjectID: project}
	// owned names the instance as its Flow records it: generation 1 in this namespace.
	owned := func(uid string, resources sandbox.Resources) sandbox.Instance {
		resources.Boundary = sandbox.ResourceRef{Name: provisioner.namespace(id), UID: uid}
		return sandbox.Instance{Identity: id, Generation: 1, Resources: resources}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	ns := provisioner.namespace(id)
	name := "sv2-cleanup-" + project
	call := func(method, path string, body any) kubeapi.Object {
		t.Helper()
		o, _, e := api.Call(ctx, method, path, body)
		if e != nil {
			t.Fatal(e)
		}
		return o
	}
	metadata := kubeapi.Metadata("", ns, provisioner.labels(sandbox.Instance{Identity: id, Generation: 1}))
	namespace := call(http.MethodPost, "/api/v1/namespaces", kubeapi.New("v1", kubeapi.KindNamespace, metadata, kubeapi.Object{"finalizers": []string{"kubernetes", "superverse.dev/cleanup-test"}}))
	uid := resource(namespace).UID
	releaseTestFinalizer := func(callCtx context.Context, current kubeapi.Object) error {
		finalizers := kubeapi.Items(kubeapi.Child(current, "spec"), "finalizers")
		retained := make([]any, 0, len(finalizers))
		for _, finalizer := range finalizers {
			if finalizer != "superverse.dev/cleanup-test" {
				retained = append(retained, finalizer)
			}
		}
		if len(retained) == len(finalizers) {
			return nil
		}
		current["spec"] = kubeapi.Object{"finalizers": retained}
		_, _, releaseErr := api.Call(callCtx, http.MethodPut, "/api/v1/namespaces/"+ns+"/finalize", current)
		return releaseErr
	}
	// removeNamespace deletes the test namespace by its exact UID and waits
	// until it is gone, releasing the test finalizer on every pass.
	removeNamespace := func(ctx context.Context) error {
		current, code, e := api.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+ns, nil)
		if e != nil || code == http.StatusNotFound {
			return e
		}
		if resource(current).UID != uid {
			return errors.New("cleanup namespace UID replaced")
		}
		if _, _, e = api.Call(ctx, http.MethodDelete, "/api/v1/namespaces/"+ns, kubeapi.DeleteOptions(uid)); e != nil {
			return e
		}
		return pollUntil(ctx, 100*time.Millisecond, func() (bool, error) {
			current, code, getErr := api.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+ns, nil)
			if getErr != nil || code == http.StatusNotFound {
				return getErr == nil, getErr
			}
			if resource(current).UID != uid {
				return false, errors.New("cleanup namespace UID replaced")
			}
			return false, releaseTestFinalizer(ctx, current)
		})
	}

	rolePath := "/apis/rbac.authorization.k8s.io/v1/clusterroles"
	bindingPath := "/apis/rbac.authorization.k8s.io/v1/clusterrolebindings"
	var role, binding kubeapi.Object
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if e := removeNamespace(cleanup); e != nil {
			t.Error("namespace cleanup did not converge:", e)
		}
		for path, ref := range map[string]sandbox.ResourceRef{bindingPath: resource(binding), rolePath: resource(role)} {
			if ref.UID == "" {
				continue
			}
			if _, _, e := api.Call(cleanup, http.MethodDelete, path+"/"+name, kubeapi.DeleteOptions(ref.UID)); e != nil {
				t.Error(e)
			}
		}
	})
	role = call(http.MethodPost, rolePath, kubeapi.ClusterRoleForTest(name, kubeapi.Object{"apiGroups": []string{""}, "resources": []string{"namespaces"}, "resourceNames": []string{ns}, "verbs": []string{"get"}}))
	binding = call(http.MethodPost, bindingPath, kubeapi.BindClusterRole(kubeapitest.KindClusterRoleBindingForTest, kubeapi.Metadata("", name, nil), name, kubeapi.ServiceAccountSubject(ns, "cleanup-probe")))
	call(http.MethodPost, "/api/v1/namespaces/"+ns+"/serviceaccounts", kubeapi.ServiceAccount(kubeapi.Metadata("", "cleanup-probe", nil)))
	limited := *provisioner
	limited.API = &kubeapi.Client{ServerURL: endpoint, HTTP: &http.Client{Transport: codingCleanupImpersonation{"system:serviceaccount:" + ns + ":cleanup-probe"}}}
	// Read Namespace with the exact ClusterRole, but deny Deployment access.
	converge, stopConverging := context.WithTimeout(ctx, 15*time.Second)
	defer stopConverging()
	var readErr error
	if err = pollUntil(converge, 100*time.Millisecond, func() (bool, error) {
		_, readErr = limited.readNamespace(ctx, owned(uid, sandbox.Resources{}))
		return readErr == nil, nil
	}); err != nil {
		t.Fatal("real namespace RBAC did not converge", readErr, err)
	}
	_, _, denied := limited.API.Call(ctx, http.MethodGet, "/apis/apps/v1/namespaces/"+ns+"/deployments/dex", nil)
	var forbidden *kubeapi.StatusError
	if !errors.As(denied, &forbidden) || forbidden.Status != http.StatusForbidden {
		t.Fatalf("expected actual Deployment RBAC denial (%T)", denied)
	}
	if _, err = limited.ReleasePreviewRoute(ctx, owned(uid, sandbox.Resources{})); !errors.As(err, &forbidden) {
		t.Fatalf("active namespace denial was hidden (%T)", err)
	}
	if _, err = provisioner.readNamespace(ctx, owned("different-uid", sandbox.Resources{})); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatal("changed UID was adopted")
	}
	wrongScope := owned(uid, sandbox.Resources{})
	wrongScope.Resources.Boundary.Name = "different-scope"
	if _, err = provisioner.readNamespace(ctx, wrongScope); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatal("wrong namespace reference was adopted")
	}
	call(http.MethodDelete, "/api/v1/namespaces/"+ns, kubeapi.DeleteOptions(uid))
	if state, stateErr := limited.readNamespace(ctx, owned(uid, sandbox.Resources{})); stateErr != nil || state != namespaceTerminating {
		t.Fatalf("terminating namespace did not remain pending (%v, %T)", state, stateErr)
	}
	if released, releaseErr := limited.ReleasePreviewRoute(ctx, owned(uid, sandbox.Resources{})); releaseErr != nil || released {
		t.Fatalf("terminating namespace RBAC race was treated as failure or completion (%T)", releaseErr)
	}
	if _, err = limited.readNamespace(ctx, owned("different-uid", sandbox.Resources{})); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatal("terminating replacement UID was adopted")
	}
	current := call(http.MethodGet, "/api/v1/namespaces/"+ns, nil)
	if err = releaseTestFinalizer(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err = pollUntil(ctx, 100*time.Millisecond, func() (bool, error) {
		state, readErr := provisioner.readNamespace(ctx, owned(uid, sandbox.Resources{}))
		return state == namespaceGone, readErr
	}); err != nil {
		t.Fatal(err)
	}
	if released, releaseErr := limited.ReleasePreviewRoute(ctx, owned(uid, sandbox.Resources{})); releaseErr != nil || !released {
		t.Fatal("absence did not reconcile original namespaced denial", releaseErr)
	}
	if stopped, stopErr := limited.StopPreview(ctx, sandbox.Instance{Identity: id, Generation: 1}, "never-provisioned"); stopErr != nil || !stopped {
		t.Fatal("partial provisioning absence did not reconcile", stopErr)
	}
	for _, remove := range []func(context.Context, sandbox.Instance) (bool, error){limited.DeletePod, limited.DeleteVolume} {
		done, removalErr := remove(ctx, owned(uid, sandbox.Resources{Instance: sandbox.ResourceRef{Name: "gone-sandbox", UID: "retained-gone"}, Volume: sandbox.ResourceRef{Name: "gone-volume", UID: "retained-gone"}}))
		if removalErr != nil || !done {
			t.Fatal("retained namespaced resource absence did not reconcile", removalErr)
		}
	}

	originalUID := uid
	replacement := call(http.MethodPost, "/api/v1/namespaces", kubeapi.New("v1", kubeapi.KindNamespace, kubeapi.Metadata("", ns, provisioner.labels(sandbox.Instance{Identity: id, Generation: 1})), nil))
	uid = resource(replacement).UID
	if uid == originalUID {
		t.Fatal("actual replacement did not change namespace UID")
	}
	if _, err = provisioner.readNamespace(ctx, owned(originalUID, sandbox.Resources{})); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatal("actual replacement namespace was adopted")
	}
	kubeapi.Labels(replacement)[kubeapi.ProjectLabel] = "differentowner"
	call(http.MethodPut, "/api/v1/namespaces/"+ns, replacement)
	if _, err = provisioner.readNamespace(ctx, owned(uid, sandbox.Resources{})); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatal("wrong-owner namespace was adopted")
	}
	t.Logf("actual namespace %s UID %s: active denial, UID rejection, terminating pending, confirmed absence", ns, uid)
}
