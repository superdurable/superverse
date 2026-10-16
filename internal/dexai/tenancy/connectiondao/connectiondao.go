// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connectiondao

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
)

// ConnectionStatus is the lifecycle of a connection row; a DELETED row stays.
type ConnectionStatus string

// The connection statuses.
const (
	ConnectionActive  ConnectionStatus = "ACTIVE"
	ConnectionDeleted ConnectionStatus = "DELETED"
)

// CredentialStatus is whether a connection's credential may be read.
type CredentialStatus string

// The credential statuses.
const (
	CredentialAuthorizing             CredentialStatus = "AUTHORIZING"
	CredentialReady                   CredentialStatus = "READY"
	CredentialReauthorizationRequired CredentialStatus = "REAUTHORIZATION_REQUIRED"
)

// ClientSource is where a connection's OAuth client comes from.
type ClientSource string

// The OAuth client sources.
const (
	ClientUser     ClientSource = "user"
	ClientPlatform ClientSource = "platform"
)

var (
	// ErrNotFound is an absent connection or scope token.
	ErrNotFound = errors.New("not found")
	// ErrRevisionChanged is a write whose expected revision, credential
	// revision or refresh lease is no longer the row's.
	ErrRevisionChanged = errors.New("revision changed")
	// ErrNameTaken is a create whose name another ACTIVE connection of the
	// same TeamSpace, connector and version line has.
	ErrNameTaken = errors.New("name taken")
	// ErrIDTaken is a create whose ID a connection of another TeamSpace has.
	ErrIDTaken = errors.New("connection ID taken")
	// ErrInUse is a deletion of a connection that a Project scope uses.
	ErrInUse = errors.New("in use")
	// ErrUnavailable is a usage of a connection that is not ACTIVE.
	ErrUnavailable = errors.New("not ACTIVE")
)

// Connection is one team_space_connections row. Settings, CredentialRef and
// LastFailure hold JSON that internal/dexai/tenancy/connections decodes.
type Connection struct {
	TeamSpaceID, ConnectionID, CreationFingerprint string
	Name, ConnectorID, ModulePath                  string
	VersionLine, ManifestVersion, AuthMethodID     string
	ClientSource                                   ClientSource
	Settings                                       json.RawMessage
	Revision                                       uint64
	CredentialRef                                  json.RawMessage
	CredentialRevision                             uint64
	CredentialStatus                               CredentialStatus
	CredentialIssuedAt, CredentialExpiresAt        sql.Null[time.Time]
	refreshLeaseOwner                              sql.Null[string]
	RefreshLeaseUntil                              sql.Null[time.Time]
	ConnectionStatus                               ConnectionStatus
	CreatorAccountID, UpdatedByAccountID           string
	LastFailure                                    json.RawMessage
	CreatedAt, UpdatedAt                           time.Time
}

// Usage is one team_space_connection_usages row.
type Usage struct {
	teamSpaceID, ConnectionID, ProjectID, Scope string
	addedAt                                     time.Time
}

// ReadAuthorization is what a scope token authorizes for one connection: the
// Project scope, and the connection when that scope uses it.
type ReadAuthorization struct {
	ProjectID, Scope string
	// Connection is nil when the scope does not use an ACTIVE connection of
	// the requested ID.
	Connection *Connection
}

// CredentialCommit is a credential write: the object reference, its times
// and the status it gives the connection.
type CredentialCommit struct {
	TeamSpaceID, ConnectionID string
	ExpectedRevision          uint64
	LeaseOwner                string
	CredentialRef             json.RawMessage
	IssuedAt, ExpiresAt       sql.Null[time.Time]
	Status                    CredentialStatus
	// UpdatedByAccountID is the actor of an authorization or key change;
	// NULL keeps the previous one for a refresh.
	UpdatedByAccountID sql.Null[string]
	// Renewal commits only over a READY credential.
	Renewal bool
	// ProviderAccount replaces the provider account columns when it is set,
	// for an authorization; nil keeps them, for a refresh.
	ProviderAccount *ProviderAccount
}

