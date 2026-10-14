// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/projects"
	projectconfig "github.com/superdurable/superversev2/internal/projects/config"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
)

// PublishingFlowService coordinates a Release with the configuration owner and
// with its automatic Live deployment, across the Release, Project and
// Deployment Flows: it is the Release Flow's dependency.
type PublishingFlowService struct {
	projects      *projects.ProjectFlowClient
	deployments   *ProjectDeployFlowClient
	configuration *projectconfig.Configuration
	usages        *connections.Usages
	// logReads and logs read the redacted build logs of this replica.
	logReads BuildLogReads
	logs     *buildLogs
}

func NewPublishingFlowService(projectClient *projects.ProjectFlowClient, deploymentClient *ProjectDeployFlowClient, configuration *projectconfig.Configuration,
	usages *connections.Usages, logReads BuildLogReads) *PublishingFlowService {
	return &PublishingFlowService{projects: projectClient, deployments: deploymentClient, configuration: configuration, usages: usages, logReads: logReads, logs: newBuildLogs(logReads.PerSecond)}
}

// ErrDeploymentInProgress refuses a release of unused connections while a
// deployment holds the Project's deployment lease.
var ErrDeploymentInProgress = errors.New("a deployment is in progress")

// ReleaseUnusedConnections keeps only the Live usages of the TeamSpace
// connections the current Live version selects, for a builder of the Project;
// it is refused while a deployment runs. A usage recorded after the database
// time read first stays, so a deployment that starts meanwhile keeps the
// usages it ensured.
func (s *PublishingFlowService) ReleaseUnusedConnections(ctx context.Context, actorAccountID, projectID string) error {
	since, err := s.usages.Now(ctx)
	if err != nil {
		return err
	}
	project, err := s.projects.AuthorizeCurrent(ctx, actorAccountID, projectID, projects.CapabilityBuild)
	if err != nil {
		return err
	}
	if project.ActiveDeployment != nil {
		return ErrDeploymentInProgress
	}
	var keep []string
	if live := project.CurrentLive; live != nil {
		deployment, readErr := s.deployments.readSnapshot(ctx, projectID, live.DeploymentID)
		if err = readErr; err != nil {
			return fmt.Errorf("read the Live deployment %s: %w", live.DeploymentID, err)
		}
		scope := infra.ScopeRef{ProjectID: projectID, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: project.Storage}
		if keep, err = s.configuration.SharedConnectionIDs(ctx, scope, deployment.Inputs.Configuration.Object); err != nil {
			return fmt.Errorf("read the connections Live deployment %s selects: %w", live.DeploymentID, err)
		}
	}
	_, err = s.usages.ReleaseUnusedLiveUsages(ctx, projectID, keep, since)
	return err
}

// RegisterReleaseSource registers the prepared definitions with the
// configuration owner. A version's configuration is built from Live's
// next-publish layer when it is published.
func (s *PublishingFlowService) RegisterReleaseSource(ctx context.Context, project projects.Project, source releases.PreparedReleaseSource, definitions []appmanifest.FlowDefinition) (releases.PreparedReleaseSource, error) {
	graphs := make([]json.RawMessage, len(definitions))
	for i, definition := range definitions {
		graphs[i] = definition.Graph
	}
	live := infra.ScopeRef{ProjectID: project.ProjectID, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: project.Storage}
	registered, err := s.configuration.RegisterPreparedSource(ctx, live, source.AppManifest, graphs)
	if err != nil {
		return source, err
	}
	source.DefinitionRevision = registered.DefinitionRevision
	return source, nil
}

