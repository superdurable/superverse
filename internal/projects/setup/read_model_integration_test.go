// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package setup_test

import (
	"maps"
	"testing"

	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/projects/setup"
)

// TestProjectReadModelTableIntegration checks the migrated projects table
// against the two Flows whose stores write its rows: a column of the right
// type for every Attribute that ProjectFlow or ProjectSetupFlow syncs, the
// row key's column as primary key, and no other column. No Attribute of the
// two maps to the same column. ProjectFlow's Members map has the child table
// project_members, with a column for every field of a member.
func TestProjectReadModelTableIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	project, projectMaps := postgrestest.SyncedColumnsForTest((&projects.ProjectFlow{}).GetPersistenceSchema().Attributes)
	columns, setupMaps := postgrestest.SyncedColumnsForTest((&setup.ProjectSetupFlow{}).GetPersistenceSchema().Attributes)
	if len(projectMaps) != 1 || projectMaps["Members"] == nil || len(setupMaps) != 0 {
		t.Fatalf("ProjectFlow and ProjectSetupFlow sync the AttributeMaps %v and %v; their stores declare only ProjectFlow's Members", projectMaps, setupMaps)
	}
	for name := range project {
		if _, shared := columns[name]; shared {
			t.Fatalf("ProjectFlow and ProjectSetupFlow both sync the column %s, so they overwrite each other", name)
		}
	}
	maps.Copy(columns, project)
	postgrestest.RequireReadModelTableForTest(t, db, postgrestest.ReadModelTableForTest{
		Name: "projects", Key: []string{"project_id"}, Values: columns,
	})
	postgrestest.RequireReadModelTableForTest(t, db, postgrestest.ReadModelTableForTest{
		Name: "project_members", Key: []string{"project_id", "account_id"}, Values: postgrestest.FieldColumnsForTest(projectMaps["Members"]),
	})
}
