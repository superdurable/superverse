// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deliverydao

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
)

// ReleaseRow is one releases row, written by Dex from ProjectReleaseFlow's
// synced Attributes, so every column but the key may be NULL. A JSON column
// holds its Attribute's JSON encoding, nil for NULL, which the owning package
// decodes.
type ReleaseRow struct {
	ProjectID, ReleaseID                             string
	ReleaseStatus, SourceCommit, BuildProfileVersion sql.Null[string]
	ActiveBuildRevisionID, ReadyBuildRevisionID      sql.Null[string]
	Revision                                         sql.Null[int64]
	PreparedSource, Failure                          json.RawMessage
	CreatedAt, UpdatedAt                             sql.Null[time.Time]
}

// BuildRevisionRow is one release_build_revisions row: the whole latest
// value of one ReleaseBuildRevisions entry, without the fields that have no
// column.
type BuildRevisionRow struct {
	ProjectID, ReleaseID, BuildRevisionID                                     string
	ActorAccountID, BuildProfileVersion, Status, ImageReference, ImageDigest  sql.Null[string]
	AutoDeploymentID, AutoDeployStatus                                        sql.Null[string]
	ConfigurationRevision, Revision                                           sql.Null[int64]
	AutoDeploy, ProviderSettled                                               sql.Null[bool]
	Configuration, Provenance, Artifacts, Failure, AutoDeployFailure, Timings json.RawMessage
	PreviewValidation                                                         json.RawMessage
	// Cancellation is NULL for a build nobody cancelled.
	Cancellation         json.RawMessage
	CreatedAt, UpdatedAt sql.Null[time.Time]
}

// DeploymentRow is one deployments row, written by Dex from
// ProjectDeployFlow's synced Attributes.
type DeploymentRow struct {
	ProjectID, DeploymentID                                              string
	ReleaseID, BuildRevisionID, DeploymentStatus, DeploymentURL          sql.Null[string]
	ConfigurationRevision, Revision                                      sql.Null[int64]
	ProjectDeploymentLease, PinnedInputs, WorkloadGrant, ReadinessResult json.RawMessage
	DeploymentResources, ProviderOperationRefs, PhaseTimings, Failure    json.RawMessage
	// StartIdentity is NULL for a deployment that started before the column.
	StartIdentity json.RawMessage
	// Cancellation is NULL for a deployment nobody cancelled.
	Cancellation         json.RawMessage
	CreatedAt, UpdatedAt sql.Null[time.Time]
	// ReleaseVersion is the version of the deployed Release, from its
	// prepared source; NULL before versions were recorded.
	ReleaseVersion json.RawMessage
}

// LiveEventRow is one entry of a Project's Live history. A published entry is
// a build revision with automatic deployment, joined with the deployment it
// started; any other is a deployment that no build started. The columns of the
// side an entry lacks are NULL.
type LiveEventRow struct {
	EventID                                                           string
	Published                                                         bool
	ReleaseID, BuildRevisionID                                        string
	DeploymentID, ActorAccountID, BuildStatus, DeploymentStatus       sql.Null[string]
	ConfigurationRevision                                             sql.Null[int64]
	StartIdentity, BuildFailure, AutoDeployFailure, DeploymentFailure json.RawMessage
	// PreviewValidation is the build's, NULL for a deployment no build started.
	PreviewValidation json.RawMessage
	// Cancellation is a person's cancellation of the entry: its deployment's,
	// or else its build's; NULL when nobody cancelled it.
	Cancellation                    json.RawMessage
	BuildTimings, DeploymentTimings json.RawMessage
	SourceCommit                    sql.Null[string]
	// Version is the Release's prepared source version, NULL for a Release
	// prepared before versions were recorded.
	Version json.RawMessage
	// ReplacedReleaseCreatedAt is when the Release of the newest READY
	// deployment before this entry was created, NULL when there is none.
	ReplacedReleaseCreatedAt, ReleaseCreatedAt sql.Null[time.Time]
	OccurredAt                                 time.Time
}

// LiveTargetRow is what Live Runs resolve in one read: the Project's status,
// its Live pointer and the references it runs on, and the serving Deployment
// that pointer names.
type LiveTargetRow struct {
	ProjectStatus                              sql.Null[string]
	CurrentLive, ProjectStorage, LiveExecution json.RawMessage
	Deployment                                 DeploymentRow
}

// DeliveryDAO reads the releases, release_build_revisions and deployments
// tables, which only Dex writes, and the projects row that a Live target
// starts from.
type DeliveryDAO struct {
	db *sql.DB
}

// New returns the DAO of db.
func New(db *sql.DB) *DeliveryDAO { return &DeliveryDAO{db: db} }

