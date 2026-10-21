// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects_test

import (
	"context"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/projects/setup"
)

// TestProjectReadModelSyncDexIntegration creates a throwaway Project through
// the actual Setup and compares its projects row, which platform Dex's
// Attribute Store writes from ProjectFlow and from ProjectSetupFlow, with the
// state of both Flows: the key column is the Project's ID, both stores' columns
// are present and neither store overwrote the other's, and the row follows the
// Project to DELETED, where it stays.
func TestProjectReadModelSyncDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	projectFlow, setupFlow := &projects.ProjectFlow{}, &setup.ProjectSetupFlow{}
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow, setupFlow)
	db := postgrestest.OpenForTest(t)
	projectClient := &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmails: []string{"admin@superverse.test"}}
	setupClient := &setup.ProjectSetupFlowClient{Dex: sdk, Flow: setupFlow}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	project := projecttest.CreateForTest(ctx, t, projectClient, t.Name())

	// The row is the latest state of each Flow, so every round reads both again.
	columns := func() map[string]any {
		t.Helper()
		current, err := projectClient.Read(ctx, project.ProjectID)
		if err != nil {
			t.Fatalf("read Project %s: %s", project.ProjectID, dexflowtest.ErrorCategory(err))
		}
		started, err := setupClient.Read(ctx, current.SetupFlowID)
		if err != nil {
			t.Fatalf("read the Setup of Project %s: %s", project.ProjectID, dexflowtest.ErrorCategory(err))
		}
		return projectColumns(current, started)
	}
	postgrestest.AwaitRowForTest(ctx, t, db, "projects", columns, "project_id = $1", project.ProjectID)
	t.Logf("Project %s READY: its row equals ProjectFlow %s and its Setup %s", project.ProjectID, project.ProjectID, project.SetupFlowID)

	deleted := projecttest.DeleteForTest(ctx, t, projectClient, project.ProjectID)
	postgrestest.AwaitRowForTest(ctx, t, db, "projects", columns, "project_id = $1", project.ProjectID)
	t.Logf("Project %s DELETED at revision %d: its row stays and equals the Flows", project.ProjectID, deleted.Revision)
}

// projectColumns are the columns of the projects row that ProjectFlow's store
// and ProjectSetupFlow's store write for a Project and its Setup. The
// AppManifest column is not compared: it holds a protobuf message that Dex
// writes in its own JSON form.
func projectColumns(p projects.Project, s setup.Setup) map[string]any {
	return map[string]any{
		"project_id": p.ProjectID, "team_space_id": p.TeamSpaceID, "creator_account_id": p.CreatorAccountID, "name": p.Name, "project_status": p.ProjectStatus,
		"revision": p.Revision, "template_revision": p.TemplateRevision, "setup_flow_id": p.SetupFlowID,
		"repository_binding": p.RepositoryBinding, "active_deployment": p.ActiveDeployment, "current_live": p.CurrentLive,
		"active_command": p.ActiveCommand, "last_failure": p.LastFailure, "session_start_failure": p.SessionStartFailure,
		"project_storage": p.Storage, "live_execution": p.LiveExecution, "created_at": p.CreatedAt, "updated_at": p.UpdatedAt,
		"setup_status": s.Status, "setup_phase": s.Phase, "setup_source_commit": s.SourceCommit, "setup_failure": s.Failure, "setup_revision": s.Revision,
	}
}
