// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package diagnostics

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/toolinput"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	buildlog "github.com/superdurable/superversev2/internal/delivery/releases/build"
	"github.com/superdurable/superversev2/internal/projects"
)

const (
	// newestChildren is how many Releases and Deployments the status lists.
	newestChildren = 3
	// maximumBuildLogBytes bounds the log tail of a failed build.
	maximumBuildLogBytes = 8 << 10
)

// deliveryStatus is where the Project's source stands on its way to Live.
// Every resource names the FlowID that read_platform_flow reads.
type deliveryStatus struct {
	Project     projectStatus      `json:"project"`
	Setup       *setupStatus       `json:"setup,omitempty"`
	HeadCommit  string             `json:"head_commit,omitempty"`
	Preview     previewStatus      `json:"preview"`
	Releases    []releaseStatus    `json:"releases"`
	Deployments []deploymentStatus `json:"deployments"`
}

type projectStatus struct {
	FlowID             string      `json:"flow_id"`
	Status             string      `json:"status"`
	Live               *liveStatus `json:"live,omitempty"`
	ActiveDeploymentID string      `json:"active_deployment_id,omitempty"`
	Failure            *failure    `json:"failure,omitempty"`
}

type liveStatus struct {
	ReleaseID       string `json:"release_id"`
	BuildRevisionID string `json:"build_revision_id"`
	DeploymentID    string `json:"deployment_id"`
	Revision        uint64 `json:"revision"`
	Ready           bool   `json:"ready"`
	URL             string `json:"url,omitempty"`
}

type setupStatus struct {
	FlowID       string   `json:"flow_id"`
	Status       string   `json:"status"`
	Phase        string   `json:"phase,omitempty"`
	SourceCommit string   `json:"source_commit,omitempty"`
	Failure      *failure `json:"failure,omitempty"`
}

// previewStatus is the Project's Preview with the Sandbox instance it uses;
// NOT_STARTED when the Project never ran a Preview command.
type previewStatus struct {
	FlowID             string        `json:"flow_id,omitempty"`
	WorkspaceRequestID string        `json:"workspace_request_id,omitempty"`
	Status             string        `json:"status"`
	URL                string        `json:"url,omitempty"`
	SourceCommit       string        `json:"source_commit,omitempty"`
	PreparedSource     *sourceStatus `json:"prepared_source,omitempty"`
	EngineStatus       string        `json:"engine_status,omitempty"`
	EngineLastStop     string        `json:"engine_last_stop,omitempty"`
	Failure            *failure      `json:"failure,omitempty"`
}

type sourceStatus struct {
	Status       string   `json:"status"`
	SourceCommit string   `json:"source_commit,omitempty"`
	Failure      *failure `json:"failure,omitempty"`
}

type releaseStatus struct {
	FlowID    string        `json:"flow_id"`
	ReleaseID string        `json:"release_id"`
	Status    string        `json:"status"`
	Source    *sourceStatus `json:"source,omitempty"`
	Build     *buildStatus  `json:"build,omitempty"`
	Failure   *failure      `json:"failure,omitempty"`
}

// buildStatus is the latest build revision of a Release; a failed one carries
// the redacted tail of its build log.
type buildStatus struct {
	BuildRevisionID   string   `json:"build_revision_id"`
	Status            string   `json:"status"`
	Failure           *failure `json:"failure,omitempty"`
	AutoDeployStatus  string   `json:"auto_deploy_status,omitempty"`
	AutoDeployFailure *failure `json:"auto_deploy_failure,omitempty"`
	LogTail           string   `json:"log_tail,omitempty"`
}

type deploymentStatus struct {
	FlowID          string   `json:"flow_id"`
	DeploymentID    string   `json:"deployment_id"`
	ReleaseID       string   `json:"release_id"`
	BuildRevisionID string   `json:"build_revision_id"`
	Status          string   `json:"status"`
	Phase           string   `json:"phase,omitempty"`
	URL             string   `json:"url,omitempty"`
	Failure         *failure `json:"failure,omitempty"`
}

