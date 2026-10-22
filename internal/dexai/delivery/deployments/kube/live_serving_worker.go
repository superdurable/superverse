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

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// probeWorker relies on the Pod's registration readiness probe.
func (l *LiveServing) probeWorker(ctx context.Context, s deployments.Snapshot) error {
	_, err := l.workerIdentity(ctx, s)
	return err
}

func (l *LiveServing) workerIdentity(ctx context.Context, s deployments.Snapshot) (infra.ResourceRef, error) {
	name := infra.WorkloadName(s.DeploymentID, s.Lease.Generation) + "-worker"
	worker, ready, err := l.DeploymentReady(ctx, s.ProjectID, name, infrakube.ComponentApplication, s.Resources.Worker.UID)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	if worker == nil || !ready {
		return infra.ResourceRef{}, fmt.Errorf("%w: Worker Deployment %s is not registered", infra.ErrPending, name)
	}
	if !ownedByGeneration(worker, s) {
		return infra.ResourceRef{}, infra.ErrOwnershipConflict
	}
	containers := kubeapi.Items(kubeapi.Child(kubeapi.Child(kubeapi.Child(worker, "spec"), "template"), "spec"), "containers")
	if len(containers) != 1 || kubeapi.String(containers[0].(map[string]any), "image") != s.Inputs.ImageReference {
		return infra.ResourceRef{}, fmt.Errorf("Worker Deployment %s has another image", name)
	}
	ref := infrakube.ResourceReference(worker)
	ref.Generation = s.Lease.Generation
	return ref, nil
}

// StopWorker uses foreground deletion so absence confirms every owned Pod exited.
func (l *LiveServing) StopWorker(ctx context.Context, s deployments.Snapshot) error {
	name := infra.WorkloadName(s.DeploymentID, s.Lease.Generation) + "-worker"
	path := "/apis/apps/v1/namespaces/" + l.ProjectNamespace(s.ProjectID) + "/deployments"
	worker, err := l.GetOwned(ctx, path+"/"+name, s.ProjectID, infrakube.ComponentApplication)
	if err != nil || worker == nil {
		return err
	}
	ref := infrakube.ResourceReference(worker)
	if !ownedByGeneration(worker, s) || (s.Resources.Worker.UID != "" && s.Resources.Worker.UID != ref.UID) {
		return fmt.Errorf("%w: Worker Deployment identity changed", infra.ErrOwnershipConflict)
	}
	if !kubeapi.Deleting(worker) {
		if err := l.Remove(ctx, path, ref, s.ProjectID, infrakube.ComponentApplication); err != nil {
			return err
		}
	}
	return fmt.Errorf("%w: Worker Deployment %s is terminating", infra.ErrPending, name)
}