const (
	releaseColumns = `r.project_id, r.release_id, r.release_status, r.source_commit, r.build_profile_version,
		r.active_build_revision_id, r.ready_build_revision_id, r.revision, r.prepared_source, r.failure, r.created_at, r.updated_at`
	buildRevisionColumns = `b.project_id, b.release_id, b.build_revision_id, b.actor_account_id, b.build_profile_version, b.status,
		b.image_reference, b.image_digest, b.auto_deployment_id, b.auto_deploy_status, b.configuration_revision, b.revision,
		b.auto_deploy, b.provider_settled, b.configuration, b.provenance, b.artifacts, b.failure, b.auto_deploy_failure, b.timings,
		b.preview_validation, b.cancellation, b.created_at, b.updated_at`
	deploymentColumns = `d.project_id, d.deployment_id, d.release_id, d.build_revision_id, d.deployment_status, d.deployment_url,
		d.configuration_revision, d.revision, d.project_deployment_lease, d.pinned_inputs, d.workload_grant, d.readiness_result,
		d.deployment_resources, d.provider_operation_refs, d.phase_timings, d.failure, d.start_identity, d.cancellation, d.created_at, d.updated_at, dr.prepared_source->'Version'`
	// deploymentRelease joins the Release a deployment deploys, for its
	// version.
	deploymentRelease = ` LEFT JOIN releases dr ON dr.project_id = d.project_id AND dr.release_id = d.release_id`
)

// Release returns one Release row.
func (d *DeliveryDAO) Release(ctx context.Context, projectID, releaseID string) (ReleaseRow, error) {
	var r ReleaseRow
	err := d.db.QueryRowContext(ctx, `SELECT `+releaseColumns+` FROM releases r
		WHERE r.project_id = $1 AND r.release_id = $2`, projectID, releaseID).Scan(r.destinations()...)
	return r, postgres.RowError("read the release", err)
}

// ListReleases returns at most limit Releases of a Project, newest first,
// after the page key when one is given, and the key of the page after them;
// statuses, when given, select those.
func (d *DeliveryDAO) ListReleases(ctx context.Context, projectID string, statuses []string, after *postgres.PageKey, limit int) ([]ReleaseRow, *postgres.PageKey, error) {
	afterCreatedAt, afterID := postgres.AfterValues(after)
	rows, err := postgres.QueryRows(ctx, d.db, "list the releases", scanRelease, `SELECT `+releaseColumns+` FROM releases r
		WHERE r.project_id = $1 AND r.release_status IS NOT NULL AND r.created_at IS NOT NULL
			AND (COALESCE(cardinality($2::text[]), 0) = 0 OR r.release_status = ANY($2))
			AND ($3::timestamptz IS NULL OR (r.created_at, r.release_id) < ($3, $4))
		ORDER BY r.created_at DESC, r.release_id DESC LIMIT $5`, projectID, statuses, afterCreatedAt, afterID, limit+1)
	if err != nil {
		return nil, nil, err
	}
	page, next := postgres.Page(rows, limit, func(r ReleaseRow) postgres.PageKey {
		return postgres.PageKey{CreatedAt: r.CreatedAt.V, ID: r.ReleaseID}
	})
	return page, next, nil
}

// BuildRevisions returns the build revisions of a Project with the given IDs,
// which are unique within the Project.
func (d *DeliveryDAO) BuildRevisions(ctx context.Context, projectID string, buildRevisionIDs []string) ([]BuildRevisionRow, error) {
	return postgres.QueryRows(ctx, d.db, "read the build revisions", scanBuildRevision, `SELECT `+buildRevisionColumns+`
		FROM release_build_revisions b WHERE b.project_id = $1 AND b.build_revision_id = ANY($2)`, projectID, buildRevisionIDs)
}

// Deployment returns one Deployment row.
func (d *DeliveryDAO) Deployment(ctx context.Context, projectID, deploymentID string) (DeploymentRow, error) {
	var r DeploymentRow
	err := d.db.QueryRowContext(ctx, `SELECT `+deploymentColumns+` FROM deployments d`+deploymentRelease+`
		WHERE d.project_id = $1 AND d.deployment_id = $2`, projectID, deploymentID).Scan(r.destinations()...)
	return r, postgres.RowError("read the deployment", err)
}

