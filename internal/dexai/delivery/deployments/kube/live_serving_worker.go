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
	"strconv"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/dexai/projects/infra/kube"
	"google.golang.org/grpc/connectivity"
)

// workerProbeTimeout bounds the connection to a generation's Worker that
// readiness requires.
const workerProbeTimeout = 2 * time.Second

// probeWorker requires the generation's Worker to accept an HTTP/2
// connection on its own Service, so the Live Worker endpoint moves only to a
// Worker that answers.
func (l *LiveServing) probeWorker(ctx context.Context, s deployments.Snapshot) error {
	address := kubeapi.ServiceAddress(l.ProjectNamespace(s.ProjectID), infra.WorkloadName(s.DeploymentID, s.Lease.Generation), infra.LiveWorkerPort)
	connection, closeConnection, err := l.DialPrivate(ctx, address)
	if err != nil {
		return err
	}
	defer func() { _ = closeConnection() }()
	probe, cancel := context.WithTimeout(ctx, workerProbeTimeout)
	defer cancel()
	connection.Connect()
	for state := connection.GetState(); state != connectivity.Ready; state = connection.GetState() {
		if state == connectivity.Shutdown || !connection.WaitForStateChange(probe, state) {
			return fmt.Errorf("%w: the Worker of %s does not accept connections yet", infra.ErrPending, infra.WorkloadName(s.DeploymentID, s.Lease.Generation))
		}
	}
	return nil
}

// workerEndpointPath is the path of the project's Live Worker endpoint.
func (l *LiveServing) workerEndpointPath(project string) string {
	return "/api/v1/namespaces/" + l.ProjectNamespace(project) + "/services/" + infra.LiveWorkerEndpoint
}

// workerEndpoint is the project's Live Worker endpoint selecting the
// generation of s: a headless Service, which the project Dex resolves itself,
// so it dispatches to the selected generation's Pod alone.
func (l *LiveServing) workerEndpoint(s deployments.Snapshot) kubeapi.Object {
	service := kubeapi.Service(l.generationMetadata(s, infra.LiveWorkerEndpoint, infrakube.ComponentRoute), l.generationLabels(s, infrakube.ComponentApplication),
		kubeapi.ServicePort("worker", infra.LiveWorkerPort, infra.LiveWorkerPort))
	kubeapi.Child(service, "spec")["clusterIP"] = "None"
	return service
}

// routeWorker moves the project's Live Worker endpoint to the generation of s
// and returns the endpoint. An endpoint of any generation other than this one
// or the Live generation it replaces belongs to a concurrent activation and is
// not taken over. An update that lost to a concurrent write is
// infra.ErrPending: the next call reads the endpoint again.
func (l *LiveServing) routeWorker(ctx context.Context, s deployments.Snapshot) (infra.ResourceRef, error) {
	want := l.workerEndpoint(s)
	current, err := l.GetOwned(ctx, l.workerEndpointPath(s.ProjectID), s.ProjectID, infrakube.ComponentRoute)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	if current == nil {
		created, createErr := l.API.GetOrCreate(ctx, want)
		if createErr != nil {
			return infra.ResourceRef{}, createErr
		}
		if err = l.VerifyOwner(created, s.ProjectID, infrakube.ComponentRoute); err != nil {
			return infra.ResourceRef{}, err
		}
		current = created
	}
	if ownedByGeneration(current, s) && workerSelects(current, s) {
		return infrakube.ResourceReference(current), nil
	}
	generation := kubeapi.String(kubeapi.Labels(current), kubeapi.GenerationLabel)
	if generation != "" && generation != strconv.FormatUint(s.ExpectedLiveRevision, 10) && generation != strconv.FormatUint(s.Lease.Generation, 10) {
		return infra.ResourceRef{}, fmt.Errorf("the Live Worker endpoint selects generation %s, neither the replaced %d nor this %d", generation, s.ExpectedLiveRevision, s.Lease.Generation)
	}
	metadata := kubeapi.Child(want, "metadata")
	metadata["resourceVersion"] = kubeapi.Child(current, "metadata")["resourceVersion"]
	metadata["uid"] = kubeapi.UID(current)
	updated, code, err := l.API.Call(ctx, http.MethodPut, l.workerEndpointPath(s.ProjectID), want)
	if code == http.StatusConflict {
		return infra.ResourceRef{}, fmt.Errorf("%w: the Live Worker endpoint changed during its update: %w", infra.ErrPending, err)
	}
	if err != nil {
		return infra.ResourceRef{}, err
	}
	return infrakube.ResourceReference(updated), nil
}

// workerSelects reports whether the endpoint selects exactly the Pods of the
// generation of s.
func workerSelects(endpoint kubeapi.Object, s deployments.Snapshot) bool {
	selector := kubeapi.Child(kubeapi.Child(endpoint, "spec"), "selector")
	return kubeapi.String(selector, kubeapi.WorkloadIDLabel) == s.DeploymentID && kubeapi.String(selector, kubeapi.GenerationLabel) == strconv.FormatUint(s.Lease.Generation, 10)
}

// removeWorker removes the project's Live Worker endpoint when it selects the
// generation of s, which the generation's removal as Live ends. An endpoint of
// another generation stays.
func (l *LiveServing) removeWorker(ctx context.Context, s deployments.Snapshot) error {
	current, err := l.GetOwned(ctx, l.workerEndpointPath(s.ProjectID), s.ProjectID, infrakube.ComponentRoute)
	if err != nil || current == nil || !ownedByGeneration(current, s) {
		return err
	}
	return l.Remove(ctx, "/api/v1/namespaces/"+l.ProjectNamespace(s.ProjectID)+"/services", infrakube.ResourceReference(current), s.ProjectID, infrakube.ComponentRoute)
}
