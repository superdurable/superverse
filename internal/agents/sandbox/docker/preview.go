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
	"strconv"

	"github.com/superdurable/superversev2/internal/agents/sandbox"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/projects/infra/docker"
)

// EnsurePreviewEnvironment starts the instance's Preview engine, or finds it
// running, and once the supervisor answers ready verifies that the engine's
// Dex answers through the instance's container: infra.ErrPending while it
// does not yet, infra.ErrOwnershipConflict when a recorded object was
// replaced.
func (p *InstanceProvider) EnsurePreviewEnvironment(ctx context.Context, s sandbox.Instance) (workspace.PreviewAnswer, error) {
	answer, err := p.Workspace.EnsurePreviewEnvironment(ctx, s.Binding(), "environment/"+s.WorkspaceRequestID+"/ensure")
	if err != nil || answer.State != workspace.PreviewStateReady {
		return answer, err
	}
	return answer, p.Connections.ProbeSandboxDex(ctx, s.DexTarget())
}

// previewRoute is the Project's Preview route as this instance serves it.
func (p *InstanceProvider) previewRoute(s sandbox.Instance) infradocker.Route {
	return infradocker.Route{
		Host:       p.PublicRoutes().Host(infra.ScopePreview, s.Identity.ProjectID),
		Owner:      infradocker.RouteOwner{Scope: infra.ScopePreview.String(), ProjectID: s.Identity.ProjectID, WorkspaceRequestID: s.WorkspaceRequestID},
		Generation: s.Generation, Container: s.Resources.ServiceUID, Port: workspace.PreviewApplicationPort,
	}
}

// ownsPreviewRoute reports whether a route is the Project's Preview route,
// whichever of the Project's instances published it: the Project's one
// PreviewFlow serves every instance of its Session.
func ownsPreviewRoute(current infradocker.Route, id sandbox.Identity) bool {
	return current.Owner.Scope == infra.ScopePreview.String() && current.Owner.ProjectID == id.ProjectID
}

// ReleasePreviewRoute withdraws the Project's Preview route, whichever of its
// instances published it: a stop calls it, and so does the deleting Project's
// release, which names no instance. A route of another holder stays.
func (p *InstanceProvider) ReleasePreviewRoute(ctx context.Context, s sandbox.Instance) (bool, error) {
	host := p.PublicRoutes().Host(infra.ScopePreview, s.Identity.ProjectID)
	_, err := p.Routes().Withdraw(ctx, host, func(current infradocker.Route) bool { return ownsPreviewRoute(current, s.Identity) })
	if err != nil {
		return false, fmt.Errorf("withdraw the Project's Preview route: %w", err)
	}
	return true, nil
}

// ExposePreview publishes the Project's Preview route to the instance's
// application and probes it through the real gateway. It writes the route as
// the platform wants it now, replacing one that a stop left out, that an
// earlier instance of the Project published or that differs; another holder
// of the host is infra.ErrOwnershipConflict. It returns the public URL once the gateway
// serves the route and the application answers, and until then an empty URL
// with what the gateway showed.
func (p *InstanceProvider) ExposePreview(ctx context.Context, s sandbox.Instance) (publicURL, unanswered string, err error) {
	route := p.previewRoute(s)
	admit := func(current infradocker.Route) bool { return ownsPreviewRoute(current, s.Identity) }
	if err = p.Routes().Publish(ctx, route, admit); err != nil {
		return "", "", err
	}
	loaded, observed, err := p.Routes().Loaded(ctx, route)
	if err != nil || !loaded {
		return "", observed, err
	}
	status, unanswered := p.Routes().Probe(ctx, route.Host, "/")
	if unanswered != "" {
		// Not reachable through the gateway yet: the Step probes again.
		return "", "the probe failed: " + unanswered, nil
	}
	if status < 200 || status >= 400 {
		return "", "the gateway answered " + strconv.Itoa(status), nil
	}
	return p.PublicRoutes().URL(infra.ScopePreview, s.Identity.ProjectID), "", nil
}

// StopPreview withdraws the Project's route and then stops the instance's
// application, so that nothing listening on the application's port stays
// reachable through the gateway while the Preview is stopped. A network that
// is gone leaves no application to stop.
func (p *InstanceProvider) StopPreview(ctx context.Context, s sandbox.Instance, commandID string) (bool, error) {
	if _, err := p.ReleasePreviewRoute(ctx, s); err != nil {
		return false, err
	}
	network, err := p.CleanupNetwork(ctx, p.network(s.Identity), s.Resources.Boundary.UID, p.owner(s.Identity))
	if err != nil || network == nil {
		return err == nil, err
	}
	// A Preview process is only ever started through the instance's container.
	// An instance that failed before its container ran has no supervisor to
	// ask, so there is nothing to stop.
	if s.Resources.Instance.UID == "" || s.Resources.ServiceName == "" {
		return true, nil
	}
	answer, err := p.Workspace.StopPreview(ctx, s.Binding(), "stop/"+s.WorkspaceRequestID+"/"+commandID)
	return answer.State == workspace.PreviewStateStopped, err
}