// ListDeployments returns at most limit Deployments of a Project, newest
// first, after the page key when one is given, and the key of the page after
// them; statuses, when given, select those.
func (d *DeliveryDAO) ListDeployments(ctx context.Context, projectID string, statuses []string, after *postgres.PageKey, limit int) ([]DeploymentRow, *postgres.PageKey, error) {
	afterCreatedAt, afterID := postgres.AfterValues(after)
	rows, err := postgres.QueryRows(ctx, d.db, "list the deployments", scanDeployment, `SELECT `+deploymentColumns+` FROM deployments d`+deploymentRelease+`
		WHERE d.project_id = $1 AND d.deployment_status IS NOT NULL AND d.created_at IS NOT NULL
			AND (COALESCE(cardinality($2::text[]), 0) = 0 OR d.deployment_status = ANY($2))
			AND ($3::timestamptz IS NULL OR (d.created_at, d.deployment_id) < ($3, $4))
		ORDER BY d.created_at DESC, d.deployment_id DESC LIMIT $5`, projectID, statuses, afterCreatedAt, afterID, limit+1)
	if err != nil {
		return nil, nil, err
	}
	page, next := postgres.Page(rows, limit, func(r DeploymentRow) postgres.PageKey {
		return postgres.PageKey{CreatedAt: r.CreatedAt.V, ID: r.DeploymentID}
	})
	return page, next, nil
}

// ListLiveEvents returns at most limit entries of a Project's Live history,
// newest first, after the page key when one is given, and the key of the page
// after them. An entry is the publishing of a build revision that deploys
// automatically, or a deployment that no build started (a deployment of an
// existing version).
func (d *DeliveryDAO) ListLiveEvents(ctx context.Context, projectID string, after *postgres.PageKey, limit int) ([]LiveEventRow, *postgres.PageKey, error) {
	afterAt, afterID := postgres.AfterValues(after)
	rows, err := postgres.QueryRows(ctx, d.db, "list the Live events", func(scan func(...any) error) (LiveEventRow, error) {
		var e LiveEventRow
		err := scan(&e.EventID, &e.Published, &e.ReleaseID, &e.BuildRevisionID, &e.DeploymentID, &e.ActorAccountID, &e.BuildStatus, &e.DeploymentStatus,
			&e.ConfigurationRevision, postgres.JSONColumn(&e.StartIdentity), postgres.JSONColumn(&e.BuildFailure), postgres.JSONColumn(&e.AutoDeployFailure),
			postgres.JSONColumn(&e.DeploymentFailure), postgres.JSONColumn(&e.PreviewValidation), postgres.JSONColumn(&e.BuildTimings), postgres.JSONColumn(&e.DeploymentTimings), &e.OccurredAt,
			&e.SourceCommit, postgres.JSONColumn(&e.Version), &e.ReleaseCreatedAt, &e.ReplacedReleaseCreatedAt, postgres.JSONColumn(&e.Cancellation))
		return e, err
	}, `WITH events AS (
			SELECT 'build:' || b.build_revision_id AS event_id, TRUE AS published, b.release_id, b.build_revision_id, d.deployment_id,
				b.actor_account_id, b.status AS build_status, d.deployment_status, b.configuration_revision, NULL::jsonb AS start_identity,
				b.failure AS build_failure, b.auto_deploy_failure, d.failure AS deployment_failure, b.preview_validation, b.timings AS build_timings,
				d.phase_timings AS deployment_timings, b.created_at AS occurred_at, COALESCE(d.cancellation, b.cancellation) AS cancellation
			FROM release_build_revisions b
			LEFT JOIN deployments d ON d.project_id = b.project_id AND d.deployment_id = b.auto_deployment_id
			WHERE b.project_id = $1 AND b.auto_deploy AND b.created_at IS NOT NULL
			UNION ALL
			SELECT 'deploy:' || d.deployment_id, FALSE, d.release_id, d.build_revision_id, d.deployment_id,
				NULL, NULL, d.deployment_status, d.configuration_revision, d.start_identity,
				NULL, NULL, d.failure, NULL, NULL, d.phase_timings, d.created_at, d.cancellation
			FROM deployments d
			WHERE d.project_id = $1 AND d.deployment_status IS NOT NULL AND d.created_at IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM release_build_revisions b WHERE b.project_id = d.project_id AND b.auto_deployment_id = d.deployment_id)
		)
		SELECT e.event_id, e.published, e.release_id, e.build_revision_id, e.deployment_id, e.actor_account_id, e.build_status, e.deployment_status,
			e.configuration_revision, e.start_identity, e.build_failure, e.auto_deploy_failure, e.deployment_failure, e.preview_validation, e.build_timings, e.deployment_timings,
			e.occurred_at, r.source_commit, r.prepared_source->'Version', r.created_at,
			(SELECT r2.created_at FROM deployments d2 JOIN releases r2 ON r2.project_id = d2.project_id AND r2.release_id = d2.release_id
				WHERE d2.project_id = $1 AND d2.deployment_status = 'READY' AND d2.created_at < e.occurred_at
				ORDER BY d2.created_at DESC LIMIT 1), e.cancellation
		FROM events e LEFT JOIN releases r ON r.project_id = $1 AND r.release_id = e.release_id
		WHERE ($2::timestamptz IS NULL OR (e.occurred_at, e.event_id) < ($2, $3))
		ORDER BY e.occurred_at DESC, e.event_id DESC LIMIT $4`, projectID, afterAt, afterID, limit+1)
	if err != nil {
		return nil, nil, err
	}
	page, next := postgres.Page(rows, limit, func(e LiveEventRow) postgres.PageKey {
		return postgres.PageKey{CreatedAt: e.OccurredAt, ID: e.EventID}
	})
	return page, next, nil
}