// readDeliveryStatus reads the Project, its Setup, the Session's Sandbox
// instance and the newest Releases and Deployments, or the ones the call
// names. The Sandbox is read, never started.
func (d *ProjectDiagnostics) readDeliveryStatus(ctx context.Context, identity agentloop.SessionIdentity, arguments string) ([]byte, error) {
	input, err := decodeArguments(toolinput.ReadDeliveryStatus.Decode, arguments)
	if err != nil {
		return nil, err
	}
	projection, err := d.Projects.ReadProjection(ctx, identity.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("read the Project: %w", err)
	}
	project := projection.Project
	redaction := newRedactor(project.ProjectID, d.Redactor)
	status := deliveryStatus{Project: projectStatusOf(redaction, project), Setup: setupStatusOf(redaction, project, projection.Setup),
		HeadCommit: identity.HeadCommit, Releases: []releaseStatus{}, Deployments: []deploymentStatus{}}
	if status.Preview, err = d.previewStatus(ctx, redaction, identity); err != nil {
		return nil, err
	}
	releaseProjections, err := d.releaseProjections(ctx, project.ProjectID, input.ReleaseID.Or(""))
	if err != nil {
		return nil, err
	}
	for _, projection := range releaseProjections {
		release, readErr := d.releaseStatus(ctx, redaction, project, projection)
		if readErr != nil {
			return nil, readErr
		}
		status.Releases = append(status.Releases, release)
	}
	deploymentProjections, err := d.deploymentProjections(ctx, project.ProjectID, input.DeploymentID.Or(""))
	if err != nil {
		return nil, err
	}
	for _, projection := range deploymentProjections {
		status.Deployments = append(status.Deployments, deploymentStatusOf(redaction, projection.Deployment))
	}
	// The log tails of failed builds are the largest part of the status: they
	// shrink until the status fits.
	return redaction.fitted(maximumBuildLogBytes, func(logBytes int) any {
		fitted := status
		fitted.Releases = slices.Clone(status.Releases)
		for index, release := range fitted.Releases {
			if release.Build != nil {
				build := *release.Build
				build.LogTail = comm.TailUTF8(build.LogTail, logBytes)
				fitted.Releases[index].Build = &build
			}
		}
		return fitted
	}), nil
}

// releaseProjections are the Release the call names, or the newest Releases
// of the Project, from the read model.
func (d *ProjectDiagnostics) releaseProjections(ctx context.Context, projectID, named string) ([]releases.ReleaseProjection, error) {
	if named == "" {
		listed, _, err := d.Releases.ListProjections(ctx, projectID, nil, nil, newestChildren)
		if err != nil {
			return nil, fmt.Errorf("list the Releases: %w", err)
		}
		return listed, nil
	}
	projection, err := d.Releases.ReadProjection(ctx, projectID, named)
	if err != nil {
		return nil, notFound(fmt.Errorf("read Release %s: %w", named, err), "Release "+named)
	}
	return []releases.ReleaseProjection{projection}, nil
}

// deploymentProjections are the Deployment the call names, or the newest
// Deployments of the Project, from the read model.
func (d *ProjectDiagnostics) deploymentProjections(ctx context.Context, projectID, named string) ([]deployments.DeploymentProjection, error) {
	if named == "" {
		listed, _, err := d.Deployments.ListProjections(ctx, projectID, nil, nil, newestChildren)
		if err != nil {
			return nil, fmt.Errorf("list the Deployments: %w", err)
		}
		return listed, nil
	}
	projection, err := d.Deployments.ReadProjection(ctx, projectID, named)
	if err != nil {
		return nil, notFound(fmt.Errorf("read Deployment %s: %w", named, err), "Deployment "+named)
	}
	return []deployments.DeploymentProjection{projection}, nil
}

func projectStatusOf(redaction redactor, project projects.Project) projectStatus {
	status := projectStatus{FlowID: flowid.ProjectFlowID(project.ProjectID), Status: project.ProjectStatus, Failure: redaction.failure(project.LastFailure)}
	if live := project.CurrentLive; live != nil {
		status.Live = &liveStatus{ReleaseID: live.ReleaseID, BuildRevisionID: live.BuildRevisionID, DeploymentID: live.DeploymentID, Revision: live.Revision, Ready: live.Ready, URL: live.URL}
	}
	if active := project.ActiveDeployment; active != nil {
		status.ActiveDeploymentID = active.DeploymentID
	}
	return status
}

