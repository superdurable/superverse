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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/platform/kubeapi"
	"github.com/superdurable/superversev2/internal/projects/infra"
	infrakube "github.com/superdurable/superversev2/internal/projects/infra/kube"
)

// Activate moves the project's Live Worker endpoint and then its Live route to
// the ready generation, makes its
// published Flow Definitions the ones project Dex serves, and succeeds once
// the public route answers the health path. Until then it reports
// infra.ErrPending with the reason.
func (l *LiveServing) Activate(ctx context.Context, s deployments.Snapshot) (deployments.Activation, error) {
	if _, err := l.VerifyReadiness(ctx, s); err != nil {
		return deployments.Activation{}, err
	}
	publication, err := deployments.ReadPublication(ctx, l.Infrastructure, s)
	if err != nil {
		return deployments.Activation{}, err
	}
	objects, err := l.S3(ctx, s.ProjectID, s.Resources.Storage.Boundary.UID)
	if err != nil {
		return deployments.Activation{}, err
	}
	// The Worker endpoint moves first, so the generation the route serves
	// already runs the open Flows.
	worker, err := l.routeWorker(ctx, s)
	if err != nil {
		return deployments.Activation{}, err
	}
	if err = l.routeLive(ctx, s); err != nil {
		return deployments.Activation{}, err
	}
	// A delayed activation never overwrites the manifest of a newer route.
	manifest, err := objects.PutGuarded(ctx, appmanifest.ActiveFlowDefinitionManifestKey, comm.Must(json.Marshal(publication.Manifest)), false, func() error {
		route, readErr := l.readLiveRoute(ctx, s.ProjectID)
		if readErr != nil {
			return readErr
		}
		if route == nil || !ownedByGeneration(route, s) {
			return errors.New("the Live route moved to another generation before FDG activation")
		}
		return nil
	})
	if err != nil {
		return deployments.Activation{}, fmt.Errorf("activate the Flow Definitions of %s: %w", s.BuildRevisionID, err)
	}
	publicURL := l.Routes.URL(infra.ScopeLive, s.ProjectID)
	// The Backend names where the route is probed, such as the Local Kind
	// gateway; the request still asks for the public host.
	target, host := strings.TrimRight(l.Backend.ProbeBase(l.Routes, publicURL), "/")+s.Inputs.HealthPath, comm.Must(url.Parse(publicURL)).Host
	if err = probeHealth(ctx, http.DefaultClient, target, host); err != nil {
		return deployments.Activation{}, fmt.Errorf("%w: health probe of the public Live route: %w", infra.ErrPending, err)
	}
	return deployments.Activation{URL: publicURL, Manifest: manifest, Worker: worker}, nil
}

// routeLive points the project's Live route at the generation's Service. A
// route of any generation other than this one or the Live generation it
// replaces belongs to a concurrent activation and is not taken over. An update
// that lost to a concurrent write is infra.ErrPending: the next call reads the
// route again.
func (l *LiveServing) routeLive(ctx context.Context, s deployments.Snapshot) error {
	metadata := l.generationMetadata(s, "live", infrakube.ComponentRoute)
	route := kubeapi.HTTPRoute(metadata, l.RouteGateway(), l.Routes.Host(infra.ScopeLive, s.ProjectID), infra.WorkloadName(s.DeploymentID, s.Lease.Generation), 8080)
	current, err := l.readLiveRoute(ctx, s.ProjectID)
	if err != nil {
		return err
	}
	if current == nil {
		_, err = l.Ensure(ctx, route, s.ProjectID, infrakube.ComponentRoute)
		return err
	}
	generation := kubeapi.String(kubeapi.Labels(current), kubeapi.GenerationLabel)
	if generation != "" && generation != strconv.FormatUint(s.ExpectedLiveRevision, 10) && generation != strconv.FormatUint(s.Lease.Generation, 10) {
		return fmt.Errorf("the Live route serves generation %s, neither the replaced %d nor this %d", generation, s.ExpectedLiveRevision, s.Lease.Generation)
	}
	metadata["resourceVersion"] = kubeapi.Child(current, "metadata")["resourceVersion"]
	metadata["uid"] = kubeapi.UID(current)
	_, code, err := l.API.Call(ctx, http.MethodPut, l.liveRoutes(s.ProjectID)+"/live", route)
	if code == http.StatusConflict {
		return fmt.Errorf("%w: the Live route changed during its update: %w", infra.ErrPending, err)
	}
	return err
}

// liveRoutes is the HTTPRoute collection of the project's Live namespace; the
// Live route is its HTTPRoute named "live", whose parent is the platform's
// project Gateway.
func (l *LiveServing) liveRoutes(project string) string {
	return "/apis/" + kubeapi.GatewayAPIVersion + "/namespaces/" + l.ProjectNamespace(project) + "/httproutes"
}

// readLiveRoute returns the project's owned Live route, or nil when there is
// none.
func (l *LiveServing) readLiveRoute(ctx context.Context, project string) (kubeapi.Object, error) {
	route, code, err := l.API.Call(ctx, http.MethodGet, l.liveRoutes(project)+"/live", nil)
	if err != nil || code == http.StatusNotFound {
		return nil, err
	}
	if err = l.VerifyOwner(route, project, infrakube.ComponentRoute); err != nil {
		return nil, err
	}
	return route, nil
}
