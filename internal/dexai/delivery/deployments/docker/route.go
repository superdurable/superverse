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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
)

// liveRoute is the project's Live route as the generation of s holds it.
func (l *LiveServing) liveRoute(s deployments.Snapshot) infradocker.Route {
	return infradocker.Route{
		Host:       l.PublicRoutes().Host(infra.ScopeLive, s.ProjectID),
		Owner:      infradocker.RouteOwner{Scope: infra.ScopeLive.String(), ProjectID: s.ProjectID, DeploymentID: s.DeploymentID},
		Generation: s.Lease.Generation, Container: s.Providers.Application.UID, Port: s.Inputs.ApplicationPort,
	}
}

// ownedByGeneration reports whether a route is held by exactly the
// deployment generation of s.
func ownedByGeneration(route infradocker.Route, s deployments.Snapshot) bool {
	return route.Owner.Scope == infra.ScopeLive.String() && route.Owner.ProjectID == s.ProjectID && route.Owner.DeploymentID == s.DeploymentID && route.Generation == s.Lease.Generation
}

// Activate writes the guarded active FDG before the HTTP route and observes
// public health. An uncertain activation rechecks exact resource ownership.
func (l *LiveServing) Activate(ctx context.Context, s deployments.Snapshot) (deployments.Activation, error) {
	if _, err := l.VerifyReadiness(ctx, s); err != nil {
		return deployments.Activation{}, err
	}
	publication, err := deployments.ReadPublication(ctx, l, s)
	if err != nil {
		return deployments.Activation{}, err
	}
	objects, err := l.S3(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID)
	if err != nil {
		return deployments.Activation{}, err
	}
	worker, err := l.workerIdentity(ctx, s)
	if err != nil {
		return deployments.Activation{}, err
	}
	route := l.liveRoute(s)
	admit := func(current infradocker.Route) bool {
		return current.Owner.Scope == route.Owner.Scope && (current.Generation == s.ExpectedLiveRevision || current.Generation == s.Lease.Generation)
	}
	// A delayed activation never overwrites the manifest of a newer route.
	manifest, err := objects.PutGuarded(ctx, appmanifest.ActiveFlowDefinitionManifestKey, comm.Must(json.Marshal(publication.Manifest)), false, func() error {
		current, exists, readErr := l.Routes().Read(route.Host)
		if readErr != nil {
			return readErr
		}
		if !exists && s.ExpectedLiveRevision != 0 || exists && !ownedByGeneration(current, s) && !admit(current) {
			return errRouteMoved
		}
		return nil
	})
	if err != nil {
		return deployments.Activation{}, fmt.Errorf("activate the Flow Definitions of %s: %w", s.BuildRevisionID, err)
	}
	if err = l.Routes().Publish(ctx, route, admit); err != nil {
		return deployments.Activation{}, err
	}
	loaded, observed, err := l.Routes().Loaded(ctx, route)
	if err != nil {
		return deployments.Activation{}, err
	}
	if !loaded {
		return deployments.Activation{}, fmt.Errorf("%w: %s", infra.ErrPending, observed)
	}
	status, unanswered := l.Routes().Probe(ctx, route.Host, s.Inputs.HealthPath)
	if unanswered != "" {
		return deployments.Activation{}, fmt.Errorf("%w: health probe of the public Live route: %s", infra.ErrPending, unanswered)
	}
	if status != http.StatusOK {
		return deployments.Activation{}, fmt.Errorf("%w: the public Live route answered %d", infra.ErrPending, status)
	}
	return deployments.Activation{URL: l.PublicRoutes().URL(infra.ScopeLive, s.ProjectID), Manifest: manifest, Worker: worker}, nil
}

// CleanupDeployment removes a generation that is not Project.CurrentLive,
// which its Deployment Flow has proved. A partially applied candidate can
// still hold the Live route although its activation commit failed, so the
// route goes with it.
func (l *LiveServing) CleanupDeployment(ctx context.Context, s deployments.Snapshot) error {
	return l.removeGeneration(ctx, s, false)
}

// RemoveServing removes the Project's current Live generation and its route
// for Live removal or Project deletion.
func (l *LiveServing) RemoveServing(ctx context.Context, p projects.Project) error {
	s := deployments.Snapshot{ProjectID: p.ProjectID, DeploymentID: p.CurrentLive.DeploymentID, Lease: projects.DeploymentLease{Generation: p.CurrentLive.Revision}, Resources: deployments.DeploymentResources{Storage: p.Storage}}
	return l.removeGeneration(ctx, s, true)
}

// generationStopGrace is how long an application container gets to stop.
const generationStopGrace = 30 * time.Second

// removeGeneration removes the generation of s and the Live route it holds. A
// generation that is not live leaves a route another generation holds. The
// live generation must hold the route. Every removal is confirmed, not merely
// accepted, and a scope still converging is projects.ErrCleanupPending.
func (l *LiveServing) removeGeneration(ctx context.Context, s deployments.Snapshot, live bool) (err error) {
	defer func() {
		if errors.Is(err, infra.ErrPending) {
			err = fmt.Errorf("%w: %w", projects.ErrCleanupPending, err)
		}
	}()
	network, err := l.CleanupNetwork(ctx, l.ProjectNetwork(s.ProjectID), s.Resources.Storage.Boundary.UID, l.OwnerLabels(s.ProjectID, infra.ComponentInfrastructure))
	if err != nil || network == nil {
		// An absent project network took the generation with it.
		return err
	}
	remaining, err := l.Routes().Withdraw(ctx, l.PublicRoutes().Host(infra.ScopeLive, s.ProjectID), func(current infradocker.Route) bool { return ownedByGeneration(current, s) })
	if err != nil {
		return err
	}
	if remaining != nil && live {
		return fmt.Errorf("%w: Live route generation changed", infra.ErrOwnershipConflict)
	}
	if err := l.StopWorker(ctx, s); err != nil {
		return err
	}
	name, labels := l.generation(s)
	gone, err := l.RemoveContainer(ctx, name, labels, generationStopGrace)
	if err != nil {
		return err
	}
	if !gone {
		return fmt.Errorf("%w: the application container %s is being removed", infra.ErrPending, name)
	}
	if gone, err = l.RemoveNetwork(ctx, name, "", labels); err != nil {
		return err
	}
	if !gone {
		return fmt.Errorf("%w: the generation network %s is being removed", infra.ErrPending, name)
	}
	return nil
}