// ConnectionDAO reads and writes the connection tables.
type ConnectionDAO struct {
	db *sql.DB
}

// New returns the DAO of db.
func New(db *sql.DB) *ConnectionDAO { return &ConnectionDAO{db: db} }

const connectionColumns = `team_space_id, connection_id, creation_fingerprint, name, connector_id, module_path,
	version_line, manifest_version, auth_method_id, oauth_client, settings, revision, credential_ref, credential_revision,
	credential_status, credential_issued_at, credential_expires_at, refresh_lease_owner, refresh_lease_until,
	connection_status, creator_account_id, updated_by_account_id, last_failure, created_at, updated_at`

// Create inserts c unless a connection of its ID exists, and returns the row
// with whether this call inserted it. A name another ACTIVE connection holds
// is ErrNameTaken.
func (d *ConnectionDAO) Create(ctx context.Context, c Connection) (Connection, bool, error) {
	inserted, err := scanConnection(d.db.QueryRowContext(ctx, `INSERT INTO team_space_connections (`+connectionColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)
		ON CONFLICT (team_space_id, connection_id) DO NOTHING RETURNING `+connectionColumns,
		c.TeamSpaceID, c.ConnectionID, c.CreationFingerprint, c.Name, c.ConnectorID, c.ModulePath,
		c.VersionLine, c.ManifestVersion, c.AuthMethodID, c.ClientSource, []byte(c.Settings), c.Revision, nullJSON(c.CredentialRef), c.CredentialRevision,
		c.CredentialStatus, c.CredentialIssuedAt, c.CredentialExpiresAt, c.refreshLeaseOwner, c.RefreshLeaseUntil,
		c.ConnectionStatus, c.CreatorAccountID, c.UpdatedByAccountID, nullJSON(c.LastFailure), c.CreatedAt, c.UpdatedAt).Scan)
	if err == nil {
		return inserted, true, nil
	}
	if isUniqueViolation(err, "team_space_connections_name") {
		return Connection{}, false, ErrNameTaken
	}
	if isUniqueViolation(err, "team_space_connections_id") {
		return Connection{}, false, ErrIDTaken
	}
	if !errors.Is(err, ErrNotFound) {
		return Connection{}, false, fmt.Errorf("insert the connection: %w", err)
	}
	existing, err := d.Connection(ctx, c.TeamSpaceID, c.ConnectionID)
	return existing, false, err
}

// NameTaken reports whether an ACTIVE connection of the connector and major
// version holds the name in the TeamSpace, as Create's name rule decides.
func (d *ConnectionDAO) NameTaken(ctx context.Context, teamSpaceID, connectorID, versionLine, name string) (bool, error) {
	var taken bool
	err := d.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT FROM team_space_connections WHERE team_space_id = $1
		AND connector_id = $2 AND version_line = $3 AND name = $4 AND connection_status <> $5)`,
		teamSpaceID, connectorID, versionLine, name, ConnectionDeleted).Scan(&taken)
	if err != nil {
		return false, fmt.Errorf("read whether the connection name is taken: %w", err)
	}
	return taken, nil
}

// Connection returns one connection, DELETED ones included.
func (d *ConnectionDAO) Connection(ctx context.Context, teamSpaceID, connectionID string) (Connection, error) {
	c, err := scanConnection(d.db.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM team_space_connections
		WHERE team_space_id = $1 AND connection_id = $2`, teamSpaceID, connectionID).Scan)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Connection{}, fmt.Errorf("read the connection: %w", err)
	}
	return c, err
}

// ConnectionByID returns the connection of an ID, which is unique across
// TeamSpaces, DELETED ones included.
func (d *ConnectionDAO) ConnectionByID(ctx context.Context, connectionID string) (Connection, error) {
	c, err := scanConnection(d.db.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM team_space_connections WHERE connection_id = $1`, connectionID).Scan)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Connection{}, fmt.Errorf("read the connection: %w", err)
	}
	return c, err
}

