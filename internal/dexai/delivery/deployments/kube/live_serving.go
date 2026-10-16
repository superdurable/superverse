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
	"strconv"

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
)

// LiveServing is the Kubernetes implementation of deployments.Serving. It runs
// each deployment generation as its own application Deployment and Service in
// the project's Live scope, activates the Flow Definitions the generation
// published to the project's storage and moves the project's public Live route
// to the generation it activates. It builds on the Kubernetes machinery of the
// Infrastructure, whose Backend decides what differs between environments.
type LiveServing struct {
	*infrakube.Infrastructure
	Routes infra.PublicRoutes
}

var _ deployments.Serving = (*LiveServing)(nil)

// generationLabels bind a resource to the deployment generation that owns it.
func (l *LiveServing) generationLabels(s deployments.Snapshot, component infrakube.Component) kubeapi.Object {
	labels := l.OwnerLabels(s.ProjectID, component)
	labels[kubeapi.WorkloadIDLabel] = s.DeploymentID
	labels[kubeapi.GenerationLabel] = strconv.FormatUint(s.Lease.Generation, 10)
	return labels
}

// generationMetadata names a resource of the deployment generation and labels
// it with the generation that owns it.
func (l *LiveServing) generationMetadata(s deployments.Snapshot, name string, component infrakube.Component) kubeapi.Object {
	metadata := l.Metadata(s.ProjectID, name, component)
	metadata["labels"] = l.generationLabels(s, component)
	return metadata
}

// ownedByGeneration reports whether a resource carries the labels of exactly
// the Snapshot's deployment generation.
func ownedByGeneration(o kubeapi.Object, s deployments.Snapshot) bool {
	labels := kubeapi.Labels(o)
	return kubeapi.String(labels, kubeapi.WorkloadIDLabel) == s.DeploymentID && kubeapi.String(labels, kubeapi.GenerationLabel) == strconv.FormatUint(s.Lease.Generation, 10)
}

// EnsureGenerationService reconciles the generation's application Service,
// which exists before its Pods, so readiness and the Live route bind a stable
// identity.
func (l *LiveServing) EnsureGenerationService(ctx context.Context, s deployments.Snapshot) (infra.ResourceRef, error) {
	name := infra.WorkloadName(s.DeploymentID, s.Lease.Generation)
	service := l.Service(s.ProjectID, name, infrakube.ComponentApplication, l.generationLabels(s, infrakube.ComponentApplication),
		kubeapi.ServicePort("http", 8080, s.Inputs.ApplicationPort), kubeapi.ServicePort("worker", infra.LiveWorkerPort, infra.LiveWorkerPort))
	service["metadata"] = l.generationMetadata(s, name, infrakube.ComponentApplication)
	found, err := l.Ensure(ctx, service, s.ProjectID, infrakube.ComponentApplication)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	generation := infrakube.ResourceReference(found)
	generation.Generation = s.Lease.Generation
	return generation, nil
}
