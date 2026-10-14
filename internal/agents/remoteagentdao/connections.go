// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagentdao

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/platform/postgres"
)

// Status describes credential admission, never Session executor authority.
type Status string

const (
	StatusPairing Status = "PAIRING"
	StatusPaired  Status = "PAIRED"
	StatusRevoked Status = "REVOKED"
)

// RevokeReason is why a connection credential was removed.
type RevokeReason string

const (
	RevokePlatform     RevokeReason = "platform"
	RevokeDisconnected RevokeReason = "disconnected"
	RevokeRemoved      RevokeReason = "removed"
)

// ErrConnectionsFull refuses a seventh live credential row in one Project.
var ErrConnectionsFull = errors.New("connections_full")

// ErrCredentialInvalid means the issuing login/account ended before admission.
var ErrCredentialInvalid = errors.New("issuing login is no longer valid")

// Connection is one credential record. Hashes never reach public DTOs.
type Connection struct {
	ConnectionID, ProjectID, AccountID, LoginSessionID string
	Status                                             Status
	PairingCodeHash, TokenHash                         sql.Null[string]
	PairingExpiresAt, CreatedAt                        time.Time
	BasisEpoch                                         int64
	Binding                                            json.RawMessage
	LastSeenAt, RevokedAt                              sql.Null[time.Time]
	RevokeReason                                       sql.Null[string]
}

// DAO owns only remote_agent_connections, using the server's shared pool.
type DAO struct{ db *sql.DB }

// ListedConnection carries public owner identity without credential hashes.
type ListedConnection struct {
	Connection
	CanonicalEmail string
}

// New returns the connection credential DAO without creating another pool.
func New(db *sql.DB) *DAO { return &DAO{db: db} }

