// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package accountdao

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
)

// TestAccountDAOIntegration runs the account queries against the lane's
// migrated PostgreSQL as the server role: registration conflicts, login
// admission, and the expiry, epoch and status conditions of authentication.
func TestAccountDAOIntegration(t *testing.T) {
	db := postgrestest.OpenForTest(t)
	dao := New(db, []string{"admin@superverse.test"})
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := flowid.RandomID(12)
	candidate := Account{
		AccountID: flowid.RandomID(32), CanonicalEmail: "dao-" + id + "@example.com", Status: StatusActive,
		PasswordRecord: json.RawMessage(`{"algorithm":"argon2id"}`), CredentialRevision: 1, AuthEpoch: 1,
		RegistrationFingerprint: "fingerprint-" + id, CreatedAt: now, UpdatedAt: now,
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if _, err := db.ExecContext(cleanup, `DELETE FROM login_sessions WHERE account_id = $1`, candidate.AccountID); err != nil {
			t.Errorf("delete the test logins: %v", err)
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM accounts WHERE account_id = $1`, candidate.AccountID); err != nil {
			t.Errorf("delete the test account: %v", err)
		}
	})

	registered, inserted, err := dao.Register(ctx, candidate)
	if err != nil || !inserted || registered.AccountID != candidate.AccountID || !registered.CreatedAt.Equal(now) {
		t.Fatalf("register: inserted=%t %+v: %v", inserted, registered, err)
	}
	other := candidate
	other.AccountID, other.RegistrationFingerprint = flowid.RandomID(32), "another"
	existing, inserted, err := dao.Register(ctx, other)
	if err != nil || inserted || existing.AccountID != candidate.AccountID || existing.RegistrationFingerprint != candidate.RegistrationFingerprint {
		t.Fatalf("a second registration of the email: inserted=%t %+v: %v", inserted, existing, err)
	}
	if _, err = dao.AccountByEmail(ctx, "missing-"+id+"@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown email: %v", err)
	}
	if admin, adminErr := dao.IsPlatformAdmin(ctx, candidate.AccountID); adminErr != nil || admin {
		t.Fatalf("an ordinary account is the platform admin=%t: %v", admin, adminErr)
	}

	login := LoginSession{SessionID: "login-" + flowid.RandomID(24), AccountID: candidate.AccountID, TokenHash: "hash-" + id, Audience: "studio", IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	if _, err = dao.CreateLoginSession(ctx, login, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a login verified against a stale credential revision was admitted: %v", err)
	}
	if _, err = dao.CreateLoginSession(ctx, login, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = dao.CreateLoginSession(ctx, login, 1); err != nil {
		t.Fatalf("recreating the same login: %v", err)
	}
	stolen := login
	stolen.TokenHash = "another-hash"
	if _, err = dao.CreateLoginSession(ctx, stolen, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a login with another token hash replaced the session: %v", err)
	}
	if account, authErr := dao.AuthenticatedAccount(ctx, login.SessionID, login.TokenHash, "studio"); authErr != nil || account.AccountID != candidate.AccountID {
		t.Fatalf("authenticate the login: %v", authErr)
	}
	if _, err = dao.AuthenticatedAccount(ctx, login.SessionID, stolen.TokenHash, "studio"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a wrong token hash authenticated: %v", err)
	}

	// An expired login neither authenticates nor survives the account's next login.
	expired := LoginSession{SessionID: "login-" + flowid.RandomID(24), AccountID: candidate.AccountID, TokenHash: "expired-" + id, Audience: "studio", IssuedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour)}
	if _, err = dao.CreateLoginSession(ctx, expired, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = dao.AuthenticatedAccount(ctx, expired.SessionID, expired.TokenHash, "studio"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an expired login authenticated: %v", err)
	}
	next := LoginSession{SessionID: "login-" + flowid.RandomID(24), AccountID: candidate.AccountID, TokenHash: "next-" + id, Audience: "studio", IssuedAt: now, ExpiresAt: now.Add(time.Hour)}
	if _, err = dao.CreateLoginSession(ctx, next, 1); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM login_sessions WHERE session_id = $1`, expired.SessionID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("the expired login remains after the next login (%d): %v", remaining, err)
	}

	// A new authentication epoch revokes every earlier login.
	if _, err = db.ExecContext(ctx, `UPDATE accounts SET auth_epoch = auth_epoch + 1 WHERE account_id = $1`, candidate.AccountID); err != nil {
		t.Fatal(err)
	}
	if _, err = dao.AuthenticatedAccount(ctx, next.SessionID, next.TokenHash, "studio"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a login of an earlier epoch authenticated: %v", err)
	}

	if err = dao.DeleteLoginSession(ctx, login.SessionID, login.TokenHash); err != nil {
		t.Fatal(err)
	}
	if err = dao.DeleteLoginSession(ctx, login.SessionID, login.TokenHash); err != nil {
		t.Fatalf("revoking a revoked login: %v", err)
	}
}

// TestPlatformAdminIntegration registers the platform admin's email in a
// scratch database, where taking them hides no real account: only listed
// ACTIVE accounts are platform administrators.
func TestPlatformAdminIntegration(t *testing.T) {
	db := postgrestest.ScratchDatabaseForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if _, err := postgres.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	dao := New(db, []string{"admin@superverse.test", "second-admin@example.com"})
	now := time.Now().UTC()
	register := func(email string) Account {
		t.Helper()
		account, inserted, err := dao.Register(ctx, Account{
			AccountID: flowid.RandomID(32), CanonicalEmail: email, Status: StatusActive, PasswordRecord: json.RawMessage(`{}`),
			CredentialRevision: 1, AuthEpoch: 1, RegistrationFingerprint: email, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil || !inserted {
			t.Fatalf("register %s: inserted=%t: %v", email, inserted, err)
		}
		return account
	}
	admin, second, other := register("admin@superverse.test"), register("second-admin@example.com"), register("other@example.com")
	for _, check := range []struct {
		accountID string
		want      bool
	}{{admin.AccountID, true}, {second.AccountID, true}, {other.AccountID, false}, {flowid.RandomID(32), false}} {
		if got, err := dao.IsPlatformAdmin(ctx, check.accountID); err != nil || got != check.want {
			t.Errorf("IsPlatformAdmin = %t, want %t: %v", got, check.want, err)
		}
	}
}
