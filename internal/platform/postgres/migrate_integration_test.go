// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
)

// readModelTables are the tables of migrations 0002 and 0003, which only Dex
// writes; childTables are those of them that hold AttributeMap instances.
var (
	readModelTables = []string{"projects", "releases", "release_build_revisions", "deployments", "team_spaces", "team_space_members", "project_members"}
	childTables     = []string{"release_build_revisions", "team_space_members", "project_members"}
)

// TestMigrateIntegration applies the embedded migrations to a scratch
// database of the lane's PostgreSQL: the first run applies every file of the
// migrations directory in name order, a second applies none, and a recorded
// migration whose file changed stops it.
func TestMigrateIntegration(t *testing.T) {
	db := postgrestest.ScratchDatabaseForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	files, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	var versions []string
	for _, file := range files {
		versions = append(versions, strings.TrimSuffix(file.Name(), ".sql"))
	}
	applied, err := postgres.Migrate(ctx, db)
	if err != nil || !slices.Equal(applied, versions) {
		t.Fatalf("the first run applied %v: %v", applied, err)
	}
	var tables int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name IN ('accounts', 'login_sessions')`).Scan(&tables); err != nil || tables != 2 {
		t.Fatalf("the migrated schema has %d of the account tables: %v", tables, err)
	}
	again, err := postgres.Migrate(ctx, db)
	if err != nil || len(again) != 0 {
		t.Fatalf("a second run applied %v: %v", again, err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE schema_migrations SET checksum = 'changed' WHERE version = '0001_accounts'`); err != nil {
		t.Fatal(err)
	}
	if changed, err := postgres.Migrate(ctx, db); err == nil {
		t.Fatalf("a changed migration was accepted; the run applied %v", changed)
	}
}

// TestReadModelMigrationIntegration checks the rules of the tables Dex
// writes: no constraint but the primary key, nullable non-key columns, Dex as
// their only writer and the server as a reader.
func TestReadModelMigrationIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT table_name || ' ' || constraint_type FROM information_schema.table_constraints
		WHERE table_schema = 'public' AND table_name = ANY($1) AND constraint_type <> 'PRIMARY KEY'
		AND constraint_name NOT LIKE '%_not_null'`, readModelTables)
	if err != nil {
		t.Fatal(err)
	}
	if constraints := scanStrings(t, rows); len(constraints) != 0 {
		t.Fatalf("read model tables have constraints that would fail a sync batch: %v", constraints)
	}
	rows, err = db.QueryContext(ctx, `SELECT c.table_name || '.' || c.column_name FROM information_schema.columns c
		WHERE c.table_schema = 'public' AND c.table_name = ANY($1) AND c.is_nullable = 'NO' AND NOT EXISTS (
			SELECT FROM information_schema.key_column_usage k JOIN information_schema.table_constraints p
				ON p.constraint_name = k.constraint_name AND p.table_schema = k.table_schema
			WHERE p.constraint_type = 'PRIMARY KEY' AND k.table_schema = c.table_schema AND k.table_name = c.table_name AND k.column_name = c.column_name)`,
		readModelTables)
	if err != nil {
		t.Fatal(err)
	}
	if required := scanStrings(t, rows); len(required) != 0 {
		t.Fatalf("read model columns outside the primary keys are NOT NULL: %v", required)
	}
	for _, table := range readModelTables {
		child := slices.Contains(childTables, table)
		for privilege, want := range map[string]bool{"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": child} {
			requirePrivilege(t, db, postgres.DexRoleForTest, table, privilege, want)
		}
		for privilege, want := range map[string]bool{"SELECT": true, "INSERT": false, "UPDATE": false, "DELETE": false} {
			requirePrivilege(t, db, postgres.ServerRole, table, privilege, want)
		}
	}
}

func requirePrivilege(t *testing.T, db *sql.DB, role, table, privilege string, want bool) {
	t.Helper()
	var granted bool
	if err := db.QueryRowContext(t.Context(), `SELECT has_table_privilege($1, $2, $3)`, role, "public."+table, privilege).Scan(&granted); err != nil {
		t.Fatalf("read %s's %s privilege on %s: %v", role, privilege, table, err)
	}
	if granted != want {
		t.Fatalf("%s has %s on %s: %t, want %t", role, privilege, table, granted, want)
	}
}

func scanStrings(t *testing.T, rows *sql.Rows) []string {
	t.Helper()
	defer func() { _ = rows.Close() }()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}
