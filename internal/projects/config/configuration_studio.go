// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package config

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost"
	"github.com/superdurable/superversev2/internal/studio/confighost/configitems"
	"github.com/superdurable/superversev2/internal/studio/flowapi"
)

// flowAPIRequestTimeout bounds one Flow API configuration request, which may
// wait on provider calls such as an OAuth token exchange.
const flowAPIRequestTimeout = 2 * time.Minute

// Access states what a configuration request does with the configuration:
// a write writes the Project's storage, so its admission reads ProjectFlow's
// current state (decision 27); a read authorizes with the read model.
type Access struct{ write bool }

var (
	// ReadAccess reads the configuration or its catalog.
	ReadAccess = Access{}
	// WriteAccess writes the configuration, its credentials or its
	// validated versions.
	WriteAccess = Access{write: true}
)

// configurationTarget is the prepared source a request addresses, resolved
// and authorized for the actor: a Release's for Live, or the one the Preview
// prepared for the Session's HEAD. manifest names the source's commit. A
// next-publish target addresses Live's next-publish layer, checked against a
// Release's source or, with previewSource, the Preview's.
type configurationTarget struct {
	projectScope  pb.ProjectScope
	scope         infra.ScopeRef
	manifest      *pb.AppManifestSnapshot
	releaseID     string
	teamSpaceID   string
	project       projects.Project
	nextPublish   bool
	previewSource bool
}

// resolveTarget authorizes the actor for the project and resolves the
// configuration that requested addresses.
func (c *Configuration) resolveTarget(ctx context.Context, actor string, requested *pb.ProjectConfigurationTarget, access Access) (configurationTarget, error) {
	scope := infra.ScopeOf(requested.GetScope())
	if scope.Validate(requested.GetProjectId()) != nil {
		return configurationTarget{}, ErrInvalid
	}
	project, err := c.authorize(ctx, actor, requested.GetProjectId(), access)
	if err != nil {
		return configurationTarget{}, err
	}
	// Live configuration belongs to one Release's commit, and the next-publish
	// layer to Live; with no Release it is checked against the Preview's source.
	nextPublish := requested.GetNextPublish()
	if scope.Kind == infra.ScopeLive && requested.GetReleaseId() == "" && !nextPublish || nextPublish && scope.Kind != infra.ScopeLive {
		return configurationTarget{}, ErrInvalid
	}
	if project.ProjectStatus != projects.StatusReady {
		return configurationTarget{}, ErrNotReady
	}
	// Both scopes keep their configuration in the Project's Live storage; the
	// scope selects only the key prefix.
	target := configurationTarget{
		projectScope: requested.GetScope(), releaseID: requested.GetReleaseId(), teamSpaceID: project.TeamSpaceID, project: project, nextPublish: nextPublish,
		scope: infra.ScopeRef{ProjectID: project.ProjectID, Scope: scope, Storage: project.Storage},
	}
	if nextPublish && target.releaseID == "" {
		target.previewSource = true
		target.manifest, err = c.Preview.ReadPreviewTarget(ctx, project.ProjectID)
		if err != nil {
			return configurationTarget{}, err
		}
		return target, nil
	}
	if scope.Kind == infra.ScopePreview {
		if requested.GetReleaseId() != "" {
			return configurationTarget{}, ErrInvalid
		}
		target.manifest, err = c.Preview.ReadPreviewTarget(ctx, project.ProjectID)
		if err != nil {
			return configurationTarget{}, err
		}
		return target, nil
	}
	source, err := c.preparedSource(ctx, project.ProjectID, requested.GetReleaseId(), access)
	if err != nil {
		return configurationTarget{}, err
	}
	if source.Status != releases.Ready || source.AppManifest == nil {
		return configurationTarget{}, ErrNotReady
	}
	target.manifest = source.AppManifest
	return target, nil
}

// preparedSource is the prepared source of a Release that a request of access
// addresses: ProjectReleaseFlow's for a write, the read model's for a read.
func (c *Configuration) preparedSource(ctx context.Context, projectID, releaseID string, access Access) (releases.PreparedReleaseSource, error) {
	if access.write {
		release, err := c.Releases.ReadSnapshot(ctx, projectID, releaseID)
		return release.PreparedSource, err
	}
	projection, err := c.Releases.ReadProjection(ctx, projectID, releaseID)
	return projection.Release.PreparedSource, err
}

// authorize returns the Project a request of access may use: ProjectFlow's
// current state for a write, the read model's for a read.
func (c *Configuration) authorize(ctx context.Context, actor, projectID string, access Access) (projects.Project, error) {
	if access.write {
		return c.Projects.AuthorizeCurrent(ctx, actor, projectID, projects.CapabilityBuild)
	}
	projection, err := c.Projects.Authorize(ctx, actor, projectID, projects.CapabilityBuild)
	return projection.Project, err
}

// Get returns the configuration that requested addresses. A Preview target
// is READY when its current draft and credentials validate; otherwise it
// names every requirement gap of the gap report, or none when the report
// itself cannot be made, such as for a connector release that cannot be
// resolved. A Live target is Live's next publish as it resolves for its
// source, READY without a gap, with its items: the layer for a next-publish
// target, a Release's frozen copy and whether it is stale for a Release's.
// Reading never freezes a build or writes anything.
func (c *Configuration) Get(ctx context.Context, actor string, requested *pb.ProjectConfigurationTarget) (*pb.ProjectConfigurationSnapshot, []configitems.Item, error) {
	target, err := c.resolveTarget(ctx, actor, requested, ReadAccess)
	if err != nil {
		return nil, nil, err
	}
	if target.scope.Scope.Kind != infra.ScopeLive {
		snapshot, readErr := c.read(ctx, target)
		return snapshot, nil, readErr
	}
	snapshot, report, err := c.liveSnapshot(ctx, actor, target)
	if err != nil {
		return nil, nil, err
	}
	return snapshot, report.Items, nil
}