// Connections returns a page of at most limit connections of a TeamSpace in
// the given statuses, newest first, after the page key when one is given, of
// one connector when connectorID is not empty, and the key of the next page.
func (d *ConnectionDAO) Connections(ctx context.Context, teamSpaceID, connectorID string, statuses []ConnectionStatus, after *postgres.PageKey, limit int) ([]Connection, *postgres.PageKey, error) {
	var afterCreatedAt sql.Null[time.Time]
	var afterConnectionID string
	if after != nil {
		afterCreatedAt, afterConnectionID = sql.Null[time.Time]{V: after.CreatedAt, Valid: true}, after.ID
	}
	names := make([]string, len(statuses))
	for index, status := range statuses {
		names[index] = string(status)
	}
	rows, err := postgres.QueryRows(ctx, d.db, "list the connections", scanConnection, `SELECT `+connectionColumns+` FROM team_space_connections
		WHERE team_space_id = $1 AND ($2 = '' OR connector_id = $2) AND connection_status = ANY($3)
			AND ($4::timestamptz IS NULL OR (created_at, connection_id) < ($4, $5))
		ORDER BY created_at DESC, connection_id DESC LIMIT $6`, teamSpaceID, connectorID, names, afterCreatedAt, afterConnectionID, limit+1)
	if err != nil {
		return nil, nil, err
	}
	page, next := postgres.Page(rows, limit, func(c Connection) postgres.PageKey {
		return postgres.PageKey{CreatedAt: c.CreatedAt, ID: c.ConnectionID}
	})
	return page, next, nil
}

// UpdateSettings replaces an ACTIVE connection's settings and the version
// whose declarations refresh it, at the expected revision. A non-empty
// reauthorization is the failure of a change that the credential no longer
// matches: the same statement marks the credential REAUTHORIZATION_REQUIRED
// and advances its revision, so no renewal in progress commits after it.
func (d *ConnectionDAO) UpdateSettings(ctx context.Context, teamSpaceID, connectionID string, expectedRevision uint64, settings json.RawMessage, manifestVersion, actorAccountID string,
	reauthorization json.RawMessage) (Connection, error) {
	c, err := scanConnection(d.db.QueryRowContext(ctx, `UPDATE team_space_connections
		SET settings = $4, manifest_version = $5, revision = revision + 1, updated_by_account_id = $6, updated_at = now(),
			credential_status = CASE WHEN $8::jsonb IS NULL THEN credential_status ELSE $9 END,
			credential_revision = credential_revision + CASE WHEN $8::jsonb IS NULL THEN 0 ELSE 1 END,
			last_failure = COALESCE($8::jsonb, last_failure)
		WHERE team_space_id = $1 AND connection_id = $2 AND revision = $3 AND connection_status = $7
		RETURNING `+connectionColumns, teamSpaceID, connectionID, expectedRevision, []byte(settings), manifestVersion, actorAccountID, ConnectionActive,
		nullJSON(reauthorization), CredentialReauthorizationRequired).Scan)
	if errors.Is(err, ErrNotFound) {
		return Connection{}, ErrRevisionChanged
	}
	if err != nil {
		return Connection{}, fmt.Errorf("update the connection settings: %w", err)
	}
	return c, nil
}

