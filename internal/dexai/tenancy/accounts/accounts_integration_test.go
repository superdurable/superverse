// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package accounts

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
)

func newAccountsForTest(t *testing.T) *Accounts {
	t.Helper()
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	signer, err := NewTokenSigner(key)
	if err != nil {
		t.Fatal(err)
	}
	return &Accounts{DAO: accountdao.New(postgrestest.OpenForTest(t), "admin@superverse.test"), Signer: signer}
}

// TestAccountsIntegration registers, logs in, authenticates and logs out
// against the lane's PostgreSQL. The account stays; every login it issues is
// revoked.
func TestAccountsIntegration(t *testing.T) {
	accounts := newAccountsForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	id := flowid.RandomID(12)
	email, requestID := "accounts-"+id+"@example.com", "register-"+id
	const password = "accounts-integration-password"
	var logins []Login
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		for _, login := range logins {
			if err := accounts.Logout(cleanup, login.Token); err != nil {
				t.Errorf("revoke a test login: %v", err)
			}
		}
	})

	registered, err := accounts.Register(ctx, strings.ToUpper(email), password, requestID)
	if err != nil {
		t.Fatal(err)
	}
	logins = append(logins, registered)
	if registered.Account.CanonicalEmail != email || IsAdministrator(registered.Account) {
		t.Fatalf("registered %+v", registered.Account)
	}
	stored, err := accounts.DAO.AccountByEmail(ctx, email)
	if err != nil || verifyStoredPassword(password, stored) != nil || strings.Contains(string(stored.PasswordRecord), password) {
		t.Fatalf("the account row does not hold the Argon2id record of the password: %v", err)
	}

	// A replay of the request with its password logs in to the same account;
	// knowing the request ID alone does not.
	replayed, err := accounts.Register(ctx, email, password, requestID)
	if err != nil || replayed.Account.AccountID != registered.Account.AccountID {
		t.Fatalf("the replayed registration: %v", err)
	}
	logins = append(logins, replayed)
	if _, err = accounts.Register(ctx, email, password+"-other", requestID); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("the replay with another password: %v", err)
	}
	if _, err = accounts.Register(ctx, email, password, requestID+"-other"); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("another registration of the email: %v", err)
	}
	if _, err = accounts.Register(ctx, "not an email", password, requestID); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("an invalid email: %v", err)
	}

	if _, err = accounts.Login(ctx, email, "wrong-password"); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("a wrong password: %v", err)
	}
	if _, err = accounts.Login(ctx, "missing-"+email, password); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("an unknown email: %v", err)
	}
	login, err := accounts.Login(ctx, email, password)
	if err != nil {
		t.Fatal(err)
	}
	logins = append(logins, login)
	accountID := registered.Account.AccountID
	if account, authErr := accounts.Authenticate(ctx, accountID, email, login.Token); authErr != nil || account.AccountID != accountID {
		t.Fatalf("authenticate the login: %v", authErr)
	}
	for name, caller := range map[string][3]string{
		"another account ID":    {flowid.RandomID(32), email, login.Token},
		"a non-canonical email": {accountID, strings.ToUpper(email), login.Token},
		"another account":       {accountID, "other-" + email, login.Token},
		"a tampered token":      {accountID, email, login.Token + "x"},
	} {
		if _, authErr := accounts.Authenticate(ctx, caller[0], caller[1], caller[2]); !errors.Is(authErr, ErrInvalidCredential) {
			t.Errorf("%s authenticated: %v", name, authErr)
		}
	}

	if err = accounts.Logout(ctx, login.Token); err != nil {
		t.Fatal(err)
	}
	if _, err = accounts.Authenticate(ctx, accountID, email, login.Token); !errors.Is(err, ErrInvalidCredential) {
		t.Fatalf("a revoked login authenticated: %v", err)
	}
	if _, err = accounts.Authenticate(ctx, accountID, email, registered.Token); err != nil {
		t.Fatalf("revoking one login revoked another: %v", err)
	}
	if admin, adminErr := accounts.IsAdministrator(ctx, accountID); adminErr != nil || admin {
		t.Fatalf("an ordinary account is the administrator=%t: %v", admin, adminErr)
	}
}

// TestConcurrentRegistrationIntegration registers one email from several
// requests at once: exactly one creates the account.
func TestConcurrentRegistrationIntegration(t *testing.T) {
	accounts := newAccountsForTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	email := "concurrent-" + flowid.RandomID(12) + "@example.com"
	const attempts = 4
	results := make([]error, attempts)
	logins := make([]Login, attempts)
	var wait sync.WaitGroup
	for attempt := range attempts {
		wait.Go(func() {
			logins[attempt], results[attempt] = accounts.Register(ctx, email, "concurrent-password", "register-"+flowid.RandomID(12))
		})
	}
	wait.Wait()
	created := 0
	for attempt, err := range results {
		switch {
		case err == nil:
			created++
			if logoutErr := accounts.Logout(ctx, logins[attempt].Token); logoutErr != nil {
				t.Errorf("revoke the test login: %v", logoutErr)
			}
		case !errors.Is(err, ErrAccountExists):
			t.Errorf("a concurrent registration failed: %v", err)
		}
	}
	if created != 1 {
		t.Fatalf("%d of %d concurrent registrations created the account", created, attempts)
	}
}
