// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package deployments_test

import (
	"context"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

// TestDeploymentReadModelTableIntegration checks the migrated deployments
// table against ProjectDeployFlow: a column of the right type for every
// synced Attribute, the row key's columns as primary key, and no other column.
func TestDeploymentReadModelTableIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	columns, attributeMaps := postgrestest.SyncedColumnsForTest((&deployments.ProjectDeployFlow{}).GetPersistenceSchema().Attributes)
	if len(attributeMaps) != 0 {
		t.Fatalf("ProjectDeployFlow syncs the AttributeMaps %v, which the deployments store does not declare", attributeMaps)
	}
	postgrestest.RequireReadModelTableForTest(t, db, postgrestest.ReadModelTableForTest{
		Name: "deployments", Key: []string{"project_id", "deployment_id"}, Values: columns,
	})
}

// TestRetainedDeploymentSyncDexIntegration compares the deployments rows of a
// Project of a completed journey with the retained Flows. The Flows come first:
// Dex search lists every ProjectDeployFlow the journey left, each has its row,
// no row lacks a Flow, and each row equals the state that its Flow reads. An
// operator supplies the Project, as for the retained reads.
func TestRetainedDeploymentSyncDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectID := dexflowtest.Setting(t, "SUPERVERSE_TEST_RETAINED_RELEASE_PROJECT_ID")
	projectFlow, deploymentFlow := &projects.ProjectFlow{}, &deployments.ProjectDeployFlow{}
	client := dexflowtest.ClientForTest(t, address, nil, projectFlow, deploymentFlow)
	deploymentClient := deployments.NewProjectDeployFlowClient(&projects.ProjectFlowClient{Dex: client, Flow: projectFlow, ProjectIDLength: len(projectID), PlatformAdminEmails: []string{"admin@superverse.test"}}, nil, nil)
	db := postgrestest.OpenForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	flowType := dex.GetFinalFlowType(deploymentFlow)
	deploymentIDs := dexflowtest.ProjectChildIDsForTest(ctx, t, client, flowType, "deploy", projectID)
	if len(deploymentIDs) == 0 {
		t.Fatalf("Dex search lists no %s of Project %s", flowType, projectID)
	}
	postgrestest.AwaitSameIDsForTest(ctx, t, "Deployment",
		func() []string {
			return dexflowtest.ProjectChildIDsForTest(ctx, t, client, flowType, "deploy", projectID)
		},
		func() []string {
			return postgrestest.StringsForTest(ctx, t, db, `SELECT deployment_id FROM deployments WHERE project_id = $1 ORDER BY deployment_id`, projectID)
		})
	for _, deploymentID := range deploymentIDs {
		postgrestest.AwaitRowForTest(ctx, t, db, "deployments", func() map[string]any {
			t.Helper()
			snapshot, err := deploymentClient.ReadSnapshotForTest(ctx, projectID, deploymentID)
			if err != nil {
				t.Fatalf("read Deployment %s: %s", deploymentID, dexflowtest.ErrorCategory(err))
			}
			return map[string]any{
				"project_id": snapshot.ProjectID, "deployment_id": snapshot.DeploymentID, "release_id": snapshot.ReleaseID, "build_revision_id": snapshot.BuildRevisionID,
				"deployment_status": snapshot.Status, "configuration_revision": snapshot.ConfigurationRevision, "project_deployment_lease": snapshot.Lease,
				"deployment_url": snapshot.URL, "pinned_inputs": snapshot.Inputs, "workload_grant": snapshot.Grant, "readiness_result": snapshot.Readiness,
				"deployment_resources": snapshot.Resources, "provider_operation_refs": snapshot.Providers, "phase_timings": snapshot.Timings, "failure": snapshot.Failure,
				"start_identity": snapshot.Identity, "cancellation": snapshot.Cancellation, "revision": snapshot.Revision, "created_at": snapshot.CreatedAt, "updated_at": snapshot.UpdatedAt,
			}
		}, "project_id = $1 AND deployment_id = $2", projectID, deploymentID)
	}
	t.Logf("Project %s: %d Deployment rows equal their Flows, with no Flow lacking a row", projectID, len(deploymentIDs))
}
