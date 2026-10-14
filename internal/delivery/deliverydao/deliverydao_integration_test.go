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
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/infra"
)

// The DAO never interprets a status; these stand for any two.
const listed, other = "LISTED", "OTHER"

// TestDeliveryDAOIntegration runs the Release, build revision and Deployment
// queries against a migrated scratch database whose rows the test writes as
// Dex would.
func TestDeliveryDAOIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	dao := New(db)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	created := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	for index, r := range []struct{ project, id, status string }{{"a001", "r1", listed}, {"a001", "r2", other}, {"a001", "r3", listed}, {"b001", "r4", listed}} {
		exec(`INSERT INTO releases (project_id, release_id, release_status, revision, source_commit, prepared_source, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'abc', '{"Status": "READY"}', $5, $5)`, r.project, r.id, r.status, index+1, created.Add(time.Duration(index)*time.Minute))
	}
	for index, id := range []string{"build-2", "build-1"} {
		exec(`INSERT INTO release_build_revisions (project_id, release_id, build_revision_id, status, revision, image_digest, auto_deploy,
			provider_settled, configuration, artifacts, created_at, updated_at)
			VALUES ('a001', 'r1', $1, $2, 1, 'sha256:x', false, true, '{"ConfigurationRevision": 5}', '{"Manifest": {"Key": "m"}}', $3, $3)`,
			id, listed, created.Add(-time.Duration(index)*time.Minute))
	}
	for index, d := range []struct{ project, id, status string }{{"a001", "d1", listed}, {"a001", "d2", other}, {"a001", "d3", listed}} {
		exec(`INSERT INTO deployments (project_id, deployment_id, release_id, build_revision_id, deployment_status, configuration_revision,
			pinned_inputs, phase_timings, revision, created_at, updated_at)
			VALUES ($1, $2, 'r1', 'build-1', $3, 5, '{"ImageDigest": "sha256:x"}', '[{"Phase": "prepare"}]', 2, $4, $4)`,
			d.project, d.id, d.status, created.Add(time.Duration(index)*time.Minute))
	}

	r, err := dao.Release(ctx, "a001", "r1")
	if err != nil || r.ReleaseStatus.V != listed || r.Revision.V != 1 || r.SourceCommit.V != "abc" || r.Failure != nil || !r.CreatedAt.V.Equal(created) {
		t.Fatalf("release r1: %+v: %v", r, err)
	}
	if _, err = dao.Release(ctx, "a001", "r4"); !errors.Is(err, postgres.ErrNotFound) {
		t.Fatalf("another Project's release: %v", err)
	}
	for _, c := range []struct {
		statuses []string
		want     []string
	}{{nil, []string{"r3", "r2", "r1"}}, {[]string{listed}, []string{"r3", "r1"}}, {[]string{"NONE"}, nil}} {
		if ids := pages(t, func(after *postgres.PageKey) ([]ReleaseRow, *postgres.PageKey, error) {
			return dao.ListReleases(ctx, "a001", c.statuses, after, 1)
		}, func(r ReleaseRow) string { return r.ReleaseID }); !slices.Equal(ids, c.want) {
			t.Fatalf("the releases of a001 in %v are %v, want %v", c.statuses, ids, c.want)
		}
	}

	builds, err := dao.BuildRevisions(ctx, "a001", []string{"build-1", "build-9"})
	if err != nil || len(builds) != 1 || builds[0].BuildRevisionID != "build-1" || builds[0].ReleaseID != "r1" ||
		builds[0].AutoDeploy != (sql.Null[bool]{V: false, Valid: true}) || builds[0].ProviderSettled != (sql.Null[bool]{V: true, Valid: true}) ||
		string(builds[0].Configuration) != `{"ConfigurationRevision": 5}` || builds[0].ActorAccountID.Valid {
		t.Fatalf("build-1 of a001, without the absent build-9: %+v: %v", builds, err)
	}

	d, err := dao.Deployment(ctx, "a001", "d1")
	if err != nil || d.DeploymentStatus.V != listed || d.ConfigurationRevision.V != 5 || d.BuildRevisionID.V != "build-1" || string(d.PhaseTimings) != `[{"Phase": "prepare"}]` {
		t.Fatalf("deployment d1: %+v: %v", d, err)
	}
	if _, err = dao.Deployment(ctx, "a001", "d9"); !errors.Is(err, postgres.ErrNotFound) {
		t.Fatalf("an absent deployment: %v", err)
	}
	if ids := pages(t, func(after *postgres.PageKey) ([]DeploymentRow, *postgres.PageKey, error) {
		return dao.ListDeployments(ctx, "a001", []string{listed}, after, 1)
	}, func(d DeploymentRow) string { return d.DeploymentID }); !slices.Equal(ids, []string{"d3", "d1"}) {
		t.Fatalf("the LISTED deployments of a001 are %v, want d3 and d1", ids)
	}
}