// read is the snapshot of the target's configuration with its readiness.
func (c *Configuration) read(ctx context.Context, target configurationTarget) (*pb.ProjectConfigurationSnapshot, error) {
	metadata, err := c.ReadMetadata(ctx, target.scope, target.manifest.SourceCommit)
	if err != nil {
		return nil, err
	}
	snapshot := configurationSnapshot(target, metadata)
	_, err = c.validatePrepared(ctx, target.scope, target.manifest, metadata, metadata.ConfigurationRevision, true)
	switch {
	case err == nil:
		snapshot.Status = pb.ConfigurationStatus_CONFIGURATION_STATUS_READY
		return snapshot, nil
	case errors.Is(err, ErrNotReady), errors.Is(err, ErrConflict):
		snapshot.ValidationErrors = []string{"Review the current source configuration and credentials before deployment."}
	default:
		return nil, err
	}
	gaps, err := c.CheckRequirements(ctx, target.scope, target.manifest)
	if err != nil && !errors.Is(err, ErrNotReady) && !errors.Is(err, ErrConflict) {
		return nil, err
	}
	snapshot.RequirementGaps = appmanifest.RequirementGapSnapshots(gaps)
	return snapshot, nil
}

// Validate validates expectedRevision of the configuration that requested
// addresses and records requestID in the validation receipt. A READY
// configuration may be written: an initial draft, or Preview's frozen version.
// A next-publish target validates the next publish as it resolves for its
// source at the layer's expectedRevision, and writes nothing.
func (c *Configuration) Validate(ctx context.Context, actor string, requested *pb.ProjectConfigurationTarget, requestID string, expectedRevision uint64) (*pb.ProjectConfigurationSnapshot, error) {
	target, err := c.resolveTarget(ctx, actor, requested, WriteAccess)
	if err != nil {
		return nil, err
	}
	if target.nextPublish {
		snapshot, report, liveErr := c.liveSnapshot(ctx, actor, target)
		switch {
		case liveErr != nil:
			return nil, liveErr
		case report.LayerRevision != expectedRevision:
			return nil, ErrConflict
		case len(report.Gaps) > 0:
			return nil, fmt.Errorf("%w: Live's next publish lacks %s", ErrNotReady, strings.Join(appmanifest.RequirementGapNames(report.Gaps), ", "))
		}
		snapshot.Validation = &pb.ProjectConfigurationValidation{RequestId: requestID, ConfigurationRevision: report.LayerRevision}
		return snapshot, nil
	}
	metadata, err := c.ReadMetadata(ctx, target.scope, target.manifest.SourceCommit)
	if err != nil {
		return nil, err
	}
	result, err := c.validatePrepared(ctx, target.scope, target.manifest, metadata, expectedRevision, false)
	if err != nil {
		return nil, err
	}
	snapshot := configurationSnapshot(target, result.Metadata)
	snapshot.Status = pb.ConfigurationStatus_CONFIGURATION_STATUS_READY
	snapshot.Validation = &pb.ProjectConfigurationValidation{RequestId: requestID, ConfigurationRevision: result.ConfigurationRevision}
	return snapshot, nil
}

// InvokeForAccount serves one named Flow API configuration operation for the
// actor on the prepared source that the request targets. Neither method nor
// path is a protocol field: the API maps each operation to a fixed route. A
// configuration belongs to a prepared source, never to a deployment or a
// Sandbox instance: the source commit and the revisions the form was read at
// fence it.
func (c *Configuration) InvokeForAccount(ctx context.Context, actor string, flowAPI *pb.FlowApiContext, access Access, method, path string, body []byte) (flowapi.Document, error) {
	requested := flowAPI.GetTarget()
	if requested.GetDeploymentId() != "" || requested.GetWorkspaceRequestId() != "" || requested.GetGeneration() != 0 {
		return flowapi.Document{}, ErrInvalid
	}
	target, err := c.resolveTarget(ctx, actor, &pb.ProjectConfigurationTarget{
		ProjectId: requested.GetProjectId(), Scope: requested.GetScope(), ReleaseId: requested.GetReleaseId(), NextPublish: requested.GetNextPublish(),
	}, access)
	if err != nil {
		return flowapi.Document{}, err
	}
	// A Release's frozen copy is written only by publishing; Live is edited in its next-publish layer.
	if access.write && target.scope.Scope.Kind == infra.ScopeLive && !target.nextPublish {
		return flowapi.Document{}, ErrInvalid
	}
	admission := confighost.ConfigurationAdmission{
		ActorID: actor, TeamSpaceID: target.teamSpaceID, SourceCommit: target.manifest.SourceCommit, DefinitionRevision: flowAPI.GetDefinitionRevision(),
		AppManifestRevision: flowAPI.GetAppManifestRevision(), ConfigurationRevision: flowAPI.GetConfigurationRevision(),
	}
	if target.nextPublish {
		if admission.NextPublish, err = c.nextPublishInputs(ctx, target); err != nil {
			return flowapi.Document{}, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, flowAPIRequestTimeout)
	defer cancel()
	host, release, err := c.acquireHost(ctx, target.scope)
	if err != nil {
		return flowapi.Document{}, err
	}
	defer release()
	return host.Invoke(ctx, admission, method, path, body)
}
