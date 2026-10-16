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
	"fmt"
	"net/http"

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// namespaceState is what cleanup finds of an instance's namespace.
type namespaceState int

const (
	namespaceGone namespaceState = iota
	namespaceTerminating
	namespaceActive
)

// readNamespace checks the instance's canonical namespace before any
// namespaced read. A cleanup instance may hold no namespace reference because
// provisioning never started; absence is established through the namespace
// API, whose authorization survives the deletion of the namespaced RoleBinding.
// A name, owner or UID other than the recorded one is an ownership conflict.
func (k *InstanceProvisioner) readNamespace(ctx context.Context, s sandbox.Instance) (namespaceState, error) {
	ref := s.Resources.Boundary
	if ref.Name != "" && ref.Name != k.namespace(s.Identity) {
		return namespaceGone, fmt.Errorf("%w: the recorded coding namespace %s is not the Project's %s", infra.ErrOwnershipConflict, ref.Name, k.namespace(s.Identity))
	}
	o, code, err := k.API.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+k.namespace(s.Identity), nil)
	if err != nil || code == http.StatusNotFound {
		return namespaceGone, err
	}
	if err = k.verify(o, s.Identity); err != nil {
		return namespaceGone, err
	}
	if ref.UID != "" && resource(o).UID != ref.UID {
		return namespaceGone, fmt.Errorf("%w: coding namespace %s UID changed from %s to %s", infra.ErrOwnershipConflict, ref.Name, ref.UID, resource(o).UID)
	}
	if kubeapi.Deleting(o) {
		return namespaceTerminating, nil
	}
	return namespaceActive, nil
}

// remove deletes the object at path, only if it still is ref, and reports
// whether it is gone.
func (k *InstanceProvisioner) remove(ctx context.Context, path string, s sandbox.Instance, ref sandbox.ResourceRef) (bool, error) {
	o, code, err := k.API.Call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	if code == http.StatusNotFound {
		return true, nil
	}
	if err = k.verify(o, s.Identity); err != nil {
		return false, err
	}
	actual := resource(o)
	if ref.UID != "" && actual.UID != ref.UID {
		return false, fmt.Errorf("%w: refusing to delete %s, whose UID changed from %s to %s", infra.ErrOwnershipConflict, path, ref.UID, actual.UID)
	}
	if !kubeapi.Deleting(o) {
		if _, _, err = k.API.Call(ctx, http.MethodDelete, path, kubeapi.DeleteOptions(actual.UID)); err != nil {
			return false, err
		}
	}
	_, code, err = k.API.Call(ctx, http.MethodGet, path, nil)
	return code == http.StatusNotFound, err
}

// removeNamespaced removes one object of the instance namespace. An absent
// namespace means the object is gone and a terminating one is still pending;
// a namespaced 403 is rechecked against the namespace before failing, because
// a deleting namespace loses its RoleBinding first.
func (k *InstanceProvisioner) removeNamespaced(ctx context.Context, path string, s sandbox.Instance, ref sandbox.ResourceRef) (bool, error) {
	state, err := k.readNamespace(ctx, s)
	if err != nil || state != namespaceActive {
		return state == namespaceGone, err
	}
	done, err := k.remove(ctx, path, s, ref)
	if !kubeapi.IsForbiddenIn(err, k.namespace(s.Identity)) {
		return done, err
	}
	state, readErr := k.readNamespace(ctx, s)
	if readErr != nil {
		return false, readErr
	}
	if state == namespaceActive {
		return false, err
	}
	return state == namespaceGone, nil
}

// DeletePod deletes the recorded Sandbox and reports whether it and every Pod
// it controlled are gone. A Pod outlives its Sandbox object while it
// terminates, and only its absence frees the workspace volume for the next
// instance.
func (k *InstanceProvisioner) DeletePod(ctx context.Context, s sandbox.Instance) (bool, error) {
	if s.Resources.Instance.UID == "" {
		return true, nil
	}
	namespace := k.namespace(s.Identity)
	deleted, err := k.removeNamespaced(ctx, "/apis/agents.x-k8s.io/v1beta1/namespaces/"+namespace+"/sandboxes/"+s.Resources.Instance.Name, s, s.Resources.Instance)
	if err != nil || !deleted {
		return false, err
	}
	state, err := k.readNamespace(ctx, s)
	if err != nil || state != namespaceActive {
		return state == namespaceGone, err
	}
	pods, _, err := k.API.Call(ctx, http.MethodGet, "/api/v1/namespaces/"+namespace+"/pods", nil)
	if err != nil {
		return false, err
	}
	for _, item := range kubeapi.Items(pods, "items") {
		if pod, _ := item.(map[string]any); kubeapi.ControlledBy(pod, "Sandbox", s.Resources.Instance.UID) {
			return false, nil
		}
	}
	return true, nil
}

// DeleteVolume deletes the recorded workspace volume.
func (k *InstanceProvisioner) DeleteVolume(ctx context.Context, s sandbox.Instance) (bool, error) {
	if s.Resources.Volume.UID == "" {
		return true, nil
	}
	return k.removeNamespaced(ctx, "/api/v1/namespaces/"+k.namespace(s.Identity)+"/persistentvolumeclaims/"+s.Resources.Volume.Name, s, s.Resources.Volume)
}

// DeleteNamespace deletes the recorded namespace.
func (k *InstanceProvisioner) DeleteNamespace(ctx context.Context, s sandbox.Instance) (bool, error) {
	if s.Resources.Boundary.UID == "" {
		return true, nil
	}
	return k.remove(ctx, "/api/v1/namespaces/"+k.namespace(s.Identity), s, s.Resources.Boundary)
}