// AdvanceManifestVersion raises the version whose declarations refresh a
// connection to candidate when newer says candidate is newer than the
// current one, under the row's lock. It reports whether it changed the row.
func (d *ConnectionDAO) AdvanceManifestVersion(ctx context.Context, teamSpaceID, connectionID, candidate string, newer func(current, candidate string) bool) (bool, error) {
	changed := false
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		var current string
		if err := tx.QueryRowContext(ctx, `SELECT manifest_version FROM team_space_connections
			WHERE team_space_id = $1 AND connection_id = $2 FOR UPDATE`, teamSpaceID, connectionID).Scan(&current); err != nil {
			return noRows(err)
		}
		if !newer(current, candidate) {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE team_space_connections SET manifest_version = $3, updated_at = now()
			WHERE team_space_id = $1 AND connection_id = $2`, teamSpaceID, connectionID, candidate); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil && !errors.Is(err, ErrNotFound) {
		return false, fmt.Errorf("advance the manifest version: %w", err)
	}
	return changed, err
}

// ClaimRefreshLease gives owner the credential lease of a connection whose
// credential is READY at the expected revision, ACTIVE and not leased, for
// duration: the lease of a renewal. A lease another owner holds, another
// revision or another status is ErrRevisionChanged.
func (d *ConnectionDAO) ClaimRefreshLease(ctx context.Context, teamSpaceID, connectionID string, expectedRevision uint64, owner string, duration time.Duration) error {
	return d.claimLease(ctx, teamSpaceID, connectionID, expectedRevision, owner, duration, true)
}

// ClaimAuthorizationLease gives owner the lease as ClaimRefreshLease does,
// whatever the credential's status: the lease of an authorization or a key
// change, which replaces the credential.
func (d *ConnectionDAO) ClaimAuthorizationLease(ctx context.Context, teamSpaceID, connectionID string, expectedRevision uint64, owner string, duration time.Duration) error {
	return d.claimLease(ctx, teamSpaceID, connectionID, expectedRevision, owner, duration, false)
}

func (d *ConnectionDAO) claimLease(ctx context.Context, teamSpaceID, connectionID string, expectedRevision uint64, owner string, duration time.Duration, renewal bool) error {
	result, err := d.db.ExecContext(ctx, `UPDATE team_space_connections
		SET refresh_lease_owner = $4, refresh_lease_until = now() + make_interval(secs => $5)
		WHERE team_space_id = $1 AND connection_id = $2 AND credential_revision = $3 AND connection_status = $6
			AND (refresh_lease_until IS NULL OR refresh_lease_until < now()) AND (NOT $7 OR credential_status = $8)`,
		teamSpaceID, connectionID, expectedRevision, owner, duration.Seconds(), ConnectionActive, renewal, CredentialReady)
	return affectedOne(result, err, "claim the credential lease")
}

// CommitCredential writes a credential under the lease that commit's owner
// holds, at the expected credential revision: the reference, the times and
// the status, with the next credential revision, and releases the lease.
func (d *ConnectionDAO) CommitCredential(ctx context.Context, commit CredentialCommit) (Connection, error) {
	var providerAccount ProviderAccount
	if commit.ProviderAccount != nil {
		providerAccount = *commit.ProviderAccount
	}
	c, err := scanConnection(d.db.QueryRowContext(ctx, `UPDATE team_space_connections
		SET credential_ref = $5, credential_revision = credential_revision + 1, credential_issued_at = $6, credential_expires_at = $7,
			credential_status = $8, last_failure = NULL, updated_by_account_id = COALESCE($9, updated_by_account_id),
			provider_account_id = CASE WHEN $12 THEN $13 ELSE provider_account_id END,
			provider_organization_id = CASE WHEN $12 THEN $14 ELSE provider_organization_id END,
			refresh_lease_owner = NULL, refresh_lease_until = NULL, updated_at = now()
		WHERE team_space_id = $1 AND connection_id = $2 AND credential_revision = $3 AND refresh_lease_owner = $4
			AND (NOT $10 OR credential_status = $11)
		RETURNING `+connectionColumns, commit.TeamSpaceID, commit.ConnectionID, commit.ExpectedRevision, commit.LeaseOwner,
		[]byte(commit.CredentialRef), commit.IssuedAt, commit.ExpiresAt, commit.Status, commit.UpdatedByAccountID, commit.Renewal, CredentialReady,
		commit.ProviderAccount != nil, providerAccount.AccountID, providerAccount.OrganizationID).Scan)
	if errors.Is(err, ErrNotFound) {
		return Connection{}, ErrRevisionChanged
	}
	if err != nil {
		return Connection{}, fmt.Errorf("commit the credential: %w", err)
	}
	return c, nil
}

// RequireReauthorization marks the credential that the lease holder found
// unusable, with its failure, advances its revision and releases the lease.
func (d *ConnectionDAO) RequireReauthorization(ctx context.Context, teamSpaceID, connectionID string, expectedRevision uint64, owner string, failure json.RawMessage) error {
	result, err := d.db.ExecContext(ctx, `UPDATE team_space_connections
		SET credential_status = $5, last_failure = $6, credential_revision = credential_revision + 1,
			refresh_lease_owner = NULL, refresh_lease_until = NULL, updated_at = now()
		WHERE team_space_id = $1 AND connection_id = $2 AND credential_revision = $3 AND refresh_lease_owner = $4`,
		teamSpaceID, connectionID, expectedRevision, owner, CredentialReauthorizationRequired, []byte(failure))
	return affectedOne(result, err, "require reauthorization")
}

// MarkReauthorizationRequired marks a READY credential at the expected
// revision as needing reauthorization, with its failure, without a lease: one
// that nothing can renew has expired, or its connection changed the scopes it
// grants. It advances the revision, so no renewal in progress commits after
// it. A credential already marked or replaced stays as it is and is
// ErrRevisionChanged.
func (d *ConnectionDAO) MarkReauthorizationRequired(ctx context.Context, teamSpaceID, connectionID string, expectedRevision uint64, failure json.RawMessage) error {
	result, err := d.db.ExecContext(ctx, `UPDATE team_space_connections
		SET credential_status = $4, last_failure = $6, credential_revision = credential_revision + 1, updated_at = now()
		WHERE team_space_id = $1 AND connection_id = $2 AND credential_revision = $3 AND credential_status = $5`,
		teamSpaceID, connectionID, expectedRevision, CredentialReauthorizationRequired, CredentialReady, []byte(failure))
	return affectedOne(result, err, "mark the credential for reauthorization")
}

// RecordAuthorizationFailure records why owner's authorization of a
// connection failed and releases the lease; the credential and its status
// stay as they were.
func (d *ConnectionDAO) RecordAuthorizationFailure(ctx context.Context, teamSpaceID, connectionID, owner string, failure json.RawMessage) error {
	if _, err := d.db.ExecContext(ctx, `UPDATE team_space_connections SET last_failure = $4, refresh_lease_owner = NULL, refresh_lease_until = NULL, updated_at = now()
		WHERE team_space_id = $1 AND connection_id = $2 AND refresh_lease_owner = $3`, teamSpaceID, connectionID, owner, []byte(failure)); err != nil {
		return fmt.Errorf("record the authorization failure: %w", err)
	}
	return nil
}

// LeaseHeldFor reports whether owner holds the lease for at least duration
// more by the database's clock, so that nobody else can claim it within
// duration.
func (d *ConnectionDAO) LeaseHeldFor(ctx context.Context, teamSpaceID, connectionID, owner string, duration time.Duration) (bool, error) {
	var held bool
	err := d.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM team_space_connections
		WHERE team_space_id = $1 AND connection_id = $2 AND refresh_lease_owner = $3 AND refresh_lease_until > now() + make_interval(secs => $4))`,
		teamSpaceID, connectionID, owner, duration.Seconds()).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("check the credential lease: %w", err)
	}
	return held, nil
}