// setupStatusOf is the Setup the Project's row holds, none before the Setup's
// first write reaches it.
func setupStatusOf(redaction redactor, project projects.Project, setup projects.SetupSummary) *setupStatus {
	if setup.Status == "" {
		return nil
	}
	return &setupStatus{FlowID: project.SetupFlowID, Status: setup.Status, Phase: setup.Phase, SourceCommit: setup.SourceCommit, Failure: redaction.failure(setup.Failure)}
}

// previewStatus reads the Project's Preview, also one whose instance was
// reclaimed. Its failures already carry the supervisor's redaction of the
// configured environment of their start.
func (d *ProjectDiagnostics) previewStatus(ctx context.Context, redaction redactor, identity agentloop.SessionIdentity) (previewStatus, error) {
	state, err := d.Previews.Get(ctx, identity.ProjectID)
	var missing *dex.FlowNotFoundError
	if errors.As(err, &missing) {
		return previewStatus{Status: preview.PreviewNotStarted}, nil
	}
	if err != nil {
		return previewStatus{}, fmt.Errorf("read the Preview: %w", err)
	}
	status := previewStatus{
		FlowID: preview.FlowID(identity.ProjectID), WorkspaceRequestID: state.WorkspaceRequestID,
		Status: state.Preview.Status, URL: state.Preview.URL, SourceCommit: state.Preview.SourceCommit,
		EngineStatus: state.Environment.Status, EngineLastStop: state.Environment.LastStop, Failure: redaction.failure(state.Failure),
	}
	if cause := state.Environment.LastStopCause; cause != "" {
		status.EngineLastStop += ": " + cause
	}
	if prepared := state.PreparedSource; prepared.Status != "" {
		status.PreparedSource = &sourceStatus{Status: prepared.Status, SourceCommit: prepared.SourceCommit, Failure: redaction.failure(prepared.Failure)}
	}
	return status, nil
}

// releaseStatus shows a Release of the read model with its active build
// revision. The log tail of a failed build is redacted with the build's own
// configured values.
func (d *ProjectDiagnostics) releaseStatus(ctx context.Context, redaction redactor, project projects.Project, projection releases.ReleaseProjection) (releaseStatus, error) {
	release := projection.Release
	flowID := comm.Must(releases.FlowID(project.ProjectID, release.ReleaseID))
	status := releaseStatus{FlowID: flowID, ReleaseID: release.ReleaseID, Status: release.Status, Failure: redaction.failure(release.Failure)}
	if source := release.PreparedSource; source.Status != "" {
		status.Source = &sourceStatus{Status: source.Status, SourceCommit: source.SourceCommit, Failure: redaction.failure(source.Failure)}
	}
	if release.ActiveBuildRevisionID == "" {
		return status, nil
	}
	// A projection holds its active build first.
	build := projection.Builds[0]
	status.Build = &buildStatus{BuildRevisionID: build.BuildRevisionID, Status: build.Status, Failure: redaction.failure(build.Failure),
		AutoDeployStatus: build.AutoDeployStatus, AutoDeployFailure: redaction.failure(build.AutoDeployFailure)}
	if build.Status != releases.Failed {
		return status, nil
	}
	// The tail is read longer than it is shown, so that the cut after redaction
	// never keeps part of a value. A read the replica deferred shows no tail.
	tail, err := d.Publishing.ReadBuildLog(ctx, project, release.ReleaseID, build.BuildRevisionID, buildlog.LogRead{Tail: true, MaximumBytes: 2 * maximumBuildLogBytes})
	if err != nil {
		return releaseStatus{}, fmt.Errorf("read the log of build revision %s: %w", build.BuildRevisionID, err)
	}
	status.Build.LogTail = comm.TailUTF8(tail.Text, maximumBuildLogBytes)
	return status, nil
}

func deploymentStatusOf(redaction redactor, deployment deployments.Snapshot) deploymentStatus {
	flowID := comm.Must(deployments.FlowID(deployment.ProjectID, deployment.DeploymentID))
	return deploymentStatus{
		FlowID: flowID, DeploymentID: deployment.DeploymentID, ReleaseID: deployment.ReleaseID, BuildRevisionID: deployment.BuildRevisionID,
		Status: deployment.Status, Phase: deployment.Providers.Phase, URL: deployment.URL, Failure: redaction.failure(deployment.Failure),
	}
}
