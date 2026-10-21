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
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
)

var (
	// ErrAccountExists rejects a registration whose email belongs to another
	// registration request or another password.
	ErrAccountExists = errors.New("account already exists")
	// ErrInvalidEmail rejects a registration email that is not a plain,
	// printable-ASCII address.
	ErrInvalidEmail = errors.New("invalid email address")
	// ErrInvalidCredential rejects a wrong password, an unknown account and any
	// invalid, expired or revoked login token.
	ErrInvalidCredential = errors.New("invalid credential")
	// ErrUnknownEmail is an email that no registered account has.
	ErrUnknownEmail = errors.New("no registered account has this email")
)

// loginLifetime bounds every issued login.
const loginLifetime = 7 * 24 * time.Hour

// AccountSummary is an ACTIVE account without credential material; only
// registration, login and authentication return one.
type AccountSummary struct {
	AccountID, CanonicalEmail string
	PlatformAdmin             bool
}

// IsAdministrator accepts only the authenticated platform admin. A browser
// role flag grants no access.
func IsAdministrator(account AccountSummary) bool {
	return account.AccountID != "" && account.PlatformAdmin
}

// Login is an issued login token and the account it authenticates.
type Login struct {
	Account   AccountSummary
	Token     string
	ExpiresAt time.Time
}

// Accounts registers, logs in, logs out and authenticates accounts, which the
// database owns (docs/architecture/teamspace/accounts.md).
type Accounts struct {
	DAO    *accountdao.AccountDAO
	Signer *TokenSigner
}

// Register creates the account of email, or logs in the account that the same
// request already registered with the same password.
func (a *Accounts) Register(ctx context.Context, email, password, requestID string) (Login, error) {
	canonical, err := canonicalEmail(email)
	if err != nil {
		return Login{}, err
	}
	now := time.Now().UTC()
	fingerprint := flowid.Fingerprint(struct{ Email, RequestID string }{canonical, requestID})
	candidate := accountdao.Account{
		AccountID: flowid.RandomID(32), CanonicalEmail: canonical, Status: accountdao.StatusActive,
		PasswordRecord: comm.Must(json.Marshal(hashPassword(password))), CredentialRevision: 1, AuthEpoch: 1,
		RegistrationFingerprint: fingerprint, CreatedAt: now, UpdatedAt: now,
	}
	account, inserted, err := a.DAO.Register(ctx, candidate)
	if err != nil {
		return Login{}, err
	}
	// Knowing the request ID does not authenticate: a replay must also hold
	// the registered password.
	if !inserted && (account.RegistrationFingerprint != fingerprint || verifyStoredPassword(password, account) != nil) {
		return Login{}, ErrAccountExists
	}
	return a.createLogin(ctx, account)
}

// Login verifies the password of an active account and issues a login.
func (a *Accounts) Login(ctx context.Context, email, password string) (Login, error) {
	canonical, err := canonicalEmail(email)
	if err != nil {
		return Login{}, ErrInvalidCredential
	}
	account, err := a.DAO.AccountByEmail(ctx, canonical)
	if errors.Is(err, accountdao.ErrNotFound) {
		return Login{}, ErrInvalidCredential
	}
	if err != nil {
		return Login{}, err
	}
	if err = verifyStoredPassword(password, account); err != nil {
		return Login{}, err
	}
	return a.createLogin(ctx, account)
}

// createLogin issues a login after its caller verified the password. The
// database admits it only for an ACTIVE account whose credential revision is
// still the verified one.
func (a *Accounts) createLogin(ctx context.Context, account accountdao.Account) (Login, error) {
	sessionID := "login-" + flowid.RandomID(24)
	now := time.Now().UTC()
	expiry := now.Add(loginLifetime)
	token := a.Signer.issue(accountKey(account.CanonicalEmail), sessionID, expiry)
	admitted, err := a.DAO.CreateLoginSession(ctx, accountdao.LoginSession{
		SessionID: sessionID, AccountID: account.AccountID, TokenHash: tokenHash(token), IssuedAt: now, ExpiresAt: expiry,
	}, account.CredentialRevision)
	if errors.Is(err, accountdao.ErrNotFound) {
		return Login{}, ErrInvalidCredential
	}
	if err != nil {
		return Login{}, err
	}
	return Login{Account: a.summary(admitted), Token: token, ExpiresAt: expiry}, nil
}

// Logout revokes the login of token.
func (a *Accounts) Logout(ctx context.Context, token string) error {
	claims, err := a.Signer.verify(token, time.Now())
	if err != nil {
		return err
	}
	return a.DAO.DeleteLoginSession(ctx, claims.SessionID, tokenHash(token))
}

// Authenticate verifies the caller identity that the Studio BFF supplies: the
// signed login must name that account, its login must be current, and the
// opaque AccountID must be the account's.
func (a *Accounts) Authenticate(ctx context.Context, accountID, email, token string) (AccountSummary, error) {
	account, _, err := a.AuthenticateLogin(ctx, accountID, email, token)
	return account, err
}

// AuthenticateLogin additionally returns the verified issuing login identity
// for credentials that must expire with this exact login, never a browser ID.
func (a *Accounts) AuthenticateLogin(ctx context.Context, accountID, email, token string) (AccountSummary, string, error) {
	canonical, err := canonicalEmail(email)
	if err != nil || canonical != email || accountID == "" {
		return AccountSummary{}, "", ErrInvalidCredential
	}
	claims, err := a.Signer.verify(token, time.Now())
	if err != nil || claims.Account != accountKey(canonical) {
		return AccountSummary{}, "", ErrInvalidCredential
	}
	account, err := a.DAO.AuthenticatedAccount(ctx, claims.SessionID, tokenHash(token))
	if errors.Is(err, accountdao.ErrNotFound) || err == nil && account.AccountID != accountID {
		return AccountSummary{}, "", ErrInvalidCredential
	}
	if err != nil {
		return AccountSummary{}, "", err
	}
	return a.summary(account), claims.SessionID, nil
}

// AccountIDByEmail returns the ID of the registered account of an email,
// which another account names, for example to add it to a TeamSpace.
func (a *Accounts) AccountIDByEmail(ctx context.Context, email string) (string, error) {
	canonical, err := canonicalEmail(email)
	if err != nil {
		return "", err
	}
	account, err := a.DAO.AccountByEmail(ctx, canonical)
	if errors.Is(err, accountdao.ErrNotFound) {
		return "", ErrUnknownEmail
	}
	return account.AccountID, err
}

// IsAdministrator reports whether accountID is the active platform admin.
func (a *Accounts) IsAdministrator(ctx context.Context, accountID string) (bool, error) {
	return a.DAO.IsPlatformAdmin(ctx, accountID)
}

func (a *Accounts) summary(account accountdao.Account) AccountSummary {
	return AccountSummary{AccountID: account.AccountID, CanonicalEmail: account.CanonicalEmail,
		PlatformAdmin: account.AccountID != "" && account.Status == accountdao.StatusActive && slices.Contains(a.DAO.PlatformAdminEmails, account.CanonicalEmail)}
}

// verifyStoredPassword checks password against the account's stored record.
func verifyStoredPassword(password string, account accountdao.Account) error {
	var record passwordRecord
	if json.Unmarshal(account.PasswordRecord, &record) != nil {
		return ErrInvalidCredential
	}
	return verifyPassword(password, record)
}