// ReleaseRefreshLease releases owner's lease without changing the credential.
func (d *ConnectionDAO) ReleaseRefreshLease(ctx context.Context, teamSpaceID, connectionID, owner string) error {
	if _, err := d.db.ExecContext(ctx, `UPDATE team_space_connections SET refresh_lease_owner = NULL, refresh_lease_until = NULL
		WHERE team_space_id = $1 AND connection_id = $2 AND refresh_lease_owner = $3`, teamSpaceID, connectionID, owner); err != nil {
		return fmt.Errorf("release the refresh lease: %w", err)
	}
	return nil
}

// Delete marks an ACTIVE connection at the expected revision DELETED unless a
// Project scope uses it; the deletion locks the row before it reads the
// usages, so it serializes with every usage insertion. Usages of Projects
// whose read model row has deletedProjectStatus are removed, not counted.
func (d *ConnectionDAO) Delete(ctx context.Context, teamSpaceID, connectionID string, expectedRevision uint64, actorAccountID, deletedProjectStatus string) error {
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		var revision uint64
		var status ConnectionStatus
		if err := tx.QueryRowContext(ctx, `SELECT revision, connection_status FROM team_space_connections
			WHERE team_space_id = $1 AND connection_id = $2 FOR UPDATE`, teamSpaceID, connectionID).Scan(&revision, &status); err != nil {
			return noRows(err)
		}
		if status != ConnectionActive {
			return nil
		}
		if revision != expectedRevision {
			return ErrRevisionChanged
		}
		// DELETED is final and the read model only lags, so a DELETED Project's usages are stale.
		if _, err := tx.ExecContext(ctx, `DELETE FROM team_space_connection_usages u USING projects p
			WHERE u.team_space_id = $1 AND u.connection_id = $2 AND p.project_id = u.project_id AND p.project_status = $3`,
			teamSpaceID, connectionID, deletedProjectStatus); err != nil {
			return err
		}
		var used bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM team_space_connection_usages
			WHERE team_space_id = $1 AND connection_id = $2)`, teamSpaceID, connectionID).Scan(&used); err != nil {
			return err
		}
		if used {
			return ErrInUse
		}
		_, err := tx.ExecContext(ctx, `UPDATE team_space_connections
			SET connection_status = $3, credential_ref = NULL, revision = revision + 1, updated_by_account_id = $4,
				refresh_lease_owner = NULL, refresh_lease_until = NULL, updated_at = now()
			WHERE team_space_id = $1 AND connection_id = $2`, teamSpaceID, connectionID, ConnectionDeleted, actorAccountID)
		return err
	})
	if err != nil && !isSentinel(err) {
		return fmt.Errorf("delete the connection: %w", err)
	}
	return err
}

// DeleteTeamSpaceConnections removes the usages of a TeamSpace's
// connections and marks every ACTIVE one DELETED, in one transaction. Its
// Projects are deleted first, so no usage of a live Project remains; a usage
// never refuses this deletion.
func (d *ConnectionDAO) DeleteTeamSpaceConnections(ctx context.Context, teamSpaceID string) error {
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM team_space_connection_usages WHERE team_space_id = $1`, teamSpaceID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE team_space_connections
			SET connection_status = $2, credential_ref = NULL, revision = revision + 1, refresh_lease_owner = NULL, refresh_lease_until = NULL, updated_at = now()
			WHERE team_space_id = $1 AND connection_status = $3`, teamSpaceID, ConnectionDeleted, ConnectionActive)
		return err
	})
	if err != nil {
		return fmt.Errorf("delete the TeamSpace's connections: %w", err)
	}
	return nil
}

// AddUsages records that a Project scope uses each connection, refreshing
// added_at of a recorded one, in one transaction that first locks every
// connection row FOR SHARE: connections that are not ACTIVE connections of
// the Project's TeamSpace are ErrUnavailable, which names them, and nothing is
// recorded. With replace, the scope's other usages are removed.
func (d *ConnectionDAO) AddUsages(ctx context.Context, teamSpaceID string, connectionIDs []string, projectID, scope string, replace bool) error {
	connectionIDs = nonNil(connectionIDs)
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT connection_id FROM team_space_connections
			WHERE team_space_id = $3 AND connection_id = ANY($1) AND connection_status = $2 FOR SHARE`, connectionIDs, ConnectionActive, teamSpaceID)
		if err != nil {
			return err
		}
		active, err := scanIDs(rows)
		if err != nil {
			return err
		}
		if unavailable := slices.DeleteFunc(slices.Clone(connectionIDs), func(id string) bool { return slices.Contains(active, id) }); len(unavailable) > 0 {
			return fmt.Errorf("%w: %s", ErrUnavailable, strings.Join(slices.Compact(slices.Sorted(slices.Values(unavailable))), ", "))
		}
		if replace {
			if _, err = tx.ExecContext(ctx, `DELETE FROM team_space_connection_usages
				WHERE project_id = $1 AND scope = $2 AND connection_id <> ALL($3)`, projectID, scope, connectionIDs); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO team_space_connection_usages (team_space_id, connection_id, project_id, scope, added_at)
			SELECT team_space_id, connection_id, $2, $3, now() FROM team_space_connections WHERE team_space_id = $4 AND connection_id = ANY($1)
			ON CONFLICT (team_space_id, connection_id, project_id, scope) DO UPDATE SET added_at = now()`,
			connectionIDs, projectID, scope, teamSpaceID)
		return err
	})
	if err != nil && !isSentinel(err) {
		return fmt.Errorf("record the connection usages: %w", err)
	}
	return err
}

// ReleaseUnusedUsages removes the usages of a Project scope that keep does
// not name and that were recorded before since.
func (d *ConnectionDAO) ReleaseUnusedUsages(ctx context.Context, projectID, scope string, keep []string, since time.Time) (int64, error) {
	result, err := d.db.ExecContext(ctx, `DELETE FROM team_space_connection_usages
		WHERE project_id = $1 AND scope = $2 AND connection_id <> ALL($3) AND added_at < $4`, projectID, scope, nonNil(keep), since)
	if err != nil {
		return 0, fmt.Errorf("release the unused connection usages: %w", err)
	}
	return result.RowsAffected()
}

// Now is the database's transaction time, the clock of added_at.
func (d *ConnectionDAO) Now(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := d.db.QueryRowContext(ctx, `SELECT now()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read the database time: %w", err)
	}
	return now, nil
}

