// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package releases_test

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/releases"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

// TestReleaseReadModelTablesIntegration checks the migrated releases and
// release_build_revisions tables against ProjectReleaseFlow: a column of the
// right type for every synced Attribute and ReleaseBuildRevisions field, the
// row key's columns as primary keys, and no other column.
func TestReleaseReadModelTablesIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	columns, attributeMaps := postgrestest.SyncedColumnsForTest((&releases.ProjectReleaseFlow{}).GetPersistenceSchema().Attributes)
	if names := slices.Collect(maps.Keys(attributeMaps)); !slices.Equal(names, []string{releases.ReleaseBuildRevisions.AttributeName()}) {
		t.Fatalf("ProjectReleaseFlow syncs the AttributeMaps %v; the releases store declares only ReleaseBuildRevisions", names)
	}
	postgrestest.RequireReadModelTableForTest(t, db, postgrestest.ReadModelTableForTest{
		Name: "releases", Key: []string{"project_id", "release_id"}, Values: columns,
	})
	postgrestest.RequireReadModelTableForTest(t, db, postgrestest.ReadModelTableForTest{
		Name: "release_build_revisions", Key: []string{"project_id", "release_id", "build_revision_id"},
		Values: postgrestest.FieldColumnsForTest(reflect.TypeFor[releases.ReleaseBuildRevision]()),
		// Recovery and recipe details that no read needs stay in the Flow.
		Skipped: []string{"fingerprint", "build_profile_digest", "operation", "expected_live_revision"},
	})
}

// TestRetainedReleaseSyncDexIntegration compares the read model of a Project
// of a completed journey with the retained Flows. The Flows come first: Dex
// search lists every ProjectReleaseFlow the journey left, and each has its
// releases row, no row lacks a Flow, and each Release's
// release_build_revisions rows are exactly the instances of its
// ReleaseBuildRevisions map. The Project's own row equals ProjectFlow, and every
// Release and build row equals its Flow's state. An operator supplies the
// Project, as for the retained reads.
func TestRetainedReleaseSyncDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_RELEASE_PROJECT_ID")
	projectFlow, releaseFlow := &projects.ProjectFlow{}, &releases.ProjectReleaseFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, projectFlow, releaseFlow)
	projectClient := &projects.ProjectFlowClient{Dex: client, Flow: projectFlow, ProjectIDLength: len(projectID), PlatformAdminEmails: []string{"admin@superverse.test"}}
	releaseClient := releases.NewProjectReleaseFlowClient(releaseFlow, projectClient, nil)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	db := postgrestest.OpenForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()

	postgrestest.AwaitRowForTest(ctx, t, db, "projects", func() map[string]any {
		t.Helper()
		project, err := projectClient.Read(ctx, projectID)
		if err != nil {
			t.Fatalf("read Project %s: %s", projectID, dexflowtest.ErrorCategory(err))
		}
		return map[string]any{"project_id": project.ProjectID, "project_status": project.ProjectStatus, "revision": project.Revision}
	}, "project_id = $1", projectID)

	flowType := dex.GetFinalFlowType(releaseFlow)
	releaseIDs := dexflowtest.ProjectChildIDsForTest(ctx, t, client, flowType, "release", projectID)
	if len(releaseIDs) == 0 {
		t.Fatalf("Dex search lists no %s of Project %s", flowType, projectID)
	}
	postgrestest.AwaitSameIDsForTest(ctx, t, "Release",
		func() []string {
			return dexflowtest.ProjectChildIDsForTest(ctx, t, client, flowType, "release", projectID)
		},
		func() []string {
			return postgrestest.StringsForTest(ctx, t, db, `SELECT release_id FROM releases WHERE project_id = $1 ORDER BY release_id`, projectID)
		})
	builds := 0
	for _, releaseID := range releaseIDs {
		flowID, err := releases.FlowID(projectID, releaseID)
		if err != nil {
			t.Fatal(err)
		}
		postgrestest.AwaitRowForTest(ctx, t, db, "releases", func() map[string]any {
			t.Helper()
			snapshot, err := releaseClient.ReadSnapshot(ctx, projectID, releaseID)
			if err != nil {
				t.Fatalf("read Release %s: %s", releaseID, dexflowtest.ErrorCategory(err))
			}
			return map[string]any{
				"project_id": snapshot.ProjectID, "release_id": snapshot.ReleaseID, "release_status": snapshot.Status, "revision": snapshot.Revision,
				"source_commit": snapshot.SourceCommit, "build_profile_version": snapshot.BuildProfileVersion, "prepared_source": snapshot.PreparedSource,
				"active_build_revision_id": snapshot.ActiveBuildRevisionID, "ready_build_revision_id": snapshot.ReadyBuildRevisionID,
				"failure": snapshot.Failure, "created_at": snapshot.CreatedAt, "updated_at": snapshot.UpdatedAt,
			}
		}, "project_id = $1 AND release_id = $2", projectID, releaseID)
		buildIDs := dexflowtest.MapInstanceKeysForTest(ctx, t, reader, flowID, releases.ReleaseBuildRevisions.AttributeName())
		postgrestest.AwaitSameIDsForTest(ctx, t, "build revision of Release "+releaseID,
			func() []string {
				return dexflowtest.MapInstanceKeysForTest(ctx, t, reader, flowID, releases.ReleaseBuildRevisions.AttributeName())
			},
			func() []string {
				return postgrestest.StringsForTest(ctx, t, db, `SELECT build_revision_id FROM release_build_revisions
					WHERE project_id = $1 AND release_id = $2 ORDER BY build_revision_id`, projectID, releaseID)
			})
		for _, buildID := range buildIDs {
			postgrestest.AwaitRowForTest(ctx, t, db, "release_build_revisions", func() map[string]any {
				t.Helper()
				build, err := releaseClient.ReadBuildRevision(ctx, projectID, releaseID, buildID)
				if err != nil {
					t.Fatalf("read build %s of Release %s: %s", buildID, releaseID, dexflowtest.ErrorCategory(err))
				}
				return map[string]any{
					"project_id": projectID, "release_id": releaseID, "build_revision_id": build.BuildRevisionID, "actor_account_id": build.ActorAccountID,
					"configuration_revision": build.ConfigurationRevision, "configuration": build.Configuration, "build_profile_version": build.BuildProfileVersion, "status": build.Status,
					"revision": build.Revision, "image_reference": build.ImageReference, "image_digest": build.ImageDigest, "provenance": build.Provenance,
					"artifacts": build.Artifacts, "failure": build.Failure, "provider_settled": build.ProviderSettled, "auto_deploy": build.AutoDeploy, "auto_deployment_id": build.AutoDeploymentID,
					"auto_deploy_status": build.AutoDeployStatus, "auto_deploy_failure": build.AutoDeployFailure, "timings": build.Timings,
					"cancellation": build.Cancellation,
					"created_at":   build.CreatedAt, "updated_at": build.UpdatedAt,
				}
			}, "project_id = $1 AND release_id = $2 AND build_revision_id = $3", projectID, releaseID, buildID)
		}
		builds += len(buildIDs)
	}
	t.Logf("Project %s: its row, %d Releases and %d build rows equal their Flows, with no Flow or build instance lacking a row", projectID, len(releaseIDs), builds)
}
