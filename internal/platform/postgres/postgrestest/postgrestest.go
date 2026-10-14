// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package postgrestest

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/postgres"
)

// laneDatabase is the database the lane scripts create and migrate.
const laneDatabase = "superverse"

// superuser is the PostgreSQL superuser of a lane's container.
const superuser = "postgres"

// OpenForTest connects to the migrated database of the lane that
// SUPERVERSE_TEST_LANE names, as the server role, and closes the pool when the
// test ends. A lane without PostgreSQL fails the test.
func OpenForTest(t testing.TB) *sql.DB {
	t.Helper()
	return open(t, laneDatabase, postgres.ServerRole)
}

// ScratchDatabaseForTest creates an empty database that the owner role owns,
// connects to it as that role, and drops it when the test ends. Migration
// tests run there, never against the lane's database.
func ScratchDatabaseForTest(t testing.TB) *sql.DB {
	t.Helper()
	admin := open(t, superuser, superuser)
	name := "scratch_" + flowid.RandomID(12)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name+" OWNER "+postgres.OwnerRole); err != nil {
		t.Fatalf("create the scratch database: %v", err)
	}
	db := open(t, name, postgres.OwnerRole)
	// Cleanups run last in, first out: the scratch pool closes before the drop.
	t.Cleanup(func() {
		drop, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if _, err := admin.ExecContext(drop, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop the scratch database %s: %v", name, err)
		}
	})
	return db
}

func open(t testing.TB, database, role string) *sql.DB {
	t.Helper()
	address, credentialsFile := dexflowtest.LanePostgres(t)
	raw, err := os.ReadFile(credentialsFile) //nolint:gosec // the lane's own private credentials file
	if err != nil {
		t.Fatalf("the lane has no PostgreSQL credentials: %v", err)
	}
	var passwords map[string]string
	if err = json.Unmarshal(raw, &passwords); err != nil || passwords[role] == "" {
		t.Fatalf("the lane's PostgreSQL credentials have no password of %s", role)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("the lane's PostgreSQL address %q is not host:port", address)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	db, err := postgres.Open(ctx, postgres.Connection{
		Host: host, Port: mustPort(t, port), Database: database, User: role, Password: passwords[role], SSLMode: "disable", MaxConnections: 8,
	})
	if err != nil {
		t.Fatalf("connect to the lane's PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustPort(t testing.TB, port string) int {
	t.Helper()
	value, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("the lane's PostgreSQL port %q is not a number", port)
	}
	return value
}
