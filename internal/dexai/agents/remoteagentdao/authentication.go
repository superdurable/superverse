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
	"fmt"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
)

// Access joins a credential with its current authenticated owner and Project
// roles. The consumer applies the owning Project capability rules, preserving
// the distinction between invalid credentials, absent Project and lost role.
type Access struct {
	Connection
	CanonicalEmail             string
	PlatformAdmin              bool
	ProjectName, ProjectStatus sql.Null[string]
	ProjectRole, TeamSpaceRole sql.Null[string]
}

const accessColumns = `c.connection_id, c.project_id, c.account_id, c.login_session_id, c.status,
	c.pairing_code_hash, c.pairing_expires_at, c.token_hash, c.basis_epoch, c.binding,
	c.last_seen_at, c.revoke_reason, c.created_at, c.revoked_at,
	a.canonical_email, a.canonical_email = ANY($2::text[]), p.name, p.project_status, pm.role, tm.role`

const accessJoins = ` FROM remote_agent_connections c
	JOIN accounts a ON a.account_id = c.account_id AND a.status = $3
	JOIN login_sessions l ON l.session_id = c.login_session_id AND l.account_id = c.account_id
		AND l.auth_epoch = a.auth_epoch AND l.expires_at > now()
	LEFT JOIN projects p ON p.project_id = c.project_id
	LEFT JOIN project_members pm ON pm.project_id = c.project_id AND pm.account_id = c.account_id
	LEFT JOIN team_space_members tm ON tm.team_space_id = p.team_space_id AND tm.account_id = c.account_id`

// Authenticate performs one indexed SQL authorization join. No Flow read or
// projection write occurs; unknown, revoked and expired credentials are absent.
func (d *DAO) Authenticate(ctx context.Context, tokenHash string, adminEmails []string, activeStatus string) (Access, error) {
	return scanAccess(d.db.QueryRowContext(ctx, `SELECT `+accessColumns+accessJoins+
		` WHERE c.token_hash = $1 AND c.status = 'PAIRED'`, tokenHash, adminEmails, activeStatus).Scan)
}

// Redeem checks the issuing login and current Project access under the same
// row lock that consumes the pairing hash. validate applies the consumer's
// Project rules before a token is committed; concurrent redemption has one winner.
func (d *DAO) Redeem(ctx context.Context, codeHash, tokenHash string, adminEmails []string, activeStatus string, validate func(Access) error) (Access, error) {
	var access Access
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		var err error
		access, err = scanAccess(tx.QueryRowContext(ctx, `SELECT `+accessColumns+accessJoins+
			` WHERE c.pairing_code_hash = $1 AND c.status = 'PAIRING' AND c.pairing_expires_at > now() FOR UPDATE OF c`,
			codeHash, adminEmails, activeStatus).Scan)
		if err != nil {
			return err
		}
		if err = validate(access); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE remote_agent_connections SET status = 'PAIRED',
			pairing_code_hash = NULL, token_hash = $2, last_seen_at = now()
			WHERE connection_id = $1 AND status = 'PAIRING' AND pairing_code_hash = $3`, access.ConnectionID, tokenHash, codeHash)
		if err != nil {
			return fmt.Errorf("redeem remote pairing: %w", err)
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return postgres.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return Access{}, err
	}
	access.Status = StatusPaired
	access.PairingCodeHash = sql.Null[string]{}
	return access, nil
}

func scanAccess(scan func(...any) error) (Access, error) {
	var access Access
	destinations := append(access.Connection.destinations(), &access.CanonicalEmail, &access.PlatformAdmin,
		&access.ProjectName, &access.ProjectStatus, &access.ProjectRole, &access.TeamSpaceRole)
	err := scan(destinations...)
	return access, postgres.RowError("authenticate remote connection", err)
}
