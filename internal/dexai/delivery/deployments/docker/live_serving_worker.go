// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package docker

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/dockerapi"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
)

// probeWorker requires the generation's Worker to accept an HTTP/2 connection
// on the port its container publishes, so the Live Worker alias moves only to
// a Worker that answers.
func (l *LiveServing) probeWorker(ctx context.Context, container *dockerapi.Container) error {
	address, err := container.PublishedAddress(infra.LiveWorkerPort)
	if err == nil {
		err = infradocker.ProbeWorker(ctx, address)
	}
	if err != nil {
		return fmt.Errorf("%w: the Worker of %s does not accept connections yet: %w", infra.ErrPending, container.Name, err)
	}
	return nil
}

// routeWorker moves the project's Live Worker alias to the application
// container of the generation of s and returns the endpoint. The engine
// changes a container's aliases only when it attaches the container again, so
// the generation is attached again under its own and the Worker alias first,
// and then every other generation that holds the alias under its own alone; a
// round cut off between them is repeated by the next. Only this generation or
// the Live generation it replaces may hold the alias; another belongs to a
// concurrent activation and is not taken over.
func (l *LiveServing) routeWorker(ctx context.Context, s deployments.Snapshot) (infra.ResourceRef, error) {
	network := s.Resources.Storage.Boundary.UID
	container, err := l.application(ctx, s)
	if err != nil {
		return infra.ResourceRef{}, err
	}
	containers, err := l.API().ListContainers(ctx, dockerapi.Labels(l.OwnerLabels(s.ProjectID, infra.ComponentApplication)))
	if err != nil {
		return infra.ResourceRef{}, err
	}
	var holders []*dockerapi.Container
	for _, summary := range containers {
		if summary.ID == container.ID {
			continue
		}
		other, inspectErr := l.API().InspectContainer(ctx, summary.ID)
		if inspectErr != nil {
			return infra.ResourceRef{}, inspectErr
		}
		if other == nil || !other.Attached(network, infra.LiveWorkerEndpoint) {
			continue
		}
		if generation := other.Config.Labels[deploymentnames.GenerationLabel]; generation != strconv.FormatUint(s.ExpectedLiveRevision, 10) {
			return infra.ResourceRef{}, fmt.Errorf("%w: the Live Worker alias is held by generation %s, not the replaced %d", infra.ErrOwnershipConflict, generation, s.ExpectedLiveRevision)
		}
		holders = append(holders, other)
	}
	if err = l.attach(ctx, network, container, infra.WorkloadName(s.DeploymentID, s.Lease.Generation), infra.LiveWorkerEndpoint); err != nil {
		return infra.ResourceRef{}, err
	}
	for _, holder := range holders {
		labels := holder.Config.Labels
		generation, parseErr := strconv.ParseUint(labels[deploymentnames.GenerationLabel], 10, 64)
		if parseErr != nil {
			return infra.ResourceRef{}, fmt.Errorf("the Live Worker alias holder %s has no generation: %w", holder.Name, parseErr)
		}
		if err = infradocker.VerifyLabels(labels, l.GenerationLabels(s.ProjectID, labels[deploymentnames.WorkloadIDLabel], generation)); err != nil {
			return infra.ResourceRef{}, err
		}
		if err = l.attach(ctx, network, holder, infra.WorkloadName(labels[deploymentnames.WorkloadIDLabel], generation)); err != nil {
			return infra.ResourceRef{}, err
		}
	}
	return infra.ResourceRef{Boundary: l.ProjectNetwork(s.ProjectID), Name: infra.LiveWorkerEndpoint, UID: container.ID, Generation: s.Lease.Generation}, nil
}

// attach attaches container to the network with ID network under exactly
// aliases, attaching it again when it holds others.
func (l *LiveServing) attach(ctx context.Context, network string, container *dockerapi.Container, aliases ...string) error {
	exact := container.Attached(network, infra.LiveWorkerEndpoint) == slices.Contains(aliases, infra.LiveWorkerEndpoint)
	for _, alias := range aliases {
		exact = exact && container.Attached(network, alias)
	}
	if exact {
		return nil
	}
	if container.Joined(network) {
		if err := l.API().DisconnectNetwork(ctx, network, container.ID); err != nil {
			return fmt.Errorf("detach %s from the project network: %w", container.Name, err)
		}
	}
	if err := l.API().ConnectNetwork(ctx, network, container.ID, aliases...); err != nil {
		return fmt.Errorf("attach %s to the project network: %w", container.Name, err)
	}
	return nil
}
