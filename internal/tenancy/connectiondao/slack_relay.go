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
	"time"

	"github.com/superdurable/superversev2/internal/platform/postgres"
)

var (
	// ErrNotSelected is a trigger registration of a connection that the
	// scope does not use, or that is not ACTIVE.
	ErrNotSelected = errors.New("connection not selected")
	// ErrNotRelayed is a trigger registration of a connection whose events
	// the platform does not relay.
	ErrNotRelayed = errors.New("connection not relayed")
	// ErrRegistrationLimit is a trigger registration of a new connection
	// name on a usage that has the limit's number of them.
	ErrRegistrationLimit = errors.New("too many registered connection names")
)

// ProviderAccount is the provider's account that an authorization installed
// a connection in, which routes the provider's events: Slack's team ID, and
// its Enterprise Grid organization ID, NULL outside Enterprise Grid.
type ProviderAccount struct {
	AccountID      string
	OrganizationID sql.Null[string]
}

// TriggerRoute is one delivery of a routed provider event: a Project scope
// and the connection name its application registered.
type TriggerRoute struct {
	ProjectID, Scope, ConnectionName string
}

// TriggerRegistration is a scope's registration of a connection name whose
// trigger receives a connector's relayed events.
type TriggerRegistration struct {
	// TokenHash authenticates the scope; ConnectionID is the connection the
	// scope selects as ConnectionName.
	TokenHash, ConnectionID, ConnectionName string
	// ConnectorID is the connector whose platform connections are relayed.
	ConnectorID string
	// Limit bounds the registered connection names of one usage.
	Limit int
}

// RegisterTrigger records a scope's trigger registration, or refreshes its
// time, in one transaction that locks the usage row before it counts, so
// concurrent registrations never exceed the limit and none outlives its
// usage. An unknown token hash is ErrNotFound; a connection the scope does
// not use, or that is not ACTIVE, ErrNotSelected; one that is not a platform
// connection of the connector, ErrNotRelayed; a new name beyond the limit,
// ErrRegistrationLimit.
func (d *ConnectionDAO) RegisterTrigger(ctx context.Context, registration TriggerRegistration) error {
	err := postgres.InTransaction(ctx, d.db, func(tx *sql.Tx) error {
		var projectID, scope string
		if err := tx.QueryRowContext(ctx, `SELECT project_id, scope FROM scope_credential_tokens WHERE token_hash = $1`,
			registration.TokenHash).Scan(&projectID, &scope); err != nil {
			return noRows(err)
		}
		var teamSpaceID, connectorID string
		var client ClientSource
		var status ConnectionStatus
		err := tx.QueryRowContext(ctx, `SELECT u.team_space_id, c.connector_id, c.oauth_client, c.connection_status
			FROM team_space_connection_usages u JOIN team_space_connections c USING (team_space_id, connection_id)
			WHERE u.project_id = $1 AND u.scope = $2 AND u.connection_id = $3 FOR NO KEY UPDATE OF u`,
			projectID, scope, registration.ConnectionID).Scan(&teamSpaceID, &connectorID, &client, &status)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return ErrNotSelected
		case err != nil:
			return err
		case status != ConnectionActive:
			return ErrNotSelected
		case connectorID != registration.ConnectorID || client != ClientPlatform:
			return ErrNotRelayed
		}
		var others int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM slack_trigger_registrations
			WHERE team_space_id = $1 AND connection_id = $2 AND project_id = $3 AND scope = $4 AND connection_name <> $5`,
			teamSpaceID, registration.ConnectionID, projectID, scope, registration.ConnectionName).Scan(&others); err != nil {
			return err
		}
		if others >= registration.Limit {
			return ErrRegistrationLimit
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO slack_trigger_registrations
			(team_space_id, connection_id, project_id, scope, connection_name, registered_at) VALUES ($1, $2, $3, $4, $5, now())
			ON CONFLICT (team_space_id, connection_id, project_id, scope, connection_name) DO UPDATE SET registered_at = now()`,
			teamSpaceID, registration.ConnectionID, projectID, scope, registration.ConnectionName)
		return err
	})
	if err != nil && !isSentinel(err) {
		return fmt.Errorf("register the trigger: %w", err)
	}
	return err
}

