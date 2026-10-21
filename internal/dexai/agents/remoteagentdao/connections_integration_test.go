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
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagentdao"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
)

// These tests call the real lane PostgreSQL. Owner-role fixture writes model
// Dex projections in a scratch database; no application dependency is replaced.
func TestRemoteConnectionsCapAndRedemptionIntegration(t *testing.T) {
	db, dao := connectionDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	for _, initial := range []int{0, 5} {
		project := fmt.Sprintf("cap-%d", initial)
		createProjectRow(t, db, project)
		for i := 0; i < initial; i++ {
			if err := createPairing(ctx, dao, project, i); err != nil {
				t.Fatal(err)
			}
		}
		var group sync.WaitGroup
		results := make(chan error, 16)
		for i := initial; i < initial+16; i++ {
			group.Go(func() { results <- createPairing(ctx, dao, project, i) })
		}
		group.Wait()
		close(results)
		succeeded, full := 0, 0
		for err := range results {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, remoteagentdao.ErrConnectionsFull):
				full++
			default:
				t.Fatal(err)
			}
		}
		if succeeded != 6-initial || full != 16-(6-initial) {
			t.Fatalf("concurrent cap from %d: success=%d full=%d", initial, succeeded, full)
		}
		rows, err := dao.List(ctx, project)
		if err != nil || len(rows) != 6 {
			t.Fatalf("cap list count=%d: %v", len(rows), err)
		}
		for _, row := range rows {
			if row.TokenHash.Valid || row.PairingCodeHash.Valid {
				t.Fatal("public list exposed a credential hash")
			}
		}
	}

	createProjectRow(t, db, "redeem")
	if err := createPairing(ctx, dao, "redeem", 0); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Go(func() {
			_, err := dao.Redeem(ctx, hash("redeem-code-0"), hash(fmt.Sprintf("token-%d", i)), []string{"admin@superverse.test"}, accountdao.StatusActive, authorize)
			results <- err
		})
	}
	group.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, postgres.ErrNotFound) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("single-use redemption had %d winners", winners)
	}
	var code sql.Null[string]
	var token sql.Null[string]
	var basis int64
	if err := db.QueryRowContext(ctx, `SELECT pairing_code_hash, token_hash, basis_epoch FROM remote_agent_connections WHERE project_id = 'redeem'`).Scan(&code, &token, &basis); err != nil || code.Valid || !token.Valid || basis != 7 {
		t.Fatalf("redemption did not preserve immutable basis and consume code: %v", err)
	}
	if access, err := dao.Authenticate(ctx, token.V, []string{"admin@superverse.test"}, accountdao.StatusActive); err != nil || authorize(access) != nil {
		t.Fatalf("redeemed credential authentication: %v", err)
	}
}

