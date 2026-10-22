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
	"fmt"
	"net/http"

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// CleanupDeployment removes a generation that is not Project.CurrentLive,
// which its Deployment Flow has proved. A partially applied candidate can
// still own the Live route although its activation commit failed, so the
// route goes with it.
func (l *LiveServing) CleanupDeployment(ctx context.Context, s deployments.Snapshot) error {
	return l.removeGeneration(ctx, s, false)
}

// RemoveServing removes the Project's current Live generation, its route and
// its Live Worker endpoint for Live removal or Project deletion.
func (l *LiveServing) RemoveServing(ctx context.Context, p projects.Project) error {
	s := deployments.Snapshot{ProjectID: p.ProjectID, DeploymentID: p.CurrentLive.DeploymentID, Lease: projects.DeploymentLease{Generation: p.CurrentLive.Revision}, Resources: deployments.DeploymentResources{Storage: p.Storage}}
	return l.removeGeneration(ctx, s, true)
}

// removeGeneration removes the generation of s and the Live route it owns. A
// generation that is not live leaves a route another generation owns. The
// live generation must own the route, and its removal is pending until the
// route is gone.
func (l *LiveServing) removeGeneration(ctx context.Context, s deployments.Snapshot, live bool) (err error) {
	defer func() { err = l.cleanupError(ctx, s, err) }()
	namespace, err := l.CleanupNamespace(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID)
	if err != nil || namespace == nil {
		return err
	}
	s.Resources.Storage.Boundary = infrakube.ResourceReference(namespace)
	route, err := l.readLiveRoute(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	if route != nil {
		switch {
		case ownedByGeneration(route, s):
			if err = l.Remove(ctx, l.liveRoutes(s.ProjectID), infrakube.ResourceReference(route), s.ProjectID, infrakube.ComponentRoute); err != nil {
				return err
			}
		case live:
			return fmt.Errorf("%w: Live route generation changed", infra.ErrOwnershipConflict)
		}
	}

	if err = l.cleanupGeneration(ctx, s); err != nil || !live {
		return err
	}
	if route, err = l.readLiveRoute(ctx, s.ProjectID); err != nil || route == nil {
		return err
	}
	return fmt.Errorf("%w: the deleted Live route %s is still present", projects.ErrCleanupPending, kubeapi.UID(route))
}

// cleanupGeneration removes one generation's namespaced objects and confirms
// that its Deployment is gone, not merely that the delete was accepted.
func (l *LiveServing) cleanupGeneration(ctx context.Context, s deployments.Snapshot) error {
	if err := l.StopWorker(ctx, s); err != nil {
		return err
	}
	ns := l.ProjectNamespace(s.ProjectID)
	name := infra.WorkloadName(s.DeploymentID, s.Lease.Generation)
	deployments := "/apis/apps/v1/namespaces/" + ns + "/deployments"
	items := []struct{ path, name string }{{deployments, name}, {"/api/v1/namespaces/" + ns + "/services", name}, {"/api/v1/namespaces/" + ns + "/secrets", name + "-environment"}, {"/apis/networking.k8s.io/v1/namespaces/" + ns + "/networkpolicies", name}}
	if !l.Backend.SharedApplicationServiceAccount() {
		items = append(items, struct{ path, name string }{"/api/v1/namespaces/" + ns + "/serviceaccounts", name})
	}
	for _, item := range items {
		o, code, err := l.API.Call(ctx, http.MethodGet, item.path+"/"+item.name, nil)
		if err != nil {
			return err
		}
		if code == http.StatusNotFound {
			continue
		}
		if err = l.VerifyOwner(o, s.ProjectID, infrakube.ComponentApplication); err != nil {
			return err
		}
		if !ownedByGeneration(o, s) {
			return fmt.Errorf("%w: deployment generation changed", infra.ErrOwnershipConflict)
		}
		if err = l.Remove(ctx, item.path, infrakube.ResourceReference(o), s.ProjectID, infrakube.ComponentApplication); err != nil {
			return err
		}
	}
	o, code, err := l.API.Call(ctx, http.MethodGet, deployments+"/"+name, nil)
	if err != nil {
		return err
	}
	if code != http.StatusNotFound && o != nil {
		return fmt.Errorf("%w: the deleted application Deployment %s is still terminating", projects.ErrCleanupPending, name)
	}
	return nil
}

// cleanupError reconciles a denied read inside the namespace against the
// namespace itself and reports a still reconciling scope as pending cleanup,
// with its reason.
func (l *LiveServing) cleanupError(ctx context.Context, s deployments.Snapshot, err error) error {
	if err == nil {
		return nil
	}
	err = l.ReconcileCleanupError(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID, err)
	if errors.Is(err, infra.ErrPending) {
		return fmt.Errorf("%w: %w", projects.ErrCleanupPending, err)
	}
	return err
}
