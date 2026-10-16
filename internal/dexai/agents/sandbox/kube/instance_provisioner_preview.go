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
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

const previewAppName = "preview-app"

// previewMetadata names and labels one Preview object of the instance.
func (k *InstanceProvisioner) previewMetadata(s sandbox.Instance, name string) kubeapi.Object {
	labels := k.labels(s)
	labels[kubeapi.PartOfLabel] = k.Names().Product
	labels["app.kubernetes.io/component"] = "session-preview"
	return kubeapi.Metadata(k.namespace(s.Identity), name, labels)
}

// EnsurePreviewEnvironment starts the instance's Preview engine, or finds it
// running, and once the supervisor answers ready verifies that the engine's
// Dex answers through the instance's own Sandbox Service: infra.ErrPending
// while it does not yet, infra.ErrOwnershipConflict when a recorded object
// was replaced.
func (k *InstanceProvisioner) EnsurePreviewEnvironment(ctx context.Context, s sandbox.Instance) (workspace.PreviewAnswer, error) {
	answer, err := k.Workspace.EnsurePreviewEnvironment(ctx, s.Binding(), "environment/"+s.WorkspaceRequestID+"/ensure")
	if err != nil || answer.State != workspace.PreviewStateReady {
		return answer, err
	}
	return answer, k.Connections.ProbeSandboxDex(ctx, s.DexTarget())
}

// previewRoute is the route of the Project's Preview as the platform wants it
// now, in the order it is created: the application's Service, the policy that
// admits its port, and the HTTPRoute that names the Service.
func (k *InstanceProvisioner) previewRoute(s sandbox.Instance) []kubeapi.Object {
	labels := k.labels(s)
	service := kubeapi.Service(k.previewMetadata(s, previewAppName), labels, kubeapi.ServicePort("http", workspace.PreviewApplicationPort, workspace.PreviewApplicationPort))
	policy := kubeapi.New("networking.k8s.io/v1", kubeapi.KindNetworkPolicy, k.previewMetadata(s, previewPolicyName), kubeapi.Object{
		"podSelector": kubeapi.MatchLabels(labels), "policyTypes": []any{"Ingress"},
		"ingress": []any{kubeapi.AllowFrom(nil, kubeapi.TCPPort(workspace.PreviewApplicationPort))},
	})
	route := kubeapi.HTTPRoute(k.previewMetadata(s, previewAppName), k.RouteGateway(), k.Routes.Host(infra.ScopePreview, s.Identity.ProjectID), previewAppName, workspace.PreviewApplicationPort)
	return []kubeapi.Object{service, policy, route}
}

// previewPolicyName is the NetworkPolicy that admits the application's port.
const previewPolicyName = "allow-preview-app"

// reconcile makes the route object named by desired the one desired
// describes. It creates an absent object and keeps one that matches. The
// Sandbox namespace admits only creates and deletes of the route objects, so
// it deletes one that differs, with its UID as a precondition, and the next
// call creates it again; an object being deleted is waited for. current
// reports whether the object matches now.
func (k *InstanceProvisioner) reconcile(ctx context.Context, s sandbox.Instance, desired kubeapi.Object) (current bool, err error) {
	found, err := k.ensure(ctx, s, desired)
	if err != nil || kubeapi.Deleting(found) {
		return false, err
	}
	if kubeapi.Matches(found, desired) {
		return true, nil
	}
	_, err = k.remove(ctx, kubeapi.CollectionPath(desired)+"/"+kubeapi.String(kubeapi.Child(desired, "metadata"), "name"), s, resource(found))
	return false, err
}

// ExposePreview reconciles the Project's route to the instance and probes it
// through the real gateway. It returns the public URL once the application
// answers, and until then an empty URL with what it waits for. The route
// objects live in the Project's Sandbox namespace, where only the Project's
// Preview publishes them, and keep the Project's host, so a route that a
// changed platform spec replaces, or that a stop removed, comes back under
// the same URL.
func (k *InstanceProvisioner) ExposePreview(ctx context.Context, s sandbox.Instance) (publicURL, unanswered string, err error) {
	for _, desired := range k.previewRoute(s) {
		current, reconcileErr := k.reconcile(ctx, s, desired)
		if reconcileErr != nil || !current {
			return "", "the " + kubeapi.String(desired, "kind") + " of the route is being replaced", reconcileErr
		}
	}
	host := k.Routes.Host(infra.ScopePreview, s.Identity.ProjectID)
	publicURL = k.Routes.URL(infra.ScopePreview, s.Identity.ProjectID)
	// The probe reaches the platform's own gateway, whose address the server's
	// configuration and the Project ID make; gosec's taint analysis follows
	// the Kind integration test's settings into it (G704).
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(k.Backend.ProbeBase(k.Routes, publicURL), "/")+"/", nil) //nolint:gosec // G704: the platform's own gateway
	if err != nil {
		return "", "", err
	}
	request.Host = host
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, probeErr := client.Do(request) //nolint:gosec // G704: the platform's own gateway
	if probeErr != nil {
		// Not reachable through the gateway yet: the Step probes again.
		return "", "the probe failed: " + probeErr.Error(), nil
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return "", "the gateway answered " + response.Status, nil
	}
	return publicURL, "", nil
}

// StopPreview removes the Project's route and then stops the application, so
// that nothing listening on the application's port stays reachable while the
// Preview is stopped, and the gateway holds routes only for the Previews that
// serve. done is false while the namespace still terminates; an
// absent namespace has nothing left to stop.
func (k *InstanceProvisioner) StopPreview(ctx context.Context, s sandbox.Instance, commandID string) (bool, error) {
	if removed, err := k.ReleasePreviewRoute(ctx, s); err != nil || !removed {
		return false, err
	}
	state, err := k.readNamespace(ctx, s)
	if err != nil || state != namespaceActive {
		return state == namespaceGone, err
	}
	// A Preview process is only ever started through the Sandbox Service. A
	// Sandbox that failed before its Service was published has no supervisor
	// to ask and no binding to reach it by, so there is nothing to stop.
	if s.Resources.Instance.UID == "" || s.Resources.ServiceName == "" {
		return true, nil
	}
	answer, err := k.Workspace.StopPreview(ctx, s.Binding(), "stop/"+s.WorkspaceRequestID+"/"+commandID)
	return answer.State == workspace.PreviewStateStopped, err
}

// ReleasePreviewRoute deletes the Project's route objects from its Sandbox
// namespace, the HTTPRoute first so that the gateway stops routing before the
// Service and the policy go. A stop calls it, and so does the deleting
// Project's release for a route that no stop removed. done is false while the
// namespace still terminates; an absent namespace took them with it.
func (k *InstanceProvisioner) ReleasePreviewRoute(ctx context.Context, s sandbox.Instance) (bool, error) {
	state, err := k.readNamespace(ctx, s)
	if err != nil || state != namespaceActive {
		return state == namespaceGone, err
	}
	namespace := k.namespace(s.Identity)
	for _, path := range []string{
		"/apis/" + kubeapi.GatewayAPIVersion + "/namespaces/" + namespace + "/httproutes/" + previewAppName,
		"/api/v1/namespaces/" + namespace + "/services/" + previewAppName,
		"/apis/networking.k8s.io/v1/namespaces/" + namespace + "/networkpolicies/" + previewPolicyName,
	} {
		removed, removeErr := k.removeNamespaced(ctx, path, s, sandbox.ResourceRef{})
		if removeErr != nil || !removed {
			return false, removeErr
		}
	}
	return true, nil
}
