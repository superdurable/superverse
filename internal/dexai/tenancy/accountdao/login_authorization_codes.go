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
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
)

var (
	ErrInvalidGrant = errors.New("invalid authorization grant")
	ErrCodeLimit    = errors.New("authorization code limit reached")
)

const codeLifetime = 60 * time.Second

// AuthorizationCode is the SQL admission input. The caller hashes the opaque
// code and decodes its S256 challenge before passing it to the DAO.
type AuthorizationCode struct {
	Hash, Challenge            []byte
	AccountID, IssuerSessionID string
	IssuerTokenHash            string
	ProductOrigin, RedirectURI string
}

// IssueAuthorizationCode admits one bounded code under the account and issuer
// row locks. SQL clock time after locking determines its exact 60-second expiry.
func (d *AccountDAO) IssueAuthorizationCode(ctx context.Context, code AuthorizationCode) (time.Time, error) {
	var expiresAt time.Time
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
			return err
		}
		var epoch int64
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status, auth_epoch FROM accounts WHERE account_id = $1 FOR UPDATE`, code.AccountID).Scan(&status, &epoch); err != nil {
			return missingGrant(err)
		}
		var issuerEpoch int64
		var issuerExpiry time.Time
		var issuerAudience string
		if err := tx.QueryRowContext(ctx, `SELECT auth_epoch, expires_at, audience FROM login_sessions
			WHERE session_id = $1 AND account_id = $2 AND token_hash = $3 FOR UPDATE`,
			code.IssuerSessionID, code.AccountID, code.IssuerTokenHash).Scan(&issuerEpoch, &issuerExpiry, &issuerAudience); err != nil {
			return missingGrant(err)
		}
		var admittedAt time.Time
		if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&admittedAt); err != nil {
			return err
		}
		if status != StatusActive || epoch != issuerEpoch || issuerAudience != "login" || !issuerExpiry.After(admittedAt) {
			return ErrInvalidGrant
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM login_authorization_codes WHERE issuer_session_id = $1 AND expires_at <= $2`,
			code.IssuerSessionID, admittedAt); err != nil {
			return err
		}
		var retained int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM login_authorization_codes WHERE issuer_session_id = $1`,
			code.IssuerSessionID).Scan(&retained); err != nil {
			return err
		}
		if retained >= 5 {
			return ErrCodeLimit
		}
		expiresAt = admittedAt.Add(codeLifetime)
		_, err := tx.ExecContext(ctx, `INSERT INTO login_authorization_codes
			(code_hash, account_id, issuer_session_id, product_origin, redirect_uri, code_challenge, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, code.Hash, code.AccountID,
			code.IssuerSessionID, code.ProductOrigin, code.RedirectURI, code.Challenge, expiresAt)
		return err
	})
	if err != nil && !errors.Is(err, ErrCodeLimit) && !errors.Is(err, ErrInvalidGrant) {
		return time.Time{}, fmt.Errorf("issue authorization code: %w", err)
	}
	return expiresAt, err
}

// ExchangeAuthorizationCode admits one product session and deletes its code in
// one transaction. prepare signs the new session using the DB admission time;
// neither a committed response nor a consumed code is replayed on uncertainty.
func (d *AccountDAO) ExchangeAuthorizationCode(ctx context.Context, hash, challenge []byte, origin, redirectURI string,
	prepare func(Account, time.Time) LoginSession) (Account, LoginSession, error) {
	var account Account
	var session LoginSession
	invalid := false
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
			return err
		}
		var accountID, issuerID string
		if err := tx.QueryRowContext(ctx, `SELECT account_id, issuer_session_id FROM login_authorization_codes WHERE code_hash = $1`,
			hash).Scan(&accountID, &issuerID); err != nil {
			return missingGrant(err)
		}
		var err error
		account, err = scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE account_id = $1 FOR UPDATE`, accountID))
		if err != nil {
			return missingGrant(err)
		}
		var issuerAccount, issuerAudience string
		var issuerEpoch int64
		var issuerExpiry time.Time
		if err := tx.QueryRowContext(ctx, `SELECT account_id, auth_epoch, expires_at, audience FROM login_sessions
			WHERE session_id = $1 FOR UPDATE`, issuerID).Scan(&issuerAccount, &issuerEpoch, &issuerExpiry, &issuerAudience); err != nil {
			return missingGrant(err)
		}
		var storedOrigin, storedRedirect string
		var storedChallenge []byte
		var codeExpiry time.Time
		if err := tx.QueryRowContext(ctx, `SELECT product_origin, redirect_uri, code_challenge, expires_at
			FROM login_authorization_codes WHERE code_hash = $1 AND account_id = $2 AND issuer_session_id = $3 FOR UPDATE`,
			hash, accountID, issuerID).Scan(&storedOrigin, &storedRedirect, &storedChallenge, &codeExpiry); err != nil {
			return missingGrant(err)
		}
		var admittedAt time.Time
		if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&admittedAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM login_authorization_codes WHERE issuer_session_id = $1 AND expires_at <= $2`,
			issuerID, admittedAt); err != nil {
			return err
		}
		if account.Status != StatusActive || account.AuthEpoch != issuerEpoch || issuerAccount != accountID ||
			issuerAudience != "login" || !issuerExpiry.After(admittedAt) || !codeExpiry.After(admittedAt) ||
			storedOrigin != origin || storedRedirect != redirectURI || subtle.ConstantTimeCompare(storedChallenge, challenge) != 1 {
			invalid = true
			return nil
		}
		session = prepare(account, admittedAt)
		result, err := tx.ExecContext(ctx, `INSERT INTO login_sessions
			(session_id, account_id, token_hash, auth_epoch, issued_at, expires_at, audience)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, session.SessionID, accountID,
			session.TokenHash, account.AuthEpoch, session.IssuedAt, session.ExpiresAt, session.Audience)
		if err != nil {
			return err
		}
		if count, err := result.RowsAffected(); err != nil || count != 1 {
			return errors.Join(ErrInvalidGrant, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM login_authorization_codes WHERE code_hash = $1`, hash); err != nil {
			return err
		}
		return nil
	})
	if err != nil && !errors.Is(err, ErrInvalidGrant) {
		return Account{}, LoginSession{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	if invalid || errors.Is(err, ErrInvalidGrant) {
		return Account{}, LoginSession{}, ErrInvalidGrant
	}
	return account, session, nil
}

func missingGrant(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNotFound) {
		return ErrInvalidGrant
	}
	return err
}