// TriggerRoutes returns the registrations of every ACTIVE, READY platform
// connection of a connector installed in the provider account, one route
// per registered connection name of each scope. The organization matches
// with IS NOT DISTINCT FROM, so NULL matches NULL.
func (d *ConnectionDAO) TriggerRoutes(ctx context.Context, connectorID string, account ProviderAccount) ([]TriggerRoute, error) {
	// The status is written into the statement, so the planner can use the
	// partial index team_space_connections_provider_account.
	rows, err := d.db.QueryContext(ctx, `SELECT r.project_id, r.scope, r.connection_name
		FROM team_space_connections c JOIN slack_trigger_registrations r USING (team_space_id, connection_id)
		WHERE c.connector_id = $1 AND c.provider_account_id = $2 AND c.connection_status = '`+string(ConnectionActive)+`'
			AND c.provider_organization_id IS NOT DISTINCT FROM $3 AND c.credential_status = $4 AND c.oauth_client = $5
		ORDER BY r.project_id, r.scope, r.connection_name`,
		connectorID, account.AccountID, account.OrganizationID, CredentialReady, ClientPlatform)
	if err != nil {
		return nil, fmt.Errorf("route the provider event: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var routes []TriggerRoute
	for rows.Next() {
		var route TriggerRoute
		if err = rows.Scan(&route.ProjectID, &route.Scope, &route.ConnectionName); err != nil {
			return nil, fmt.Errorf("route the provider event: %w", err)
		}
		routes = append(routes, route)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("route the provider event: %w", err)
	}
	return routes, nil
}

// ConnectionKey identifies a TeamSpace connection.
type ConnectionKey struct{ TeamSpaceID, ConnectionID string }

// PlatformConnections returns the ACTIVE platform connections of a connector
// installed in the provider account, whatever their credential status or
// registrations: the connections an event without a route was meant for.
func (d *ConnectionDAO) PlatformConnections(ctx context.Context, connectorID string, account ProviderAccount) ([]ConnectionKey, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT team_space_id, connection_id FROM team_space_connections
		WHERE connector_id = $1 AND provider_account_id = $2 AND connection_status = '`+string(ConnectionActive)+`'
			AND provider_organization_id IS NOT DISTINCT FROM $3 AND oauth_client = $4
		ORDER BY team_space_id, connection_id`, connectorID, account.AccountID, account.OrganizationID, ClientPlatform)
	if err != nil {
		return nil, fmt.Errorf("list the installation's connections: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var keys []ConnectionKey
	for rows.Next() {
		var key ConnectionKey
		if err = rows.Scan(&key.TeamSpaceID, &key.ConnectionID); err != nil {
			return nil, fmt.Errorf("list the installation's connections: %w", err)
		}
		keys = append(keys, key)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list the installation's connections: %w", err)
	}
	return keys, nil
}

// RequireAccountReauthorization marks every READY credential of the ACTIVE
// platform connections of a connector installed in the provider account as
// needing reauthorization, with failure, after the provider reported at
// occurredAt that the installation is gone, and returns how many it marked.
// The mark takes the next credential revision and ends any lease, so an
// in-flight refresh cannot commit the credential READY again; a credential
// issued after occurredAt belongs to a later installation and stays.
func (d *ConnectionDAO) RequireAccountReauthorization(ctx context.Context, connectorID string, account ProviderAccount, occurredAt time.Time, failure json.RawMessage) (int64, error) {
	result, err := d.db.ExecContext(ctx, `UPDATE team_space_connections SET credential_status = $5, last_failure = $8,
			credential_revision = credential_revision + 1, refresh_lease_owner = NULL, refresh_lease_until = NULL, updated_at = now()
		WHERE connector_id = $1 AND provider_account_id = $2 AND connection_status = $3
			AND provider_organization_id IS NOT DISTINCT FROM $4 AND credential_status = $6 AND oauth_client = $7
			AND (credential_issued_at IS NULL OR credential_issued_at < $9)`,
		connectorID, account.AccountID, ConnectionActive, account.OrganizationID, CredentialReauthorizationRequired, CredentialReady, ClientPlatform, []byte(failure), occurredAt)
	if err != nil {
		return 0, fmt.Errorf("require reauthorization of the provider account: %w", err)
	}
	return result.RowsAffected()
}

// RetainTriggerRegistrations removes a Project scope's trigger registrations
// whose connection name selects the registered connection in none of the
// selected configurations, each by connection name, so a renamed or
// replaced name frees its slot; the application registers its current names
// again when it starts.
func (d *ConnectionDAO) RetainTriggerRegistrations(ctx context.Context, projectID, scope string, selected ...map[string]string) error {
	var names, ids []string
	for _, selections := range selected {
		for name, id := range selections {
			names, ids = append(names, name), append(ids, id)
		}
	}
	if _, err := d.db.ExecContext(ctx, `DELETE FROM slack_trigger_registrations r WHERE r.project_id = $1 AND r.scope = $2
		AND NOT EXISTS (SELECT 1 FROM unnest($3::text[], $4::text[]) AS kept(connection_name, connection_id)
			WHERE kept.connection_name = r.connection_name AND kept.connection_id = r.connection_id)`, projectID, scope, names, ids); err != nil {
		return fmt.Errorf("retain the trigger registrations: %w", err)
	}
	return nil
}
