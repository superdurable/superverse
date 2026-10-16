// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases

import (
	"context"
	"errors"
	"fmt"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deliverydao"
)

// ReleaseProjection is a Release as the read model holds it, seconds behind
// ProjectReleaseFlow, with its active build revision and, when another, its
// ready one. The Release's Identity, Repository, BuildProfileDigest and
// CancellationRequested and a build's Fingerprint, BuildProfileDigest,
// Operation and ExpectedLiveRevision are not synced and stay zero. It serves
// reads only: no write admission or Flow input takes one.
type ReleaseProjection struct {
	Release Snapshot
	Builds  []ReleaseBuildRevision
}

// ReadProjection returns the read model's Release of a Project whose access
// the caller checked. The row of a Release prepared a moment ago is waited for
// (postgres.AwaitRow); a Release without a Flow is postgres.ErrNotFound.
func (c *ProjectReleaseFlowClient) ReadProjection(ctx context.Context, project, release string) (ReleaseProjection, error) {
	if _, err := FlowID(project, release); err != nil {
		return ReleaseProjection{}, err
	}
	row, err := postgres.AwaitRow(ctx, func(ctx context.Context) (deliverydao.ReleaseRow, error) {
		return c.Rows.Release(ctx, project, release)
	}, func(ctx context.Context) (bool, error) {
		_, readErr := c.ReadSnapshot(ctx, project, release)
		if _, missing := errors.AsType[*dex.FlowNotFoundError](readErr); missing {
			return false, nil
		}
		return readErr == nil, readErr
	})
	if err != nil {
		return ReleaseProjection{}, err
	}
	projections, err := c.projections(ctx, project, []deliverydao.ReleaseRow{row})
	if err != nil {
		return ReleaseProjection{}, err
	}
	return projections[0], nil
}

// ListProjections returns at most limit Releases of a Project from the read
// model, newest first, after the page key when one is given, and the key of
// the page after them; statuses, when given, select those.
func (c *ProjectReleaseFlowClient) ListProjections(ctx context.Context, project string, statuses []string, after *postgres.PageKey, limit int) ([]ReleaseProjection, *postgres.PageKey, error) {
	rows, next, err := c.Rows.ListReleases(ctx, project, statuses, after, limit)
	if err != nil {
		return nil, nil, err
	}
	projections, err := c.projections(ctx, project, rows)
	return projections, next, err
}

// projections decodes Release rows with their active and ready builds, which
// one query reads for all of them.
func (c *ProjectReleaseFlowClient) projections(ctx context.Context, project string, rows []deliverydao.ReleaseRow) ([]ReleaseProjection, error) {
	var ids []string
	for _, row := range rows {
		for _, id := range []string{row.ActiveBuildRevisionID.V, row.ReadyBuildRevisionID.V} {
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	builds := map[string]ReleaseBuildRevision{}
	if len(ids) > 0 {
		buildRows, err := c.Rows.BuildRevisions(ctx, project, ids)
		if err != nil {
			return nil, err
		}
		for _, row := range buildRows {
			build, err := buildRevisionOf(row)
			if err != nil {
				return nil, err
			}
			builds[build.BuildRevisionID] = build
		}
	}
	projections := make([]ReleaseProjection, 0, len(rows))
	for _, row := range rows {
		release, err := releaseOf(row)
		if err != nil {
			return nil, err
		}
		projection := ReleaseProjection{Release: release}
		for index, id := range []string{release.ActiveBuildRevisionID, release.ReadyBuildRevisionID} {
			if id == "" || index == 1 && id == release.ActiveBuildRevisionID {
				continue
			}
			build, found := builds[id]
			if !found {
				// Dex writes a build's row in the batch that names it.
				return nil, fmt.Errorf("build revision %s of Release %s has no row", id, release.ReleaseID)
			}
			projection.Builds = append(projection.Builds, build)
		}
		projections = append(projections, projection)
	}
	return projections, nil
}

// releaseOf decodes a releases row into the types of the Attributes Dex wrote
// it from.
func releaseOf(row deliverydao.ReleaseRow) (Snapshot, error) {
	s := Snapshot{
		ProjectID: row.ProjectID, ReleaseID: row.ReleaseID, SourceCommit: row.SourceCommit.V, BuildProfileVersion: row.BuildProfileVersion.V,
		Status: row.ReleaseStatus.V, ActiveBuildRevisionID: row.ActiveBuildRevisionID.V, ReadyBuildRevisionID: row.ReadyBuildRevisionID.V,
		Revision: postgres.Unsigned(row.Revision), CreatedAt: row.CreatedAt.V, UpdatedAt: row.UpdatedAt.V,
	}
	err := errors.Join(
		postgres.DecodeJSON("prepared_source", row.PreparedSource, &s.PreparedSource),
		postgres.DecodeJSON("failure", row.Failure, &s.Failure),
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf("release %s: %w", row.ReleaseID, err)
	}
	return s, nil
}

// buildRevisionOf decodes a release_build_revisions row, the fields of one
// ReleaseBuildRevisions instance that have a column.
func buildRevisionOf(row deliverydao.BuildRevisionRow) (ReleaseBuildRevision, error) {
	b := ReleaseBuildRevision{
		BuildRevisionID: row.BuildRevisionID, ActorAccountID: row.ActorAccountID.V, ConfigurationRevision: postgres.Unsigned(row.ConfigurationRevision),
		BuildProfileVersion: row.BuildProfileVersion.V, Status: row.Status.V, Revision: postgres.Unsigned(row.Revision),
		ImageReference: row.ImageReference.V, ImageDigest: row.ImageDigest.V, ProviderSettled: row.ProviderSettled.V, AutoDeploy: row.AutoDeploy.V,
		AutoDeploymentID: row.AutoDeploymentID.V, AutoDeployStatus: row.AutoDeployStatus.V, CreatedAt: row.CreatedAt.V, UpdatedAt: row.UpdatedAt.V,
	}
	err := errors.Join(
		postgres.DecodeJSON("configuration", row.Configuration, &b.Configuration),
		postgres.DecodeJSON("provenance", row.Provenance, &b.Provenance),
		postgres.DecodeJSON("artifacts", row.Artifacts, &b.Artifacts),
		postgres.DecodeJSON("failure", row.Failure, &b.Failure),
		postgres.DecodeJSON("auto_deploy_failure", row.AutoDeployFailure, &b.AutoDeployFailure),
		postgres.DecodeJSON("timings", row.Timings, &b.Timings),
		postgres.DecodeJSON("preview_validation", row.PreviewValidation, &b.PreviewValidation),
		postgres.DecodeJSON("cancellation", row.Cancellation, &b.Cancellation),
	)
	if err != nil {
		return ReleaseBuildRevision{}, fmt.Errorf("build revision %s: %w", row.BuildRevisionID, err)
	}
	return b, nil
}
