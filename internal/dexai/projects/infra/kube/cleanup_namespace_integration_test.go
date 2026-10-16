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

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi/kubeapitest"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// loopbackHost is the only host a test reaches its Local Kind API proxy on.
const loopbackHost = "127.0.0.1"

// Injects only actual Kubernetes impersonation headers. All responses, RBAC
// decisions and namespace lifecycle transitions come from the real API server.
type cleanupImpersonation struct{ user string }

func (t cleanupImpersonation) RoundTrip(r *http.Request) (*http.Response, error) {
	request := r.Clone(r.Context())
	request.Header.Set("Impersonate-User", t.user)
	return http.DefaultTransport.RoundTrip(request)
}

// Requires an operator-owned kubectl proxy to the isolated Local Kind cluster.
// It creates only its random namespace and exact-name RBAC resources. No server
// or test double is started. Missing real dependencies are an explicit failure.
func TestCleanupNamespaceKubernetesIntegration(t *testing.T) {
	endpoint := os.Getenv("SUPERVERSE_TEST_KUBERNETES_API")
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != loopbackHost {
		t.Fatal("requires an owned real Local Kind API proxy in SUPERVERSE_TEST_KUBERNETES_API")
	}
	project := flowid.RandomID(12)
	api := &kubeapi.Client{ServerURL: endpoint, HTTP: http.DefaultClient}
	infrastructure := &Infrastructure{API: api, Environment: deploymentnames.EnvironmentTest}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	ns := infrastructure.ProjectNamespace(project)
	name := "sv2-cleanup-" + project
	call := func(method, path string, body any) kubeapi.Object {
		t.Helper()
		o, _, e := api.Call(ctx, method, path, body)
		if e != nil {
			t.Fatal(e)
		}
		return o
	}
	metadata := kubeapi.Metadata("", ns, infrastructure.OwnerLabels(project, ComponentInfrastructure))
	namespace := call(http.MethodPost, "/api/v1/namespaces", kubeapi.New("v1", kubeapi.KindNamespace, metadata, kubeapi.Object{"finalizers": []string{"kubernetes", "superverse.dev/cleanup-test"}}))
	uid := ResourceReference(namespace).UID
	releaseTestFinalizer := func(callCtx context.Context, current kubeapi.Object) error {
		finalizers, _ := kubeapi.Child(current, "spec")["finalizers"].([]any)
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

	rolePath := "/apis/rbac.authorization.k8s.io/v1/clusterroles"
	bindingPath := "/apis/rbac.authorization.k8s.io/v1/clusterrolebindings"
	var role, binding kubeapi.Object
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		current, code, e := api.Call(cleanup, http.MethodGet, "/api/v1/namespaces/"+ns, nil)
		if e != nil {
			t.Error(e)
		} else if code != http.StatusNotFound {
			if ResourceReference(current).UID != uid {
				t.Error("cleanup namespace UID replaced")
			} else if _, _, e = api.Call(cleanup, http.MethodDelete, "/api/v1/namespaces/"+ns, kubeapi.DeleteOptions(uid)); e != nil {
				t.Error(e)
			} else {
				for {
					current, code, e = api.Call(cleanup, http.MethodGet, "/api/v1/namespaces/"+ns, nil)
					if e != nil || code == http.StatusNotFound {
						break
					}
					if ResourceReference(current).UID != uid {
						t.Error("cleanup namespace UID replaced")
						break
					}
					if e = releaseTestFinalizer(cleanup, current); e != nil {
						t.Error(e)
						break
					}

					select {
					case <-cleanup.Done():
						t.Error("namespace cleanup did not converge")
					case <-time.After(100 * time.Millisecond):
						continue
					}
					break
				}
				if e != nil {
					t.Error(e)
				}
			}
		}
		for path, ref := range map[string]infra.ResourceRef{bindingPath: ResourceReference(binding), rolePath: ResourceReference(role)} {
			if ref.UID == "" {
				continue
			}
			if _, _, e = api.Call(cleanup, http.MethodDelete, path+"/"+name, kubeapi.DeleteOptions(ref.UID)); e != nil {
				t.Error(e)
			}
		}
	})
	role = call(http.MethodPost, rolePath, kubeapi.ClusterRoleForTest(name, kubeapi.Object{"apiGroups": []string{""}, "resources": []string{"namespaces"}, "resourceNames": []string{ns}, "verbs": []string{"get"}}))
	binding = call(http.MethodPost, bindingPath, kubeapi.BindClusterRole(kubeapitest.KindClusterRoleBindingForTest, kubeapi.Metadata("", name, nil), name, kubeapi.ServiceAccountSubject(ns, "cleanup-probe")))
	call(http.MethodPost, "/api/v1/namespaces/"+ns+"/serviceaccounts", kubeapi.ServiceAccount(kubeapi.Metadata("", "cleanup-probe", nil)))
	limited := *infrastructure
	limited.API = &kubeapi.Client{ServerURL: endpoint, HTTP: &http.Client{Transport: cleanupImpersonation{"system:serviceaccount:" + ns + ":cleanup-probe"}}}
	// Read Namespace with the exact ClusterRole, but deny Deployment access.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err = limited.CleanupNamespace(ctx, project, uid); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("real namespace RBAC did not converge", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	_, _, denied := limited.API.Call(ctx, http.MethodGet, "/apis/apps/v1/namespaces/"+ns+"/deployments/dex", nil)
	var forbidden *kubeapi.StatusError
	if !errors.As(denied, &forbidden) || forbidden.Status != http.StatusForbidden {
		t.Fatalf("expected actual Deployment RBAC denial (%T)", denied)
	}
	if err = limited.ReconcileCleanupError(ctx, project, uid, denied); !errors.As(err, &forbidden) {
		t.Fatalf("active namespace denial was hidden (%T)", err)
	}
	if _, err = infrastructure.CleanupNamespace(ctx, project, "different-uid"); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatal("changed UID was adopted")
	}
	call(http.MethodDelete, "/api/v1/namespaces/"+ns, kubeapi.DeleteOptions(uid))
	if _, err = limited.CleanupNamespace(ctx, project, uid); !errors.Is(err, infra.ErrPending) {
		t.Fatalf("terminating namespace did not remain pending (%T)", err)
	}
	if err = limited.ReconcileCleanupError(ctx, project, uid, denied); !errors.Is(err, infra.ErrPending) {
		t.Fatalf("terminating namespace RBAC race was treated as failure (%T)", err)
	}
	if _, err = limited.CleanupNamespace(ctx, project, "different-uid"); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatal("terminating replacement UID was adopted")
	}
	current := call(http.MethodGet, "/api/v1/namespaces/"+ns, nil)
	if err = releaseTestFinalizer(ctx, current); err != nil {
		t.Fatal(err)
	}
	for {
		observed, e := infrastructure.CleanupNamespace(ctx, project, uid)
		if e == nil && observed == nil {
			break
		}
		if e != nil && !errors.Is(e, infra.ErrPending) {
			t.Fatal(e)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err = limited.ReconcileCleanupError(ctx, project, uid, denied); err != nil {
		t.Fatal("absence did not reconcile original namespaced denial", err)
	}
	originalUID := uid
	replacement := call(http.MethodPost, "/api/v1/namespaces", kubeapi.New("v1", kubeapi.KindNamespace, kubeapi.Metadata("", ns, infrastructure.OwnerLabels(project, ComponentInfrastructure)), nil))
	uid = ResourceReference(replacement).UID
	if uid == originalUID {
		t.Fatal("actual replacement did not change namespace UID")
	}
	if _, err = infrastructure.CleanupNamespace(ctx, project, originalUID); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatal("actual replacement namespace was adopted")
	}
	kubeapi.Labels(replacement)[kubeapi.ProjectLabel] = "differentowner"
	call(http.MethodPut, "/api/v1/namespaces/"+ns, replacement)
	if _, err = infrastructure.CleanupNamespace(ctx, project, uid); !errors.Is(err, infra.ErrOwnershipConflict) {
		t.Fatal("wrong-owner namespace was adopted")
	}
	t.Logf("actual namespace %s UID %s: active denial, UID rejection, terminating pending, confirmed absence", ns, uid)
}