// TestLiveTargetDAOIntegration selects Live targets from rows the test writes
// as Dex would, the Live pointer being a marshaled projects.LivePointer, the
// value ProjectFlow writes: the Deployment, Release and build revision the
// pointer names select the target, and a pointer that disagrees with the rows
// in any of them, or a Deployment that is not serving, selects none.
func TestLiveTargetDAOIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	dao := New(db)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	// The DAO never interprets a status; serving stands for the serving one.
	const serving = "SERVING"
	storage := infra.StorageRefs{Boundary: infra.ResourceRef{Name: "project-c001"}, StorageBucket: "bucket"}
	for _, live := range []struct {
		project          string
		pointer          *projects.LivePointer
		deploymentStatus string
		found            bool
	}{
		{"c001", &projects.LivePointer{DeploymentID: "d1", ReleaseID: "r1", BuildRevisionID: "build-1", Revision: 2, Ready: true}, serving, true},
		{"c002", nil, serving, false},
		{"c003", &projects.LivePointer{DeploymentID: "d9", ReleaseID: "r1", BuildRevisionID: "build-1"}, serving, false},
		{"c004", &projects.LivePointer{DeploymentID: "d1", ReleaseID: "r1", BuildRevisionID: "build-1"}, other, false},
		{"c005", &projects.LivePointer{DeploymentID: "d1", ReleaseID: "r2", BuildRevisionID: "build-1"}, serving, false},
		{"c006", &projects.LivePointer{DeploymentID: "d1", ReleaseID: "r1", BuildRevisionID: "build-2"}, serving, false},
	} {
		var pointer, refs []byte
		if live.pointer != nil {
			pointer = comm.Must(json.Marshal(live.pointer))
		}
		refs = comm.Must(json.Marshal(storage))
		if _, err := db.ExecContext(ctx, `INSERT INTO projects (project_id, project_status, current_live, project_storage) VALUES ($1, 'READY', $2, $3)`,
			live.project, pointer, refs); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO deployments (project_id, deployment_id, release_id, build_revision_id, deployment_status)
			VALUES ($1, 'd1', 'r1', 'build-1', $2)`, live.project, live.deploymentStatus); err != nil {
			t.Fatal(err)
		}
		target, err := dao.LiveTarget(ctx, live.project, serving)
		if !live.found {
			if !errors.Is(err, postgres.ErrNotFound) {
				t.Fatalf("Project %s's pointer %+v selected a Live target: %+v: %v", live.project, live.pointer, target, err)
			}
			continue
		}
		if err != nil || target.Deployment.DeploymentID != "d1" || target.Deployment.ReleaseID.V != "r1" || target.Deployment.BuildRevisionID.V != "build-1" ||
			target.ProjectStatus.V != "READY" || target.LiveExecution != nil {
			t.Fatalf("the Live target of %s: %+v: %v", live.project, target, err)
		}
		var decoded projects.LivePointer
		var decodedStorage infra.StorageRefs
		if err = json.Unmarshal(target.CurrentLive, &decoded); err != nil || decoded != *live.pointer {
			t.Fatalf("the Live pointer decodes as %+v: %v", decoded, err)
		}
		if err = json.Unmarshal(target.ProjectStorage, &decodedStorage); err != nil || decodedStorage != storage {
			t.Fatalf("the Live target's storage decodes as %+v: %v", decodedStorage, err)
		}
	}
}

// pages reads every page of a listing and returns the IDs in order; every
// page but the last is full.
func pages[T any](t *testing.T, list func(*postgres.PageKey) ([]T, *postgres.PageKey, error), id func(T) string) []string {
	t.Helper()
	var ids []string
	var after *postgres.PageKey
	for {
		page, next, err := list(after)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range page {
			ids = append(ids, id(row))
		}
		if next == nil {
			return ids
		}
		if len(page) != 1 {
			t.Fatalf("a page of %d rows names a next page", len(page))
		}
		after = next
	}
}

// TestLiveEventsDAOIntegration lists the Live history from rows the test writes
// as Dex would: a build with automatic deployment is one published entry
// joined with its deployment, a deployment no build started is one entry with
// the actor of its start identity, a build without automatic deployment and
// another Project's rows are not listed, and an entry names the creation time
// of the Release it replaced, which decides a rollback.
func TestLiveEventsDAOIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	dao := New(db)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	base := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return base.Add(time.Duration(minutes) * time.Minute) }
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, r := range []struct {
		project, id string
		created     time.Time
	}{{"e001", "old", at(0)}, {"e001", "new", at(60)}, {"e002", "other", at(0)}} {
		exec(`INSERT INTO releases (project_id, release_id, release_status, revision, source_commit, prepared_source, created_at, updated_at)
			VALUES ($1, $2, 'READY', 1, $2, '{"Version": {"Number": 2, "Subject": "Second"}}', $3, $3)`, r.project, r.id, r.created)
	}
	for _, b := range []struct {
		project, id, release, deployment string
		auto                             bool
		created                          time.Time
	}{{"e001", "build-old", "old", "d-old", true, at(1)}, {"e001", "build-new", "new", "d-new", true, at(10)}, {"e001", "build-manual", "new", "", false, at(11)}, {"e002", "build-other", "other", "d-other", true, at(1)}} {
		exec(`INSERT INTO release_build_revisions (project_id, release_id, build_revision_id, actor_account_id, status, revision, auto_deploy, auto_deployment_id, provider_settled, created_at, updated_at)
			VALUES ($1, $2, $3, 'builder', 'READY', 1, $4, NULLIF($5, ''), true, $6, $6)`, b.project, b.release, b.id, b.auto, b.deployment, b.created)
	}
	for _, d := range []struct {
		project, id, release, build string
		identity                    any
		created                     time.Time
	}{
		{"e001", "d-old", "old", "build-old", nil, at(2)}, {"e001", "d-new", "new", "build-new", nil, at(12)},
		{"e001", "d-back", "old", "build-old", `{"ActorAccountID": "operator"}`, at(20)}, {"e001", "d-forward", "new", "build-new", `{"ActorAccountID": "owner"}`, at(30)},
		{"e002", "d-other", "other", "build-other", nil, at(2)},
	} {
		exec(`INSERT INTO deployments (project_id, deployment_id, release_id, build_revision_id, deployment_status, configuration_revision, start_identity, revision, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'READY', 5, $5::jsonb, 1, $6, $6)`, d.project, d.id, d.release, d.build, d.identity, d.created)
	}

	events := func(limit int) []LiveEventRow {
		var all []LiveEventRow
		pages(t, func(after *postgres.PageKey) ([]LiveEventRow, *postgres.PageKey, error) {
			page, next, err := dao.ListLiveEvents(ctx, "e001", after, limit)
			all = append(all, page...)
			return page, next, err
		}, func(e LiveEventRow) string { return e.EventID })
		return all
	}
	all := events(1)
	var ids []string
	for _, e := range all {
		ids = append(ids, e.EventID)
	}
	if want := []string{"deploy:d-forward", "deploy:d-back", "build:build-new", "build:build-old"}; !slices.Equal(ids, want) {
		t.Fatalf("the Live events of e001 are %v, want %v", ids, want)
	}
	if got := events(10); len(got) != 4 {
		t.Fatalf("one page of ten lists %d events, want 4", len(got))
	}
	forward, back, published := all[0], all[1], all[2]
	if forward.Published || string(forward.StartIdentity) != `{"ActorAccountID": "owner"}` || forward.ReleaseID != "new" || !forward.ReleaseCreatedAt.V.Equal(at(60)) ||
		!forward.ReplacedReleaseCreatedAt.V.Equal(at(0)) || !forward.ReleaseCreatedAt.V.After(forward.ReplacedReleaseCreatedAt.V) {
		t.Fatalf("the deployment of the newer Release replaces the older: %+v", forward)
	}
	if !back.ReleaseCreatedAt.V.Equal(at(0)) || !back.ReplacedReleaseCreatedAt.V.Equal(at(60)) || !back.ReleaseCreatedAt.V.Before(back.ReplacedReleaseCreatedAt.V) {
		t.Fatalf("the deployment of the older Release replaces the newer, a rollback: %+v", back)
	}
	if !published.Published || published.ActorAccountID.V != "builder" || published.DeploymentID.V != "d-new" || published.DeploymentStatus.V != "READY" ||
		published.BuildStatus.V != "READY" || string(published.Version) != `{"Number": 2, "Subject": "Second"}` || !published.OccurredAt.Equal(at(10)) {
		t.Fatalf("the published entry joins its build and automatic deployment: %+v", published)
	}
}
