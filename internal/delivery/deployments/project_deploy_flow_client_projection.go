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
	"errors"
	"fmt"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/delivery/deliverydao"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/platform/repohost"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// DeploymentProjection is a deployment as the read model holds it, seconds
// behind ProjectDeployFlow. Its ExpectedLiveRevision and
// CleanupRequested are not synced and stay zero. It serves reads only: no
// write admission or Flow input takes one.
type DeploymentProjection struct {
	Deployment Snapshot
	// ReleaseVersion is the version of the deployed Release, zero for a
	// Release prepared before versions were recorded.
	ReleaseVersion repohost.CommitVersion
}

// LiveTarget is the Project's current Live deployment as the read model
// selects it: the Project's Live pointer, the storage and Live execution
// references it runs on, and the READY deployment the pointer names with the
// pointer's Release and build revision. ProjectFlow and ProjectDeployFlow
// checked what the deployment pinned, so nothing of it is compared again. It
// serves reads only; a write confirms the pointer with ProjectFlow.
type LiveTarget struct {
	ProjectStatus projects.Status
	Live          projects.LivePointer
	Storage       infra.StorageRefs
	Execution     infra.ExecutionRefs
	Deployment    Snapshot
}

// ReadProjection returns the read model's deployment of a Project whose
// access the caller checked. The row of a deployment started a moment ago is
// waited for (postgres.AwaitRow); one without a Flow is postgres.ErrNotFound.
func (c *ProjectDeployFlowClient) ReadProjection(ctx context.Context, project, deployment string) (DeploymentProjection, error) {
	if _, err := FlowID(project, deployment); err != nil {
		return DeploymentProjection{}, err
	}
	row, err := postgres.AwaitRow(ctx, func(ctx context.Context) (deliverydao.DeploymentRow, error) {
		return c.Rows.Deployment(ctx, project, deployment)
	}, func(ctx context.Context) (bool, error) {
		_, readErr := c.readSnapshot(ctx, project, deployment)
		if _, missing := errors.AsType[*dex.FlowNotFoundError](readErr); missing {
			return false, nil
		}
		return readErr == nil, readErr
	})
	if err != nil {
		return DeploymentProjection{}, err
	}
	return projectionOf(row)
}

// ListProjections returns at most limit deployments of a Project from the
// read model, newest first, after the page key when one is given, and the key
// of the page after them; statuses, when given, select those.
func (c *ProjectDeployFlowClient) ListProjections(ctx context.Context, project string, statuses []string, after *postgres.PageKey, limit int) ([]DeploymentProjection, *postgres.PageKey, error) {
	rows, next, err := c.Rows.ListDeployments(ctx, project, statuses, after, limit)
	if err != nil {
		return nil, nil, err
	}
	projections := make([]DeploymentProjection, 0, len(rows))
	for _, row := range rows {
		projection, decodeErr := projectionOf(row)
		if decodeErr != nil {
			return nil, nil, decodeErr
		}
		projections = append(projections, projection)
	}
	return projections, next, nil
}

// ReadLiveTarget returns the Project's current Live deployment that the read
// model selects; a Project without one is postgres.ErrNotFound.
func (c *ProjectDeployFlowClient) ReadLiveTarget(ctx context.Context, project string) (LiveTarget, error) {
	row, err := c.Rows.LiveTarget(ctx, project, Ready)
	if err != nil {
		return LiveTarget{}, err
	}
	target := LiveTarget{ProjectStatus: row.ProjectStatus.V}
	if target.Deployment, err = deploymentOf(row.Deployment); err != nil {
		return LiveTarget{}, err
	}
	err = errors.Join(
		postgres.DecodeJSON("current_live", row.CurrentLive, &target.Live),
		postgres.DecodeJSON("project_storage", row.ProjectStorage, &target.Storage),
		postgres.DecodeJSON("live_execution", row.LiveExecution, &target.Execution),
	)
	if err != nil {
		return LiveTarget{}, fmt.Errorf("the Live target of project %s: %w", project, err)
	}
	return target, nil
}

// projectionOf decodes a deployments row with the version of its Release.
func projectionOf(row deliverydao.DeploymentRow) (DeploymentProjection, error) {
	snapshot, err := deploymentOf(row)
	if err != nil {
		return DeploymentProjection{}, err
	}
	projection := DeploymentProjection{Deployment: snapshot}
	if err = postgres.DecodeJSON("release version", row.ReleaseVersion, &projection.ReleaseVersion); err != nil {
		return DeploymentProjection{}, fmt.Errorf("deployment %s: %w", row.DeploymentID, err)
	}
	return projection, nil
}

// deploymentOf decodes a deployments row into the types of the Attributes Dex
// wrote it from.
func deploymentOf(row deliverydao.DeploymentRow) (Snapshot, error) {
	s := Snapshot{
		ProjectID: row.ProjectID, DeploymentID: row.DeploymentID, ReleaseID: row.ReleaseID.V, BuildRevisionID: row.BuildRevisionID.V,
		Status: row.DeploymentStatus.V, URL: row.DeploymentURL.V, ConfigurationRevision: postgres.Unsigned(row.ConfigurationRevision),
		Revision: postgres.Unsigned(row.Revision), CreatedAt: row.CreatedAt.V, UpdatedAt: row.UpdatedAt.V,
	}
	err := errors.Join(
		postgres.DecodeJSON("project_deployment_lease", row.ProjectDeploymentLease, &s.Lease),
		postgres.DecodeJSON("pinned_inputs", row.PinnedInputs, &s.Inputs),
		postgres.DecodeJSON("workload_grant", row.WorkloadGrant, &s.Grant),
		postgres.DecodeJSON("readiness_result", row.ReadinessResult, &s.Readiness),
		postgres.DecodeJSON("deployment_resources", row.DeploymentResources, &s.Resources),
		postgres.DecodeJSON("provider_operation_refs", row.ProviderOperationRefs, &s.Providers),
		postgres.DecodeJSON("phase_timings", row.PhaseTimings, &s.Timings),
		postgres.DecodeJSON("failure", row.Failure, &s.Failure),
		postgres.DecodeJSON("start_identity", row.StartIdentity, &s.Identity),
		postgres.DecodeJSON("cancellation", row.Cancellation, &s.Cancellation),
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf("deployment %s: %w", row.DeploymentID, err)
	}
	return s, nil
}