func TestRemoteConnectionsCleanupAndInvalidationIntegration(t *testing.T) {
	db, dao := connectionDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	createProjectRow(t, db, "cleanup")
	for i := 0; i < 6; i++ {
		if err := createPairing(ctx, dao, "cleanup", i); err != nil {
			t.Fatal(err)
		}
	}
	if err := dao.Revoke(ctx, "cleanup", "cleanup-0", remoteagentdao.RevokeRemoved); err != nil {
		t.Fatal(err)
	}
	execute(t, db, `UPDATE remote_agent_connections SET pairing_expires_at = now() - interval '1 second' WHERE connection_id = 'cleanup-1'`)
	if _, err := dao.Redeem(ctx, hash("cleanup-code-1"), hash("expired-token"), []string{"admin@superverse.test"}, accountdao.StatusActive, authorize); !errors.Is(err, postgres.ErrNotFound) {
		t.Fatalf("expired pairing redeemed: %v", err)
	}
	if err := createPairing(ctx, dao, "cleanup", 6); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM remote_agent_connections WHERE project_id = 'cleanup'`).Scan(&count); err != nil || count != 5 {
		t.Fatalf("expired/revoked cleanup count=%d: %v", count, err)
	}
	if done, err := dao.DeleteProject(ctx, "cleanup", projects.StatusDeleting, projects.StatusDeleted); err != nil || done {
		t.Fatalf("stale READY projection acknowledged deletion: %v", err)
	}
	execute(t, db, `UPDATE projects SET project_status = 'DELETING' WHERE project_id = 'cleanup'`)
	if done, err := dao.DeleteProject(ctx, "cleanup", projects.StatusDeleting, projects.StatusDeleted); err != nil || !done {
		t.Fatalf("visible deleting Project did not clean credentials: %v", err)
	}
	if err := createPairing(ctx, dao, "cleanup", 7); !errors.Is(err, postgres.ErrNotFound) {
		t.Fatalf("delayed pairing reappeared after cleanup: %v", err)
	}
	if _, err := dao.Redeem(ctx, hash("cleanup-code-2"), hash("late"), []string{"admin@superverse.test"}, accountdao.StatusActive, authorize); !errors.Is(err, postgres.ErrNotFound) {
		t.Fatalf("redemption recreated deleted credential: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM remote_agent_connections WHERE project_id = 'cleanup'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("Project cleanup left %d credentials: %v", count, err)
	}

	for _, mutation := range []string{"logout", "expiry", "auth-epoch", "role", "membership", "revoked", "deleting", "deleted", "missing-project"} {
		t.Run(mutation, func(t *testing.T) {
			db, dao := connectionDatabase(t)
			createProjectRow(t, db, "auth")
			if err := createPairing(t.Context(), dao, "auth", 0); err != nil {
				t.Fatal(err)
			}
			if _, err := dao.Redeem(t.Context(), hash("auth-code-0"), hash("credential"), []string{"admin@superverse.test"}, accountdao.StatusActive, authorize); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "logout":
				execute(t, db, `DELETE FROM login_sessions WHERE session_id = 'login'`)
			case "expiry":
				execute(t, db, `UPDATE login_sessions SET expires_at = now() - interval '1 second'`)
			case "auth-epoch":
				execute(t, db, `UPDATE accounts SET auth_epoch = auth_epoch + 1`)
			case "role":
				execute(t, db, `UPDATE project_members SET role = 'OPERATOR'`)
			case "membership":
				execute(t, db, `DELETE FROM team_space_members`)
			case "revoked":
				if err := dao.Revoke(t.Context(), "auth", "auth-0", remoteagentdao.RevokeDisconnected); err != nil {
					t.Fatal(err)
				}
			case "deleting":
				execute(t, db, `UPDATE projects SET project_status = 'DELETING'`)
			case "missing-project":
				execute(t, db, `DELETE FROM projects`)
			case "deleted":
				execute(t, db, `UPDATE projects SET project_status = 'DELETED'`)
			}
			access, err := dao.Authenticate(t.Context(), hash("credential"), []string{"admin@superverse.test"}, accountdao.StatusActive)
			if err == nil {
				err = authorize(access)
			}
			if !errors.Is(err, postgres.ErrNotFound) && !errors.Is(err, projects.ErrForbidden) {
				t.Fatalf("credential survived %s or failed with an unrelated error: %v", mutation, err)
			}
		})
	}
}

func connectionDatabase(t *testing.T) (*sql.DB, *remoteagentdao.DAO) {
	t.Helper()
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	execute(t, db, `INSERT INTO accounts (account_id,canonical_email,status,password_record,credential_revision,auth_epoch,registration_fingerprint,created_at,updated_at)
		VALUES ('builder','builder@example.com','ACTIVE','{}',1,1,'registration',now(),now())`)
	execute(t, db, `INSERT INTO login_sessions (session_id,account_id,token_hash,auth_epoch,issued_at,expires_at)
		VALUES ('login','builder','login-hash',1,now(),now()+interval '1 day')`)
	execute(t, db, `INSERT INTO team_space_members (team_space_id,account_id,role) VALUES ('team','builder','MEMBER')`)
	return db, remoteagentdao.New(db)
}

func createProjectRow(t *testing.T, db *sql.DB, project string) {
	t.Helper()
	execute(t, db, `INSERT INTO projects (project_id,name,team_space_id,project_status) VALUES ($1,'Project','team','READY')`, project)
	execute(t, db, `INSERT INTO project_members (project_id,account_id,role) VALUES ($1,'builder','BUILDER')`, project)
}

func createPairing(ctx context.Context, dao *remoteagentdao.DAO, project string, n int) error {
	return dao.CreatePairing(ctx, remoteagentdao.Connection{ConnectionID: fmt.Sprintf("%s-%d", project, n),
		ProjectID: project, AccountID: "builder", LoginSessionID: "login", BasisEpoch: 7, CreatedAt: time.Now().UTC(),
		PairingCodeHash:  sql.Null[string]{V: hash(fmt.Sprintf("%s-code-%d", project, n)), Valid: true},
		PairingExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, []string{"admin@superverse.test"}, accountdao.StatusActive, authorize)
}

func authorize(access remoteagentdao.Access) error {
	if !access.ProjectStatus.Valid || access.ProjectStatus.V == projects.StatusDeleting || access.ProjectStatus.V == projects.StatusDeleted {
		return postgres.ErrNotFound
	}
	if !projects.CapabilityBuild.Allows(access.PlatformAdmin, access.ProjectRole.V, access.TeamSpaceRole.V) {
		return projects.ErrForbidden
	}
	return nil
}

func execute(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func hash(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])
}