// FreezeReleaseConfiguration freezes the build's accepted draft revision for
// an actor who still owns the READY Project. Once the validation refuses the
// draft it asks for the gap report: a draft with gaps is refused with them,
// and one without keeps the refusal as the error, which the build asks again.
func (s *PublishingFlowService) FreezeReleaseConfiguration(ctx context.Context, state releases.Snapshot, build releases.ReleaseBuildRevision) (releases.ReadyConfiguration, []appmanifest.RequirementGap, error) {
	p, err := s.projects.AuthorizeCurrent(ctx, build.ActorAccountID, state.ProjectID, projects.CapabilityBuild)
	if err != nil {
		return releases.ReadyConfiguration{}, nil, fmt.Errorf("authorize the build's actor on Project %s: %w", state.ProjectID, err)
	}
	if p.ProjectStatus != projects.StatusReady {
		return releases.ReadyConfiguration{}, nil, fmt.Errorf("project %s is %s, not READY", state.ProjectID, p.ProjectStatus)
	}
	manifest := state.PreparedSource.AppManifest
	ready, err := s.configuration.FreezeReleaseConfiguration(ctx, p, manifest, build.BuildRevisionID, build.ConfigurationRevision)
	if !errors.Is(err, projectconfig.ErrNotReady) && !errors.Is(err, projectconfig.ErrConflict) {
		return ready, nil, err
	}
	live := infra.ScopeRef{ProjectID: p.ProjectID, Scope: infra.Scope{Kind: infra.ScopeLive}, Storage: p.Storage}
	gaps, gapsErr := s.configuration.CheckRequirements(ctx, live, manifest)
	if gapsErr != nil {
		return releases.ReadyConfiguration{}, nil, fmt.Errorf("list the configuration gaps of %s after %w: %w", state.SourceCommit, err, gapsErr)
	}
	if len(gaps) == 0 {
		return releases.ReadyConfiguration{}, nil, err
	}
	return releases.ReadyConfiguration{}, gaps, nil
}

// CheckReleaseDraft refuses actor's new build of the frozen copy of the
// Release's version when it is stale, built before Live, Preview's applied
// version or Live's next-publish layer changed, or moved past the revision
// the build would freeze.
func (s *PublishingFlowService) CheckReleaseDraft(ctx context.Context, actor string, state releases.Snapshot, configurationRevision uint64) error {
	copied, err := s.configuration.LiveDraftState(ctx, actor, state.ProjectID, state.ReleaseID)
	switch {
	case err != nil:
		return err
	case copied.Stale:
		return releases.ErrConfigurationStale
	case copied.ConfigurationRevision != configurationRevision:
		return dexflow.RejectStale("the Live configuration copy revision changed")
	}
	return nil
}

// DeployReleaseBuild deploys an automatically deployed build under its stable
// deployment ID and the Live revision the build was admitted against.
//
// The Release status is deliberately not re-checked: the Release Flow reaches
// this phase only for an auto-deploy build whose artifacts and frozen
// configuration are committed, and a recovered build keeps the phase with
// status RECOVERING and must reach the idempotent start. The build is not read
// either, because the deployment starts while its build revision is still
// pending.
func (s *PublishingFlowService) DeployReleaseBuild(ctx context.Context, state releases.Snapshot, build releases.ReleaseBuildRevision) (string, error) {
	start := Start{ProjectID: state.ProjectID, DeploymentID: strings.TrimPrefix(build.BuildRevisionID, "build-"), ReleaseID: state.ReleaseID, BuildRevisionID: build.BuildRevisionID, ConfigurationRevision: build.Configuration.ConfigurationRevision, ExpectedLiveRevision: build.ExpectedLiveRevision}
	p, err := s.deployments.admit(ctx, build.ActorAccountID, start)
	if err != nil {
		return "", err
	}
	liveRevision := uint64(0)
	if live := p.CurrentLive; live != nil {
		if live.DeploymentID == start.DeploymentID && live.BuildRevisionID == build.BuildRevisionID && live.ReleaseID == state.ReleaseID && live.Ready {
			return start.DeploymentID, nil
		}
		liveRevision = live.Revision
	}
	if liveRevision != build.ExpectedLiveRevision {
		return "", dexflow.Reject(dexflow.RejectionAborted, "Live changed while building; deploy this Release explicitly")
	}
	return s.deployments.start(ctx, build.ActorAccountID, "release-auto-"+build.Fingerprint, p, start)
}