// LiveTarget selects the Live target of a Project: the Deployment that its
// Live pointer names, in the serving status the caller passes and with the
// pointer's Release and build revision. It reads the pointer's DeploymentID,
// ReleaseID and BuildRevisionID, the Go field names of projects.LivePointer,
// which has no JSON tags. A Project without a Live pointer, or whose pointer
// selects no such row, has none: postgres.ErrNotFound.
func (d *DeliveryDAO) LiveTarget(ctx context.Context, projectID, servingStatus string) (LiveTargetRow, error) {
	var t LiveTargetRow
	destinations := []any{&t.ProjectStatus, postgres.JSONColumn(&t.CurrentLive), postgres.JSONColumn(&t.ProjectStorage), postgres.JSONColumn(&t.LiveExecution)}
	err := d.db.QueryRowContext(ctx, `SELECT p.project_status, p.current_live, p.project_storage, p.live_execution, `+deploymentColumns+`
		FROM projects p
		JOIN deployments d ON d.project_id = p.project_id AND d.deployment_id = p.current_live->>'DeploymentID'
			AND d.deployment_status = $2 AND d.release_id = p.current_live->>'ReleaseID'
			AND d.build_revision_id = p.current_live->>'BuildRevisionID'`+deploymentRelease+`
		WHERE p.project_id = $1`, projectID, servingStatus).Scan(append(destinations, t.Deployment.destinations()...)...)
	return t, postgres.RowError("read the Live target", err)
}

func (r *ReleaseRow) destinations() []any {
	return []any{&r.ProjectID, &r.ReleaseID, &r.ReleaseStatus, &r.SourceCommit, &r.BuildProfileVersion,
		&r.ActiveBuildRevisionID, &r.ReadyBuildRevisionID, &r.Revision, postgres.JSONColumn(&r.PreparedSource), postgres.JSONColumn(&r.Failure), &r.CreatedAt, &r.UpdatedAt}
}

func (b *BuildRevisionRow) destinations() []any {
	return []any{&b.ProjectID, &b.ReleaseID, &b.BuildRevisionID, &b.ActorAccountID, &b.BuildProfileVersion, &b.Status,
		&b.ImageReference, &b.ImageDigest, &b.AutoDeploymentID, &b.AutoDeployStatus, &b.ConfigurationRevision, &b.Revision,
		&b.AutoDeploy, &b.ProviderSettled, postgres.JSONColumn(&b.Configuration), postgres.JSONColumn(&b.Provenance), postgres.JSONColumn(&b.Artifacts),
		postgres.JSONColumn(&b.Failure), postgres.JSONColumn(&b.AutoDeployFailure), postgres.JSONColumn(&b.Timings), postgres.JSONColumn(&b.PreviewValidation), postgres.JSONColumn(&b.Cancellation), &b.CreatedAt, &b.UpdatedAt}
}

func (d *DeploymentRow) destinations() []any {
	return []any{&d.ProjectID, &d.DeploymentID, &d.ReleaseID, &d.BuildRevisionID, &d.DeploymentStatus, &d.DeploymentURL,
		&d.ConfigurationRevision, &d.Revision, postgres.JSONColumn(&d.ProjectDeploymentLease), postgres.JSONColumn(&d.PinnedInputs), postgres.JSONColumn(&d.WorkloadGrant), postgres.JSONColumn(&d.ReadinessResult),
		postgres.JSONColumn(&d.DeploymentResources), postgres.JSONColumn(&d.ProviderOperationRefs), postgres.JSONColumn(&d.PhaseTimings), postgres.JSONColumn(&d.Failure), postgres.JSONColumn(&d.StartIdentity), postgres.JSONColumn(&d.Cancellation), &d.CreatedAt, &d.UpdatedAt, postgres.JSONColumn(&d.ReleaseVersion)}
}

// scanRelease, scanBuildRevision and scanDeployment read the columns of one
// row with scan, the Scan of a result set.
func scanRelease(scan func(...any) error) (ReleaseRow, error) {
	var r ReleaseRow
	err := scan(r.destinations()...)
	return r, err
}

func scanBuildRevision(scan func(...any) error) (BuildRevisionRow, error) {
	var b BuildRevisionRow
	err := scan(b.destinations()...)
	return b, err
}

func scanDeployment(scan func(...any) error) (DeploymentRow, error) {
	var d DeploymentRow
	err := scan(d.destinations()...)
	return d, err
}
