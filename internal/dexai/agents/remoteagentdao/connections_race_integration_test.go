// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagentdao_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagentdao"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
)

// Both contenders use real PostgreSQL locks. No substitute controls admission.
func TestRemoteConnectionsDeletionRaceIntegration(t *testing.T) {
	for _, operation := range []string{"create", "redeem"} {
		t.Run(operation, func(t *testing.T) {
			db, dao := connectionDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			createProjectRow(t, db, "race")
			if err := createPairing(ctx, dao, "race", 0); err != nil {
				t.Fatal(err)
			}
			barrier, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = barrier.Rollback() }()
			if operation == "create" {
				if _, err = barrier.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1380012097,hashtext('race'))`); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err = barrier.ExecContext(ctx, `SELECT connection_id FROM remote_agent_connections WHERE connection_id='race-0' FOR UPDATE`); err != nil {
					t.Fatal(err)
				}
			}
			contender := make(chan error, 1)
			go func() {
				if operation == "create" {
					contender <- createPairing(ctx, dao, "race", 1)
				} else {
					_, err := dao.Redeem(ctx, hash("race-code-0"), hash("race-token"), []string{"admin@superverse.test"}, accountdao.StatusActive, authorize)
					contender <- err
				}
			}()
			awaitBlockedDatabaseCall(ctx, t, db)
			execute(t, db, `UPDATE projects SET project_status='DELETING' WHERE project_id='race'`)
			cleanup := make(chan error, 1)
			go func() {
				done, err := dao.DeleteProject(ctx, "race", projects.StatusDeleting, projects.StatusDeleted)
				if err == nil && !done {
					err = errors.New("cleanup did not acknowledge visible deletion")
				}
				cleanup <- err
			}()
			if err = barrier.Commit(); err != nil {
				t.Fatal(err)
			}
			if err = <-contender; operation == "create" && !errors.Is(err, postgres.ErrNotFound) {
				t.Fatalf("delayed insert admitted after deletion: %v", err)
			} else if operation == "redeem" && err != nil && !errors.Is(err, postgres.ErrNotFound) {
				t.Fatalf("unexpected redemption race error: %v", err)
			}
			if err = <-cleanup; err != nil {
				t.Fatal(err)
			}
			var count int
			if err = db.QueryRowContext(ctx, `SELECT count(*) FROM remote_agent_connections WHERE project_id='race'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("cleanup left %d rows: %v", count, err)
			}
			if _, err = dao.Authenticate(ctx, hash("race-token"), []string{"admin@superverse.test"}, accountdao.StatusActive); !errors.Is(err, postgres.ErrNotFound) {
				t.Fatalf("post-cleanup token survived: %v", err)
			}
		})
	}
}

func awaitBlockedDatabaseCall(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		var blocked bool
		err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock')`).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("database contender did not reach its real lock")
		case <-timer.C:
		}
	}
}

