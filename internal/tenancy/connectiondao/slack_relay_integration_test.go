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
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// relayedConnector is the connector whose platform connections the tests
// relay; the relay names it, the DAO only compares it.
const relayedConnector = "slack"

// registrationLimit is the relay's limit of connection names per usage.
const registrationLimit = 8

// slackConnection is a new ACTIVE, READY platform connection of the relayed
// connector, installed in team.
func (f fixture) slackConnection(t *testing.T, name, team string, organization sql.Null[string]) Connection {
	t.Helper()
	c := f.connection(name)
	c.ConnectorID, c.ClientSource = relayedConnector, ClientPlatform
	c = f.create(t, c)
	f.installIn(t, c, ProviderAccount{AccountID: team, OrganizationID: organization})
	read, err := f.dao.Connection(t.Context(), c.TeamSpaceID, c.ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	return read
}

// installIn commits a credential of c under a lease with the provider
// account an authorization stores.
func (f fixture) installIn(t *testing.T, c Connection, account ProviderAccount) {
	t.Helper()
	ctx := t.Context()
	current, err := f.dao.Connection(ctx, c.TeamSpaceID, c.ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.dao.ClaimAuthorizationLease(ctx, c.TeamSpaceID, c.ConnectionID, current.CredentialRevision, "install", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err = f.dao.CommitCredential(ctx, CredentialCommit{
		TeamSpaceID: c.TeamSpaceID, ConnectionID: c.ConnectionID, ExpectedRevision: current.CredentialRevision, LeaseOwner: "install",
		CredentialRef: json.RawMessage(`{"Key":"k"}`), Status: CredentialReady, ProviderAccount: &account,
	}); err != nil {
		t.Fatal(err)
	}
}

// providerAccount reads the provider account columns of c.
func (f fixture) providerAccount(t *testing.T, c Connection) (sql.Null[string], sql.Null[string]) {
	t.Helper()
	var account, organization sql.Null[string]
	if err := f.db.QueryRowContext(t.Context(), `SELECT provider_account_id, provider_organization_id FROM team_space_connections
		WHERE team_space_id = $1 AND connection_id = $2`, c.TeamSpaceID, c.ConnectionID).Scan(&account, &organization); err != nil {
		t.Fatal(err)
	}
	return account, organization
}

// scopeToken records a token hash of a new Project scope that uses
// connections, and deletes the Project's rows when the test ends.
func (f fixture) scopeToken(t *testing.T, scope string, connections ...Connection) (string, string) {
	t.Helper()
	ctx := t.Context()
	project := "p" + flowid.RandomID(7)
	hash := "hash-" + flowid.RandomID(20)
	if _, err := f.dao.RecordScopeToken(ctx, project, scope, hash, json.RawMessage(`{"key":"token"}`)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := f.dao.DeleteProjectUsages(cleanup, project); err != nil {
			t.Error(err)
		}
	})
	var ids []string
	for _, c := range connections {
		ids = append(ids, c.ConnectionID)
	}
	if len(ids) > 0 {
		if err := f.dao.AddUsages(ctx, f.teamSpaceID, ids, project, scope, false); err != nil {
			t.Fatal(err)
		}
	}
	return project, hash
}

func (f fixture) register(ctx context.Context, hash string, c Connection, name string) error {
	return f.dao.RegisterTrigger(ctx, TriggerRegistration{
		TokenHash: hash, ConnectionID: c.ConnectionID, ConnectionName: name, ConnectorID: relayedConnector, Limit: registrationLimit,
	})
}

func (f fixture) registrationCount(t *testing.T, project string) int {
	t.Helper()
	var count int
	if err := f.db.QueryRowContext(t.Context(), `SELECT count(*) FROM slack_trigger_registrations WHERE project_id = $1`, project).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// TestTriggerRegistrationIntegration registers connection names as an
// application's start does: a scope registers only a platform connection of
// the relayed connector that it uses, an unknown token is refused, a
// registration refreshes its time, a ninth name is refused while the eighth
// refreshes, and twenty concurrent names leave exactly the limit. Without the
// usage row's lock the race would store more than eight.
func TestTriggerRegistrationIntegration(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	slack := f.slackConnection(t, "workspace", "T"+flowid.RandomID(8), sql.Null[string]{})
	user := f.connection("user-slack")
	user.ConnectorID = relayedConnector
	user = f.create(t, user)
	other := f.create(t, f.connection("other"))
	unused := f.slackConnection(t, "unused", "T"+flowid.RandomID(8), sql.Null[string]{})
	project, hash := f.scopeToken(t, previewScope, slack, user, other)

	if err := f.register(ctx, "hash-unknown-"+flowid.RandomID(8), slack, "support"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown token registered: %v", err)
	}
	if err := f.register(ctx, hash, unused, "support"); !errors.Is(err, ErrNotSelected) {
		t.Fatalf("a connection the scope does not use registered: %v", err)
	}
	for _, c := range []Connection{user, other} {
		if err := f.register(ctx, hash, c, "support"); !errors.Is(err, ErrNotRelayed) {
			t.Fatalf("connection %s registered: %v", c.Name, err)
		}
	}
	if err := f.register(ctx, hash, slack, "support"); err != nil {
		t.Fatal(err)
	}
	var first time.Time
	if err := f.db.QueryRowContext(ctx, `SELECT registered_at FROM slack_trigger_registrations WHERE project_id = $1`, project).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := f.register(ctx, hash, slack, "support"); err != nil {
		t.Fatal(err)
	}
	var refreshed time.Time
	if err := f.db.QueryRowContext(ctx, `SELECT registered_at FROM slack_trigger_registrations WHERE project_id = $1`, project).Scan(&refreshed); err != nil || !refreshed.After(first) {
		t.Fatalf("a repeated registration kept %v (now %v): %v", first, refreshed, err)
	}
	for index := 1; index < registrationLimit; index++ {
		if err := f.register(ctx, hash, slack, "name-"+strconv.Itoa(index)); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.register(ctx, hash, slack, "ninth"); !errors.Is(err, ErrRegistrationLimit) {
		t.Fatalf("a ninth name registered: %v", err)
	}
	if err := f.register(ctx, hash, slack, "name-7"); err != nil {
		t.Fatalf("the eighth name did not refresh: %v", err)
	}
	if count := f.registrationCount(t, project); count != registrationLimit {
		t.Fatalf("the usage has %d registrations", count)
	}

	racing, racingHash := f.scopeToken(t, liveScope, slack)
	var wait sync.WaitGroup
	for index := range 20 {
		wait.Go(func() {
			if err := f.register(ctx, racingHash, slack, "race-"+strconv.Itoa(index)); err != nil && !errors.Is(err, ErrRegistrationLimit) {
				t.Error(err)
			}
		})
	}
	wait.Wait()
	if count := f.registrationCount(t, racing); count != registrationLimit {
		t.Fatalf("concurrent registrations left %d rows", count)
	}
}

// TestTriggerRegistrationCleanupIntegration proves that a registration goes
// with its usage: a Preview's replaced usage set, a release of unused
// usages, a Project's deletion and a TeamSpace's deletion each remove the
// registrations of the usages they remove; a freeze also removes the
// registrations of names its document no longer selects.
func TestTriggerRegistrationCleanupIntegration(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	team := "T" + flowid.RandomID(8)
	a := f.slackConnection(t, "a", team, sql.Null[string]{})
	b := f.slackConnection(t, "b", team, sql.Null[string]{})

	preview, previewHash := f.scopeToken(t, previewScope, a, b)
	for _, c := range []Connection{a, b} {
		if err := f.register(ctx, previewHash, c, c.Name); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.dao.AddUsages(ctx, f.teamSpaceID, []string{b.ConnectionID}, preview, previewScope, true); err != nil {
		t.Fatal(err)
	}
	if count := f.registrationCount(t, preview); count != 1 {
		t.Fatalf("the Preview's replaced usages left %d registrations", count)
	}

	// A freeze keeps only the names its document still selects: a renamed
	// name's registration frees its slot.
	if err := f.register(ctx, previewHash, b, "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := f.dao.RetainTriggerRegistrations(ctx, preview, previewScope, map[string]string{"b": b.ConnectionID}); err != nil {
		t.Fatal(err)
	}
	var names []string
	rows, err := f.db.QueryContext(ctx, `SELECT connection_name FROM slack_trigger_registrations WHERE project_id = $1`, preview)
	if err != nil {
		t.Fatal(err)
	}
	names, err = scanIDs(rows)
	if err != nil || !slices.Equal(names, []string{"b"}) {
		t.Fatalf("the retained registrations are %v: %v", names, err)
	}

	live, liveHash := f.scopeToken(t, liveScope, a)
	if err := f.register(ctx, liveHash, a, "a"); err != nil {
		t.Fatal(err)
	}
	since, err := f.dao.Now(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.dao.ReleaseUnusedUsages(ctx, live, liveScope, nil, since); err != nil {
		t.Fatal(err)
	}
	if count := f.registrationCount(t, live); count != 0 {
		t.Fatalf("released usages left %d registrations", count)
	}

	if err = f.dao.DeleteProjectUsages(ctx, preview); err != nil {
		t.Fatal(err)
	}
	if count := f.registrationCount(t, preview); count != 0 {
		t.Fatalf("a deleted Project left %d registrations", count)
	}

	deleted, deletedHash := f.scopeToken(t, liveScope, a)
	if err = f.register(ctx, deletedHash, a, "a"); err != nil {
		t.Fatal(err)
	}
	if err = f.dao.DeleteTeamSpaceConnections(ctx, f.teamSpaceID); err != nil {
		t.Fatal(err)
	}
	if count := f.registrationCount(t, deleted); count != 0 {
		t.Fatalf("a deleted TeamSpace left %d registrations", count)
	}
}

// TestTriggerRoutingIntegration routes provider events: only registrations
// of ACTIVE, READY platform connections of the event's account and
// organization receive it, NULL organizations match each other, a refresh
// keeps the provider account and a reauthorization replaces it, and an
// uninstall marks only the matching platform connections.
func TestTriggerRoutingIntegration(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	team, otherTeam, grid := "T"+flowid.RandomID(8), "T"+flowid.RandomID(8), sql.Null[string]{V: "E" + flowid.RandomID(8), Valid: true}
	plain := f.slackConnection(t, "plain", team, sql.Null[string]{})
	gridded := f.slackConnection(t, "grid", team, grid)
	other := f.slackConnection(t, "other", otherTeam, sql.Null[string]{})
	unregistered := f.slackConnection(t, "unregistered", team, sql.Null[string]{})
	user := f.connection("user")
	user.ConnectorID = relayedConnector
	user = f.create(t, user)
	f.installIn(t, user, ProviderAccount{AccountID: team})

	project, hash := f.scopeToken(t, previewScope, plain, gridded, other, unregistered, user)
	for _, c := range []Connection{plain, gridded, other} {
		if err := f.register(ctx, hash, c, c.Name); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.register(ctx, hash, plain, "second"); err != nil {
		t.Fatal(err)
	}
	routes, err := f.dao.TriggerRoutes(ctx, relayedConnector, ProviderAccount{AccountID: team})
	want := []TriggerRoute{{ProjectID: project, Scope: previewScope, ConnectionName: "plain"}, {ProjectID: project, Scope: previewScope, ConnectionName: "second"}}
	if err != nil || !slices.Equal(routes, want) {
		t.Fatalf("the team's routes are %+v: %v", routes, err)
	}
	if routes, err = f.dao.TriggerRoutes(ctx, relayedConnector, ProviderAccount{AccountID: team, OrganizationID: grid}); err != nil ||
		!slices.Equal(routes, []TriggerRoute{{ProjectID: project, Scope: previewScope, ConnectionName: "grid"}}) {
		t.Fatalf("the organization's routes are %+v: %v", routes, err)
	}
	if routes, err = f.dao.TriggerRoutes(ctx, "github", ProviderAccount{AccountID: team}); err != nil || len(routes) != 0 {
		t.Fatalf("another connector routed %+v: %v", routes, err)
	}

	// A refresh keeps the account; a reauthorization to another workspace
	// moves the connection's routes there.
	f.refresh(t, plain)
	if account, _ := f.providerAccount(t, plain); account.V != team {
		t.Fatalf("a refresh changed the provider account to %q", account.V)
	}
	f.installIn(t, other, ProviderAccount{AccountID: team, OrganizationID: grid})
	if account, organization := f.providerAccount(t, other); account.V != team || organization != grid {
		t.Fatalf("a reauthorization stored %v %v", account, organization)
	}
	f.installIn(t, other, ProviderAccount{AccountID: otherTeam})
	if _, organization := f.providerAccount(t, other); organization.Valid {
		t.Fatalf("a reauthorization outside Enterprise Grid kept the organization %q", organization.V)
	}

	// A refresh in flight while the uninstall arrives cannot commit the
	// credential READY again, and a credential issued after the uninstall
	// event, by a reinstallation, keeps working.
	inFlight, err := f.dao.Connection(ctx, plain.TeamSpaceID, plain.ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.dao.ClaimRefreshLease(ctx, plain.TeamSpaceID, plain.ConnectionID, inFlight.CredentialRevision, "late-refresh", time.Minute); err != nil {
		t.Fatal(err)
	}
	reinstalled := f.slackConnection(t, "reinstalled", team, sql.Null[string]{})
	f.issueAt(t, reinstalled, time.Now().Add(time.Hour))
	marked, err := f.dao.RequireAccountReauthorization(ctx, relayedConnector, ProviderAccount{AccountID: team}, time.Now(), json.RawMessage(`{"code":"app_uninstalled"}`))
	if err != nil || marked != 2 {
		t.Fatalf("an uninstall marked %d connections: %v", marked, err)
	}
	if _, err = f.dao.CommitCredential(ctx, CredentialCommit{TeamSpaceID: plain.TeamSpaceID, ConnectionID: plain.ConnectionID, ExpectedRevision: inFlight.CredentialRevision,
		LeaseOwner: "late-refresh", CredentialRef: json.RawMessage(`{"Key":"late"}`), Status: CredentialReady, Renewal: true}); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("a refresh in flight committed over the uninstall: %v", err)
	}
	if read, err := f.dao.Connection(ctx, reinstalled.TeamSpaceID, reinstalled.ConnectionID); err != nil || read.CredentialStatus != CredentialReady {
		t.Fatalf("an uninstall older than the reinstallation marked it %s: %v", read.CredentialStatus, err)
	}
	for _, c := range []Connection{plain, unregistered, gridded, other, user} {
		read, err := f.dao.Connection(ctx, c.TeamSpaceID, c.ConnectionID)
		wantMarked := c.ConnectionID == plain.ConnectionID || c.ConnectionID == unregistered.ConnectionID
		if err != nil || (read.CredentialStatus == CredentialReauthorizationRequired) != wantMarked {
			t.Fatalf("after the uninstall %s is %s: %v", c.Name, read.CredentialStatus, err)
		}
	}
	if routes, err = f.dao.TriggerRoutes(ctx, relayedConnector, ProviderAccount{AccountID: team}); err != nil || len(routes) != 0 {
		t.Fatalf("a connection that needs reauthorization routed %+v: %v", routes, err)
	}
}

// refresh commits a renewed credential of c without a provider account, as
// a refresh does.
func (f fixture) refresh(t *testing.T, c Connection) {
	t.Helper()
	ctx := t.Context()
	current, err := f.dao.Connection(ctx, c.TeamSpaceID, c.ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.dao.ClaimRefreshLease(ctx, c.TeamSpaceID, c.ConnectionID, current.CredentialRevision, "refresh", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err = f.dao.CommitCredential(ctx, CredentialCommit{
		TeamSpaceID: c.TeamSpaceID, ConnectionID: c.ConnectionID, ExpectedRevision: current.CredentialRevision, LeaseOwner: "refresh",
		CredentialRef: json.RawMessage(`{"Key":"renewed"}`), Status: CredentialReady, Renewal: true,
	}); err != nil {
		t.Fatal(err)
	}
}

// issueAt commits a new credential of c issued at issuedAt, as an
// authorization at that time does.
func (f fixture) issueAt(t *testing.T, c Connection, issuedAt time.Time) {
	t.Helper()
	ctx := t.Context()
	current, err := f.dao.Connection(ctx, c.TeamSpaceID, c.ConnectionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.dao.ClaimAuthorizationLease(ctx, c.TeamSpaceID, c.ConnectionID, current.CredentialRevision, "issue", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err = f.dao.CommitCredential(ctx, CredentialCommit{
		TeamSpaceID: c.TeamSpaceID, ConnectionID: c.ConnectionID, ExpectedRevision: current.CredentialRevision, LeaseOwner: "issue",
		CredentialRef: json.RawMessage(`{"Key":"issued"}`), Status: CredentialReady, IssuedAt: sql.Null[time.Time]{V: issuedAt.UTC(), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
}