// Usages returns the usages of the given connections of a TeamSpace.
func (d *ConnectionDAO) Usages(ctx context.Context, teamSpaceID string, connectionIDs []string) ([]Usage, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT team_space_id, connection_id, project_id, scope, added_at FROM team_space_connection_usages
		WHERE team_space_id = $1 AND connection_id = ANY($2) ORDER BY connection_id, project_id, scope`, teamSpaceID, connectionIDs)
	if err != nil {
		return nil, fmt.Errorf("list the connection usages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var usages []Usage
	for rows.Next() {
		var u Usage
		if err = rows.Scan(&u.teamSpaceID, &u.ConnectionID, &u.ProjectID, &u.Scope, &u.addedAt); err != nil {
			return nil, fmt.Errorf("list the connection usages: %w", err)
		}
		usages = append(usages, u)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list the connection usages: %w", err)
	}
	return usages, nil
}

// DeleteProjectUsages removes every usage and scope token of a Project, so
// its credential reads are refused.
func (d *ConnectionDAO) DeleteProjectUsages(ctx context.Context, projectID string) error {
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM team_space_connection_usages WHERE project_id = $1`, projectID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM scope_credential_tokens WHERE project_id = $1`, projectID)
		return err
	})
	if err != nil {
		return fmt.Errorf("delete the project's connection usages and tokens: %w", err)
	}
	return nil
}

// RecordScopeToken stores the hash of a scope's token and the reference of
// the secret object that holds it, unless the scope has one, and returns the
// scope's stored reference, the given one when this call stored it.
func (d *ConnectionDAO) RecordScopeToken(ctx context.Context, projectID, scope, tokenHash string, secretRef json.RawMessage) (json.RawMessage, error) {
	if _, err := d.db.ExecContext(ctx, `INSERT INTO scope_credential_tokens (project_id, scope, token_hash, token_secret_ref, created_at)
		VALUES ($1, $2, $3, $4, now()) ON CONFLICT (project_id, scope) DO NOTHING`, projectID, scope, tokenHash, []byte(secretRef)); err != nil {
		return nil, fmt.Errorf("record the scope token: %w", err)
	}
	return d.ScopeToken(ctx, projectID, scope)
}

// ScopeToken returns the reference of a scope's token secret object.
func (d *ConnectionDAO) ScopeToken(ctx context.Context, projectID, scope string) (json.RawMessage, error) {
	var ref json.RawMessage
	err := d.db.QueryRowContext(ctx, `SELECT token_secret_ref FROM scope_credential_tokens WHERE project_id = $1 AND scope = $2`,
		projectID, scope).Scan(postgres.JSONColumn(&ref))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read the scope token: %w", err)
	}
	return ref, nil
}

// AuthorizeRead resolves a scope token's hash for one connection ID: the
// Project scope, and the ACTIVE connection when the scope uses it. An unknown
// hash is ErrNotFound.
func (d *ConnectionDAO) AuthorizeRead(ctx context.Context, tokenHash, connectionID string) (ReadAuthorization, error) {
	var authorization ReadAuthorization
	var teamSpaceID sql.Null[string]
	err := d.db.QueryRowContext(ctx, `SELECT t.project_id, t.scope, u.team_space_id FROM scope_credential_tokens t
		LEFT JOIN team_space_connection_usages u ON u.project_id = t.project_id AND u.scope = t.scope AND u.connection_id = $2
		WHERE t.token_hash = $1`, tokenHash, connectionID).Scan(&authorization.ProjectID, &authorization.Scope, &teamSpaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return ReadAuthorization{}, ErrNotFound
	}
	if err != nil {
		return ReadAuthorization{}, fmt.Errorf("authorize the credential read: %w", err)
	}
	if !teamSpaceID.Valid {
		return authorization, nil
	}
	c, err := d.Connection(ctx, teamSpaceID.V, connectionID)
	if errors.Is(err, ErrNotFound) {
		return authorization, nil
	}
	if err != nil {
		return ReadAuthorization{}, err
	}
	if c.ConnectionStatus == ConnectionActive {
		authorization.Connection = &c
	}
	return authorization, nil
}

// scanConnection reads the connectionColumns of one row with scan, the Scan
// of a row or of a result set; no row is ErrNotFound.
func scanConnection(scan func(...any) error) (Connection, error) {
	var c Connection
	err := scan(&c.TeamSpaceID, &c.ConnectionID, &c.CreationFingerprint, &c.Name, &c.ConnectorID, &c.ModulePath,
		&c.VersionLine, &c.ManifestVersion, &c.AuthMethodID, &c.ClientSource, postgres.JSONColumn(&c.Settings), &c.Revision,
		postgres.JSONColumn(&c.CredentialRef), &c.CredentialRevision, &c.CredentialStatus, &c.CredentialIssuedAt, &c.CredentialExpiresAt,
		&c.refreshLeaseOwner, &c.RefreshLeaseUntil, &c.ConnectionStatus, &c.CreatorAccountID, &c.UpdatedByAccountID,
		postgres.JSONColumn(&c.LastFailure), &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	return c, err
}

// nullJSON writes an absent JSON value as NULL.
func nullJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return []byte(value)
}

// nonNil is ids, or an empty list for nil: a nil slice is a NULL array, for
// which = ANY and <> ALL are never true.
func nonNil(ids []string) []string {
	if ids == nil {
		return []string{}
	}
	return ids
}

// noRows turns an absent row into ErrNotFound.
func noRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// scanIDs reads and closes rows of one text column.
func scanIDs(rows *sql.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// isSentinel reports the errors this package returns unwrapped.
func isSentinel(err error) bool {
	for _, sentinel := range []error{ErrNotFound, ErrRevisionChanged, ErrNameTaken, ErrIDTaken, ErrInUse, ErrUnavailable,
		ErrNotSelected, ErrNotRelayed, ErrRegistrationLimit} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// affectedOne turns a write that changed no row into ErrRevisionChanged.
func affectedOne(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if affected != 1 {
		return ErrRevisionChanged
	}
	return nil
}

// isUniqueViolation reports a violation of the unique index named index.
func isUniqueViolation(err error, index string) bool {
	var failure *pgconn.PgError
	return errors.As(err, &failure) && failure.Code == "23505" && failure.ConstraintName == index
}