func TestRemoteConnectionsPairedRetentionAndLivenessIntegration(t *testing.T) {
	db, dao := connectionDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	createProjectRow(t, db, "paired")
	for i := 0; i < 6; i++ {
		if err := createPairing(ctx, dao, "paired", i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := dao.Redeem(ctx, hash("paired-code-0"), hash("paired-token"), []string{"admin@superverse.test"}, accountdao.StatusActive, authorize); err != nil {
		t.Fatal(err)
	}
	// A real unique-token violation includes value-bearing server fields.
	// Safe metadata remains useful through wrappers without exposing them.
	_, collision := db.ExecContext(ctx, `UPDATE remote_agent_connections SET status='PAIRED',
		pairing_code_hash=NULL, token_hash=$1 WHERE connection_id='paired-1'`, hash("paired-token"))
	if collision == nil {
		t.Fatal("actual PostgreSQL accepted a duplicate token hash")
	}
	state, constraint := postgres.ErrorMetadata(fmt.Errorf("redeem credential: %w", collision))
	if state != "23505" || constraint != "remote_agent_connections_token_hash_key" {
		t.Fatal("safe PostgreSQL metadata lost the exact real constraint classification")
	}
	execute(t, db, `UPDATE remote_agent_connections SET pairing_expires_at=now()-interval '1 day' WHERE connection_id='paired-0'`)
	if err := createPairing(ctx, dao, "paired", 6); !errors.Is(err, remoteagentdao.ErrConnectionsFull) {
		t.Fatalf("paired row was evicted: %v", err)
	}
	var before, after time.Time
	if err := db.QueryRowContext(ctx, `SELECT last_seen_at FROM remote_agent_connections WHERE connection_id='paired-0'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := dao.Touch(ctx, "paired-0"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT last_seen_at FROM remote_agent_connections WHERE connection_id='paired-0'`).Scan(&after); err != nil || !after.Equal(before) {
		t.Fatalf("liveness wrote inside its 60 second budget: %v", err)
	}
	execute(t, db, `UPDATE remote_agent_connections SET last_seen_at=now()-interval '61 seconds' WHERE connection_id='paired-0'`)
	if err := dao.Touch(ctx, "paired-0"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT last_seen_at FROM remote_agent_connections WHERE connection_id='paired-0'`).Scan(&after); err != nil || after.Before(before) {
		t.Fatalf("liveness did not refresh after its budget: %v", err)
	}
	seen, err := dao.LastSeen(ctx, "paired", "paired-0")
	if err != nil || seen == nil || !seen.Equal(after) {
		t.Fatalf("scoped paired liveness differs from SQL: %v", err)
	}
	for _, key := range [][2]string{{"another-project", "paired-0"}, {"paired", "absent"}, {"paired", "paired-1"}} {
		if seen, err := dao.LastSeen(ctx, key[0], key[1]); err != nil || seen != nil {
			t.Fatalf("absent, wrong-project or PAIRING liveness was exposed: %v", err)
		}
	}
	execute(t, db, `UPDATE remote_agent_connections SET last_seen_at=NULL WHERE connection_id='paired-0'`)
	if seen, err := dao.LastSeen(ctx, "paired", "paired-0"); err != nil || seen != nil {
		t.Fatalf("null liveness was fabricated: %v", err)
	}
	if err := dao.Touch(ctx, "paired-0"); err != nil {
		t.Fatal(err)
	}
	failed, stop := context.WithCancel(ctx)
	stop()
	if _, err := dao.LastSeen(failed, "paired", "paired-0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("SQL query failure was hidden: %v", err)
	}
	// An ACTIVE platform administrator retains the real owning capability even
	// without either membership; a changed authentication epoch invalidates its issuing login.
	execute(t, db, `UPDATE accounts SET canonical_email=$1 WHERE account_id='builder'`, "second-admin@example.com")
	execute(t, db, `DELETE FROM project_members`)
	execute(t, db, `DELETE FROM team_space_members`)
	access, err := dao.Authenticate(ctx, hash("paired-token"), []string{"admin@superverse.test", "second-admin@example.com"}, accountdao.StatusActive)
	if err != nil || !access.PlatformAdmin || authorize(access) != nil {
		t.Fatalf("platform-admin rules were lost: %v", err)
	}
	if err := dao.Revoke(ctx, "paired", "paired-0", remoteagentdao.RevokeRemoved); err != nil {
		t.Fatal(err)
	}
	if seen, err := dao.LastSeen(ctx, "paired", "paired-0"); err != nil || seen != nil {
		t.Fatalf("revoked connection liveness was exposed: %v", err)
	}
	// Restore a separate real paired credential to verify administrator invalidation.
	if _, err := dao.Redeem(ctx, hash("paired-code-1"), hash("admin-token"), []string{"admin@superverse.test", "second-admin@example.com"}, accountdao.StatusActive, authorize); err != nil {
		t.Fatal(err)
	}
	// The current account schema permits ACTIVE only; do not invent another
	// persisted account status just to fabricate coverage.
	if _, err = db.ExecContext(ctx, `UPDATE accounts SET status='DISABLED' WHERE account_id='builder'`); err == nil {
		t.Fatal("account status schema accepted an undeclared value")
	}
	execute(t, db, `UPDATE accounts SET auth_epoch=auth_epoch+1 WHERE account_id='builder'`)
	if _, err = dao.Authenticate(ctx, hash("admin-token"), []string{"admin@superverse.test", "second-admin@example.com"}, accountdao.StatusActive); !errors.Is(err, postgres.ErrNotFound) {
		t.Fatalf("changed administrator auth epoch authenticated: %v", err)
	}
}