// CreatePairing serializes admission even when the Project has no rows.
// Expired PAIRING and REVOKED rows can never act again; PAIRED rows are never evicted.
func (d *DAO) CreatePairing(ctx context.Context, c Connection, adminEmail, activeStatus string, validate func(Access) error) error {
	return postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		if err := lockProject(ctx, tx, c.ProjectID); err != nil {
			return err
		}
		// The service's earlier read cannot authorize a delayed insert. Recheck
		// current issuing login and Project roles/status after the shared lock.
		access := Access{Connection: c}
		err := tx.QueryRowContext(ctx, `SELECT a.canonical_email, a.canonical_email = $4,
			p.name, p.project_status, pm.role, tm.role FROM accounts a
			JOIN login_sessions l ON l.account_id = a.account_id AND l.session_id = $3
				AND l.auth_epoch = a.auth_epoch AND l.expires_at > now()
			LEFT JOIN projects p ON p.project_id = $1
			LEFT JOIN project_members pm ON pm.project_id = p.project_id AND pm.account_id = a.account_id
			LEFT JOIN team_space_members tm ON tm.team_space_id = p.team_space_id AND tm.account_id = a.account_id
			WHERE a.account_id = $2 AND a.status = $5`, c.ProjectID, c.AccountID, c.LoginSessionID, adminEmail, activeStatus).
			Scan(&access.CanonicalEmail, &access.PlatformAdmin, &access.ProjectName, &access.ProjectStatus, &access.ProjectRole, &access.TeamSpaceRole)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCredentialInvalid
		}
		if err != nil {
			return fmt.Errorf("authorize delayed remote pairing: %w", err)
		}
		if err = validate(access); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM remote_agent_connections WHERE project_id = $1
			AND (status = 'REVOKED' OR (status = 'PAIRING' AND pairing_expires_at <= now()))`, c.ProjectID); err != nil {
			return fmt.Errorf("clean expired remote connections: %w", err)
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM remote_agent_connections WHERE project_id = $1`, c.ProjectID).Scan(&count); err != nil {
			return fmt.Errorf("count remote connections: %w", err)
		}
		if count >= 6 {
			return ErrConnectionsFull
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO remote_agent_connections
			(connection_id, project_id, account_id, login_session_id, status, pairing_code_hash, pairing_expires_at, basis_epoch, created_at)
			VALUES ($1, $2, $3, $4, 'PAIRING', $5, $6, $7, $8)`,
			c.ConnectionID, c.ProjectID, c.AccountID, c.LoginSessionID, c.PairingCodeHash, c.PairingExpiresAt, c.BasisEpoch, c.CreatedAt)
		return err
	})
}

// List returns bounded Project credentials without exposing their hashes.
func (d *DAO) List(ctx context.Context, projectID string) ([]ListedConnection, error) {
	return postgres.QueryRows(ctx, d.db, "list remote connections", func(scan func(...any) error) (ListedConnection, error) {
		var c ListedConnection
		err := scan(append(c.Connection.destinations(), &c.CanonicalEmail)...)
		return c, err
	}, `SELECT c.connection_id, c.project_id, c.account_id, c.login_session_id, c.status,
		NULL, c.pairing_expires_at, NULL, c.basis_epoch, c.binding, c.last_seen_at, c.revoke_reason, c.created_at, c.revoked_at,
		a.canonical_email FROM remote_agent_connections c JOIN accounts a ON a.account_id = c.account_id
		WHERE c.project_id = $1 AND c.status <> 'REVOKED'
		AND (c.status <> 'PAIRING' OR c.pairing_expires_at > now()) ORDER BY c.created_at, c.connection_id LIMIT 6`, projectID)
}

// Revoke removes one exact credential in the authorized Project. A repeated
// revocation is idempotent, and cannot revoke another connection.
func (d *DAO) Revoke(ctx context.Context, projectID, connectionID string, reason RevokeReason) error {
	result, err := d.db.ExecContext(ctx, `UPDATE remote_agent_connections SET status = 'REVOKED',
		pairing_code_hash = NULL, token_hash = NULL, revoke_reason = COALESCE(revoke_reason, $3), revoked_at = COALESCE(revoked_at, now())
		WHERE project_id = $1 AND connection_id = $2`, projectID, connectionID, reason)
	if err != nil {
		return fmt.Errorf("revoke remote connection: %w", err)
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return postgres.ErrNotFound
	}
	return err
}

// Touch rate-limits liveness writes to one per connection per minute.
func (d *DAO) Touch(ctx context.Context, connectionID string) error {
	_, err := d.db.ExecContext(ctx, `UPDATE remote_agent_connections SET last_seen_at = now()
		WHERE connection_id = $1 AND status = 'PAIRED'
		AND (last_seen_at IS NULL OR last_seen_at <= now() - interval '60 seconds')`, connectionID)
	return err
}

// LastSeen reads one exact Project credential for transient Session display.
// It never authenticates, refreshes liveness or supplies executor authority.
func (d *DAO) LastSeen(ctx context.Context, projectID, connectionID string) (*time.Time, error) {
	var seen sql.Null[time.Time]
	err := d.db.QueryRowContext(ctx, `SELECT last_seen_at FROM remote_agent_connections
		WHERE project_id = $1 AND connection_id = $2 AND status = 'PAIRED'`, projectID, connectionID).Scan(&seen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read remote connection last seen: %w", err)
	}
	if !seen.Valid {
		return nil, nil
	}
	return &seen.V, nil
}

// DeleteProject removes credentials at the existing Project cleanup boundary.
func (d *DAO) DeleteProject(ctx context.Context, projectID, deletingStatus, deletedStatus string) (bool, error) {
	ready := false
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		if err := lockProject(ctx, tx, projectID); err != nil {
			return err
		}
		var status sql.Null[string]
		err := tx.QueryRowContext(ctx, `SELECT project_status FROM projects WHERE project_id = $1`, projectID).Scan(&status)
		if errors.Is(err, sql.ErrNoRows) || err == nil && (!status.Valid || status.V != deletingStatus && status.V != deletedStatus) {
			// Existing Project reconciliation retries; there is no new loop or
			// state. A stale READY projection must not acknowledge deletion.
			return nil
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM remote_agent_connections WHERE project_id = $1`, projectID); err != nil {
			return err
		}
		ready = true
		return nil
	})
	return ready && err == nil, err
}

func lockProject(ctx context.Context, tx *sql.Tx, projectID string) error {
	// Private two-key namespace. Hash collisions only serialize unrelated
	// Projects; admission and cleanup always use the same lock.
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(1380012097, hashtext($1))`, projectID)
	return err
}

func (c *Connection) destinations() []any {
	return []any{&c.ConnectionID, &c.ProjectID, &c.AccountID, &c.LoginSessionID, &c.Status,
		&c.PairingCodeHash, &c.PairingExpiresAt, &c.TokenHash, &c.BasisEpoch, postgres.JSONColumn(&c.Binding),
		&c.LastSeenAt, &c.RevokeReason, &c.CreatedAt, &c.RevokedAt}
}
