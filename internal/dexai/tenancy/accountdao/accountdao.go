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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
)

// StatusActive is the status of an account that may log in; the only status an
// account has.
const StatusActive = "ACTIVE"

// ErrNotFound is an account or login session that does not exist, or a login
// session that is expired, revoked or of an earlier authentication epoch.
var ErrNotFound = errors.New("not found")

// Account is one accounts row. PasswordRecord is the encoded Argon2id record,
// which only the account package interprets.
type Account struct {
	AccountID, CanonicalEmail, Status string
	PasswordRecord                    json.RawMessage
	CredentialRevision, AuthEpoch     int64
	RegistrationFingerprint           string
	CreatedAt, UpdatedAt              time.Time
}

// LoginSession is one login_sessions row; the token is stored only as its
// SHA-256.
type LoginSession struct {
	SessionID, AccountID, TokenHash, Audience string
	IssuedAt, ExpiresAt                       time.Time
}

// AccountDAO reads and writes the accounts and login_sessions tables.
type AccountDAO struct {
	db                  *sql.DB
	PlatformAdminEmails []string
}

// New returns the DAO of db.
func New(db *sql.DB, platformAdminEmails []string) *AccountDAO {
	return &AccountDAO{db: db, PlatformAdminEmails: platformAdminEmails}
}

const accountColumns = `account_id, canonical_email, status, password_record, credential_revision, auth_epoch,
	registration_fingerprint, created_at, updated_at`

// Register inserts the account unless its email is registered, and returns the
// registered row and whether it is the one this call inserted.
func (d *AccountDAO) Register(ctx context.Context, a Account) (Account, bool, error) {
	row := d.db.QueryRowContext(ctx, `INSERT INTO accounts (`+accountColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (canonical_email) DO NOTHING RETURNING `+accountColumns,
		a.AccountID, a.CanonicalEmail, a.Status, a.PasswordRecord, a.CredentialRevision, a.AuthEpoch,
		a.RegistrationFingerprint, a.CreatedAt, a.UpdatedAt)
	inserted, err := scanAccount(row)
	if err == nil {
		return inserted, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Account{}, false, fmt.Errorf("insert the account: %w", err)
	}
	existing, err := d.AccountByEmail(ctx, a.CanonicalEmail)
	return existing, false, err
}

// AccountByEmail returns the account of a canonical email.
func (d *AccountDAO) AccountByEmail(ctx context.Context, canonicalEmail string) (Account, error) {
	a, err := scanAccount(d.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE canonical_email = $1`, canonicalEmail))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Account{}, fmt.Errorf("read the account by email: %w", err)
	}
	return a, err
}

// IsPlatformAdmin reports whether accountID is the active platform admin.
func (d *AccountDAO) IsPlatformAdmin(ctx context.Context, accountID string) (bool, error) {
	var administrator bool
	err := d.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM accounts
		WHERE account_id = $1 AND canonical_email = ANY($2::text[]) AND status = $3)`, accountID, d.PlatformAdminEmails, StatusActive).Scan(&administrator)
	if err != nil {
		return false, fmt.Errorf("read the account: %w", err)
	}
	return administrator, nil
}

// CreateLoginSession admits a login of an ACTIVE account whose credential
// revision is still the one its password was verified against, and deletes
// the account's expired logins in the same transaction. It returns the account,
// or ErrNotFound when the login is not admitted. Recreating the same session
// with the same token hash succeeds; another token hash is not admitted.
func (d *AccountDAO) CreateLoginSession(ctx context.Context, s LoginSession, credentialRevision int64) (Account, error) {
	var admitted Account
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		a, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts
			WHERE account_id = $1 AND status = $2 AND credential_revision = $3 FOR SHARE`, s.AccountID, StatusActive, credentialRevision))
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM login_sessions WHERE account_id = $1 AND expires_at <= now()`, s.AccountID); err != nil {
			return fmt.Errorf("delete expired logins: %w", err)
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO login_sessions (session_id, account_id, token_hash, auth_epoch, issued_at, expires_at, audience)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (session_id) DO UPDATE SET session_id = EXCLUDED.session_id
			WHERE login_sessions.account_id = EXCLUDED.account_id AND login_sessions.token_hash = EXCLUDED.token_hash
				AND login_sessions.audience = EXCLUDED.audience`,
			s.SessionID, s.AccountID, s.TokenHash, a.AuthEpoch, s.IssuedAt, s.ExpiresAt, s.Audience)
		if err != nil {
			return fmt.Errorf("insert the login: %w", err)
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			return errors.Join(ErrNotFound, err)
		}
		admitted = a
		return nil
	})
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Account{}, fmt.Errorf("create the login: %w", err)
	}
	return admitted, err
}

// AuthenticatedAccount returns the account of an unexpired login whose token
// hash and authentication epoch match an ACTIVE account.
func (d *AccountDAO) AuthenticatedAccount(ctx context.Context, sessionID, tokenHash, audience string) (Account, error) {
	a, err := scanAccount(d.db.QueryRowContext(ctx, `SELECT a.account_id, a.canonical_email, a.status, a.password_record,
		a.credential_revision, a.auth_epoch, a.registration_fingerprint, a.created_at, a.updated_at
		FROM login_sessions s JOIN accounts a ON a.account_id = s.account_id
		WHERE s.session_id = $1 AND s.token_hash = $2 AND s.audience = $3
			AND s.expires_at > now() AND s.auth_epoch = a.auth_epoch AND a.status = $4`,
		sessionID, tokenHash, audience, StatusActive))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Account{}, fmt.Errorf("authenticate the login: %w", err)
	}
	return a, err
}

// DeleteLoginSession revokes a login; an absent one is already revoked.
func (d *AccountDAO) DeleteLoginSession(ctx context.Context, sessionID, tokenHash string) error {
	if _, err := d.db.ExecContext(ctx, `DELETE FROM login_sessions WHERE session_id = $1 AND token_hash = $2`, sessionID, tokenHash); err != nil {
		return fmt.Errorf("delete the login: %w", err)
	}
	return nil
}

func scanAccount(row *sql.Row) (Account, error) {
	var a Account
	err := row.Scan(&a.AccountID, &a.CanonicalEmail, &a.Status, &a.PasswordRecord, &a.CredentialRevision, &a.AuthEpoch,
		&a.RegistrationFingerprint, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return a, err
}
