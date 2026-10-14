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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/tenancy/accountdao"
)

// deletedProjectStatus is projects.StatusDeleted, the read model's final status.
const deletedProjectStatus = "DELETED"

// The scopes a usage or token row names.
const (
	previewScope = "preview"
	liveScope    = "live"
)

// fixture is one TeamSpace's rows in the lane's PostgreSQL, deleted when the
// test ends.
type fixture struct {
	dao                  *ConnectionDAO
	db                   *sql.DB
	teamSpaceID, account string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := postgrestest.OpenForTest(t)
	f := fixture{dao: New(db), db: db, teamSpaceID: "ts" + flowid.RandomID(10), account: flowid.RandomID(32)}
	now := time.Now().UTC()
	if _, _, err := accountdao.New(db, "admin@superverse.test").Register(t.Context(), accountdao.Account{
		AccountID: f.account, CanonicalEmail: "conn-" + f.account + "@example.com", Status: accountdao.StatusActive,
		PasswordRecord: json.RawMessage(`{}`), CredentialRevision: 1, AuthEpoch: 1, RegistrationFingerprint: f.account, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DELETE FROM team_space_connection_usages WHERE team_space_id = $1`,
			`DELETE FROM team_space_connections WHERE team_space_id = $1`,
		} {
			if _, err := db.ExecContext(ctx, statement, f.teamSpaceID); err != nil {
				t.Errorf("clean up: %v", err)
			}
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM accounts WHERE account_id = $1`, f.account); err != nil {
			t.Errorf("clean up the account: %v", err)
		}
	})
	return f
}

// connection is a new ACTIVE, READY connection row of the fixture.
func (f fixture) connection(name string) Connection {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return Connection{
		TeamSpaceID: f.teamSpaceID, ConnectionID: "c" + flowid.RandomID(11), CreationFingerprint: "fp-" + name, Name: name,
		ConnectorID: "oauth2-http", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http",
		VersionLine: "v0", ManifestVersion: "v0.1.0", AuthMethodID: "", ClientSource: ClientUser, Settings: json.RawMessage(`{"scopes":["openid"]}`),
		Revision: 1, CredentialStatus: CredentialReady, ConnectionStatus: ConnectionActive,
		CreatorAccountID: f.account, UpdatedByAccountID: f.account, CreatedAt: now, UpdatedAt: now,
	}
}

func (f fixture) create(t *testing.T, c Connection) Connection {
	t.Helper()
	created, inserted, err := f.dao.Create(t.Context(), c)
	if err != nil || !inserted {
		t.Fatalf("create %s: inserted=%t: %v", c.Name, inserted, err)
	}
	return created
}

// TestConnectionCreationIntegration proves that a create is idempotent by
// connection ID, that an ACTIVE name is unique per connector and version line
// even when two creates race, and that a deleted connection frees its name.
func TestConnectionCreationIntegration(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	first := f.create(t, f.connection("preview-test1"))
	replayed, inserted, err := f.dao.Create(ctx, first)
	if err != nil || inserted || replayed.CreationFingerprint != first.CreationFingerprint {
		t.Fatalf("a replay inserted=%t %+v: %v", inserted, replayed, err)
	}
	if _, _, err = f.dao.Create(ctx, f.connection("preview-test1")); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("a second connection of the same name: %v", err)
	}
	otherLine := f.connection("preview-test1")
	otherLine.VersionLine = "v1"
	f.create(t, otherLine)

	var winners, taken int
	var mutex sync.Mutex
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			_, inserted, err := f.dao.Create(ctx, f.connection("raced"))
			mutex.Lock()
			defer mutex.Unlock()
			switch {
			case err == nil && inserted:
				winners++
			case errors.Is(err, ErrNameTaken):
				taken++
			default:
				t.Errorf("a racing create: inserted=%t: %v", inserted, err)
			}
		})
	}
	group.Wait()
	if winners != 1 || taken != 7 {
		t.Fatalf("concurrent creates of one name: %d won and %d were refused", winners, taken)
	}

	if err = f.dao.Delete(ctx, f.teamSpaceID, first.ConnectionID, first.Revision, f.account, deletedProjectStatus); err != nil {
		t.Fatal(err)
	}
	f.create(t, f.connection("preview-test1"))
}

// TestRefreshLeaseIntegration proves that one claimant holds the lease until
// it expires, that a commit needs both the lease and the credential revision,
// and that a claimant whose lease another took over loses its write-back.
func TestRefreshLeaseIntegration(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	c := f.create(t, f.connection("lease"))
	if err := f.dao.ClaimRefreshLease(ctx, f.teamSpaceID, c.ConnectionID, c.CredentialRevision, "first", time.Second); err != nil {
		t.Fatal(err)
	}
	if err := f.dao.ClaimRefreshLease(ctx, f.teamSpaceID, c.ConnectionID, c.CredentialRevision, "second", time.Second); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("a held lease was claimed again: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if err := f.dao.ClaimRefreshLease(ctx, f.teamSpaceID, c.ConnectionID, c.CredentialRevision, "second", 30*time.Second); err != nil {
		t.Fatalf("an expired lease was not taken over: %v", err)
	}
	commit := CredentialCommit{TeamSpaceID: f.teamSpaceID, ConnectionID: c.ConnectionID, ExpectedRevision: c.CredentialRevision,
		LeaseOwner: "first", CredentialRef: json.RawMessage(`{"Key":"k1"}`), Status: CredentialReady, Renewal: true}
	if _, err := f.dao.CommitCredential(ctx, commit); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("the expired claimant's write-back was accepted: %v", err)
	}
	commit.LeaseOwner = "second"
	committed, err := f.dao.CommitCredential(ctx, commit)
	if err != nil || committed.CredentialRevision != c.CredentialRevision+1 || committed.refreshLeaseOwner.Valid {
		t.Fatalf("the lease holder's write-back: %+v: %v", committed, err)
	}
	if _, err = f.dao.CommitCredential(ctx, commit); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("a second write-back at the old revision was accepted: %v", err)
	}
	if err = f.dao.ClaimRefreshLease(ctx, f.teamSpaceID, c.ConnectionID, committed.CredentialRevision, "third", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err = f.dao.RequireReauthorization(ctx, f.teamSpaceID, c.ConnectionID, committed.CredentialRevision, "third", json.RawMessage(`{"code":"invalid_grant"}`)); err != nil {
		t.Fatal(err)
	}
	marked, err := f.dao.Connection(ctx, f.teamSpaceID, c.ConnectionID)
	if err != nil || marked.CredentialStatus != CredentialReauthorizationRequired || marked.refreshLeaseOwner.Valid || marked.CredentialRevision != committed.CredentialRevision+1 {
		t.Fatalf("a terminal refresh: %+v: %v", marked, err)
	}
}

// TestReauthorizationStaysUntilAuthorizedIntegration proves that a
// credential marked REAUTHORIZATION_REQUIRED by a settings change stays so: a
// renewal that held the lease before the change cannot commit, no renewal can
// claim the lease, and only an authorization replaces the credential.
func TestReauthorizationStaysUntilAuthorizedIntegration(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	c := f.create(t, f.connection("reauthorize"))
	if c.CredentialStatus != CredentialReady {
		t.Fatalf("the fixture's credential is %s", c.CredentialStatus)
	}
	if err := f.dao.ClaimRefreshLease(ctx, f.teamSpaceID, c.ConnectionID, c.CredentialRevision, "renewal", time.Second); err != nil {
		t.Fatal(err)
	}
	failure := json.RawMessage(`{"code":"CREDENTIAL_REJECTED"}`)
	updated, err := f.dao.UpdateSettings(ctx, f.teamSpaceID, c.ConnectionID, c.Revision, json.RawMessage(`{"tokenUrl":"https://elsewhere.example"}`), c.ManifestVersion, f.account, failure)
	if err != nil || updated.CredentialStatus != CredentialReauthorizationRequired || updated.CredentialRevision != c.CredentialRevision+1 || len(updated.LastFailure) == 0 {
		t.Fatalf("a settings change that needs reauthorization: %+v: %v", updated, err)
	}
	renewal := CredentialCommit{TeamSpaceID: f.teamSpaceID, ConnectionID: c.ConnectionID, ExpectedRevision: c.CredentialRevision,
		LeaseOwner: "renewal", CredentialRef: json.RawMessage(`{"Key":"renewed"}`), Status: CredentialReady, Renewal: true}
	if _, err = f.dao.CommitCredential(ctx, renewal); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("a renewal committed over the reauthorization: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if err = f.dao.ClaimRefreshLease(ctx, f.teamSpaceID, c.ConnectionID, updated.CredentialRevision, "renewal", time.Minute); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("a renewal claimed the lease of a credential that needs reauthorization: %v", err)
	}
	renewal.ExpectedRevision, renewal.LeaseOwner = updated.CredentialRevision, "authorization"
	if err = f.dao.ClaimAuthorizationLease(ctx, f.teamSpaceID, c.ConnectionID, updated.CredentialRevision, "authorization", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err = f.dao.CommitCredential(ctx, renewal); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("a renewal commit replaced a credential that needs reauthorization: %v", err)
	}
	if err = f.dao.MarkReauthorizationRequired(ctx, f.teamSpaceID, c.ConnectionID, updated.CredentialRevision, failure); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("marking a marked credential again: %v", err)
	}
	renewal.Renewal = false
	authorized, err := f.dao.CommitCredential(ctx, renewal)
	if err != nil || authorized.CredentialStatus != CredentialReady || authorized.CredentialRevision != updated.CredentialRevision+1 {
		t.Fatalf("the authorization's commit: %+v: %v", authorized, err)
	}
	if err = f.dao.MarkReauthorizationRequired(ctx, f.teamSpaceID, c.ConnectionID, authorized.CredentialRevision, failure); err != nil {
		t.Fatal(err)
	}
	if marked, err := f.dao.Connection(ctx, f.teamSpaceID, c.ConnectionID); err != nil || marked.CredentialRevision != authorized.CredentialRevision+1 {
		t.Fatalf("marking advances the credential revision: %+v: %v", marked, err)
	}
}

// TestUsageAndDeletionRaceIntegration starts a usage and a deletion of the
// same connection together many times: a deletion never leaves a DELETED
// connection in use, because both lock the connection row.
func TestUsageAndDeletionRaceIntegration(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	for round := range 40 {
		c := f.create(t, f.connection("race-"+flowid.RandomID(6)))
		project := "p" + flowid.RandomID(7)
		var group sync.WaitGroup
		var usageErr, deleteErr error
		group.Go(func() {
			usageErr = f.dao.AddUsages(ctx, f.teamSpaceID, []string{c.ConnectionID}, project, previewScope, true)
		})
		group.Go(func() {
			deleteErr = f.dao.Delete(ctx, f.teamSpaceID, c.ConnectionID, c.Revision, f.account, deletedProjectStatus)
		})
		group.Wait()
		if usageErr != nil && !errors.Is(usageErr, ErrUnavailable) || deleteErr != nil && !errors.Is(deleteErr, ErrInUse) {
			t.Fatalf("round %d: usage %v, deletion %v", round, usageErr, deleteErr)
		}
		current, err := f.dao.Connection(ctx, f.teamSpaceID, c.ConnectionID)
		if err != nil {
			t.Fatal(err)
		}
		usages, err := f.dao.Usages(ctx, f.teamSpaceID, []string{c.ConnectionID})
		if err != nil {
			t.Fatal(err)
		}
		if current.ConnectionStatus == ConnectionDeleted && len(usages) > 0 {
			t.Fatalf("round %d: the DELETED connection %s is used by %+v", round, c.ConnectionID, usages)
		}
		if (usageErr == nil) == (deleteErr == nil) {
			t.Fatalf("round %d: exactly one of the usage (%v) and the deletion (%v) must win", round, usageErr, deleteErr)
		}
	}
}

// TestUsagesIntegration covers replacing a scope's set, two names selecting
// one connection, releasing unused Live usages only before a time, the read
// authorization of scope tokens, refusing usages that include a deleted
// connection by naming only it, and the cleanup of a Project and a TeamSpace.
func TestUsagesIntegration(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	a, b := f.create(t, f.connection("a")), f.create(t, f.connection("b"))
	project := "p" + flowid.RandomID(7)
	t.Cleanup(func() {
		if err := f.dao.DeleteProjectUsages(context.Background(), project); err != nil {
			t.Error(err)
		}
	})
	if err := f.dao.AddUsages(ctx, f.teamSpaceID, []string{a.ConnectionID, b.ConnectionID}, project, previewScope, true); err != nil {
		t.Fatal(err)
	}
	if err := f.dao.AddUsages(ctx, f.teamSpaceID, []string{a.ConnectionID}, project, previewScope, true); err != nil {
		t.Fatal(err)
	}
	usages, err := f.dao.Usages(ctx, f.teamSpaceID, []string{a.ConnectionID, b.ConnectionID})
	if err != nil || len(usages) != 1 || usages[0].ConnectionID != a.ConnectionID {
		t.Fatalf("the replaced Preview set is %+v: %v", usages, err)
	}

	if err = f.dao.AddUsages(ctx, f.teamSpaceID, []string{a.ConnectionID, b.ConnectionID}, project, liveScope, false); err != nil {
		t.Fatal(err)
	}
	since, err := f.dao.Now(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.dao.AddUsages(ctx, f.teamSpaceID, []string{b.ConnectionID}, project, liveScope, false); err != nil {
		t.Fatal(err)
	}
	released, err := f.dao.ReleaseUnusedUsages(ctx, project, liveScope, nil, since)
	if err != nil || released != 1 {
		t.Fatalf("released %d Live usages: %v", released, err)
	}
	if usages, err = f.dao.Usages(ctx, f.teamSpaceID, []string{a.ConnectionID, b.ConnectionID}); err != nil || len(usages) != 2 {
		t.Fatalf("the usage recorded after the release time was released: %+v: %v", usages, err)
	}

	stored, err := f.dao.RecordScopeToken(ctx, project, previewScope, "hash-"+project, json.RawMessage(`{"key":"first"}`))
	if err != nil || secretKey(t, stored) != "first" {
		t.Fatalf("record the scope token: %s: %v", stored, err)
	}
	if stored, err = f.dao.RecordScopeToken(ctx, project, previewScope, "hash2-"+project, json.RawMessage(`{"key":"second"}`)); err != nil || secretKey(t, stored) != "first" {
		t.Fatalf("a second token replaced the scope's: %s: %v", stored, err)
	}
	if _, err = f.dao.AuthorizeRead(ctx, "unknown-"+project, a.ConnectionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown token: %v", err)
	}
	read, err := f.dao.AuthorizeRead(ctx, "hash-"+project, a.ConnectionID)
	if err != nil || read.ProjectID != project || read.Scope != previewScope || read.Connection == nil || read.Connection.ConnectionID != a.ConnectionID {
		t.Fatalf("the Preview token reading its connection: %+v: %v", read, err)
	}
	if read, err = f.dao.AuthorizeRead(ctx, "hash-"+project, b.ConnectionID); err != nil || read.Connection != nil {
		t.Fatalf("the Preview token read a connection only Live uses: %+v: %v", read, err)
	}

	if err = f.dao.Delete(ctx, f.teamSpaceID, a.ConnectionID, a.Revision, f.account, deletedProjectStatus); !errors.Is(err, ErrInUse) {
		t.Fatalf("a used connection was deleted: %v", err)
	}
	if err = f.dao.DeleteProjectUsages(ctx, project); err != nil {
		t.Fatal(err)
	}
	if _, err = f.dao.AuthorizeRead(ctx, "hash-"+project, a.ConnectionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a deleted Project's token: %v", err)
	}
	if err = f.dao.Delete(ctx, f.teamSpaceID, a.ConnectionID, a.Revision, f.account, deletedProjectStatus); err != nil {
		t.Fatal(err)
	}
	err = f.dao.AddUsages(ctx, f.teamSpaceID, []string{a.ConnectionID, b.ConnectionID}, "p"+flowid.RandomID(7), liveScope, false)
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), a.ConnectionID) || strings.Contains(err.Error(), b.ConnectionID) {
		t.Fatalf("usages including a deleted connection: %v", err)
	}
	if usages, err = f.dao.Usages(ctx, f.teamSpaceID, []string{b.ConnectionID}); err != nil || len(usages) != 0 {
		t.Fatalf("refused usages recorded %+v: %v", usages, err)
	}
	if err = f.dao.AddUsages(ctx, f.teamSpaceID, []string{b.ConnectionID}, "p"+flowid.RandomID(7), liveScope, false); err != nil {
		t.Fatal(err)
	}
	if err = f.dao.DeleteTeamSpaceConnections(ctx, f.teamSpaceID); err != nil {
		t.Fatal(err)
	}
	if active, _, err := f.dao.Connections(ctx, f.teamSpaceID, "", []ConnectionStatus{ConnectionActive}, nil, 10); err != nil || len(active) != 0 {
		t.Fatalf("%d connections survived the TeamSpace's deletion: %v", len(active), err)
	}
}

// TestManifestVersionIntegration proves that the version whose declarations
// refresh a connection moves only when the comparison says it is newer.
func TestManifestVersionIntegration(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	c := f.create(t, f.connection("versions"))
	newer := func(current, candidate string) bool { return candidate > current }
	if changed, err := f.dao.AdvanceManifestVersion(ctx, f.teamSpaceID, c.ConnectionID, "v0.2.0", newer); err != nil || !changed {
		t.Fatalf("advance to v0.2.0: changed=%t: %v", changed, err)
	}
	if changed, err := f.dao.AdvanceManifestVersion(ctx, f.teamSpaceID, c.ConnectionID, "v0.1.5", newer); err != nil || changed {
		t.Fatalf("an older version moved it back: changed=%t: %v", changed, err)
	}
	updated, err := f.dao.UpdateSettings(ctx, f.teamSpaceID, c.ConnectionID, c.Revision, json.RawMessage(`{"scopes":["email"]}`), "v0.2.0", f.account, nil)
	if err != nil || updated.Revision != c.Revision+1 || updated.ManifestVersion != "v0.2.0" {
		t.Fatalf("update the settings: %+v: %v", updated, err)
	}
	if _, err = f.dao.UpdateSettings(ctx, f.teamSpaceID, c.ConnectionID, c.Revision, json.RawMessage(`{}`), "v0.2.0", f.account, nil); !errors.Is(err, ErrRevisionChanged) {
		t.Fatalf("a stale settings update: %v", err)
	}
}

// secretKey is the key of a stored token secret reference.
func secretKey(t *testing.T, ref json.RawMessage) string {
	t.Helper()
	var decoded struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(ref, &decoded); err != nil {
		t.Fatalf("the stored reference %s: %v", ref, err)
	}
	return decoded.Key
}

// TestDeletedProjectUsagesIntegration proves on a migrated scratch database,
// where the test may write the read model's projects rows, that a usage of a
// DELETED Project neither blocks a deletion nor survives it, while a usage of
// a Project that is not DELETED still does.
func TestDeletedProjectUsagesIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	ctx := t.Context()
	f := fixture{dao: New(db), db: db, teamSpaceID: "ts" + flowid.RandomID(10), account: flowid.RandomID(32)}
	now := time.Now().UTC()
	if _, _, err := accountdao.New(db, "admin@superverse.test").Register(ctx, accountdao.Account{
		AccountID: f.account, CanonicalEmail: "conn-" + f.account + "@example.com", Status: accountdao.StatusActive,
		PasswordRecord: json.RawMessage(`{}`), CredentialRevision: 1, AuthEpoch: 1, RegistrationFingerprint: f.account, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	deleted, ready := "p"+flowid.RandomID(7), "p"+flowid.RandomID(7)
	for project, status := range map[string]string{deleted: deletedProjectStatus, ready: "READY"} {
		if _, err := db.ExecContext(ctx, `INSERT INTO projects (project_id, team_space_id, project_status) VALUES ($1, $2, $3)`, project, f.teamSpaceID, status); err != nil {
			t.Fatal(err)
		}
	}
	stale, used := f.create(t, f.connection("stale")), f.create(t, f.connection("used"))
	if err := f.dao.AddUsages(ctx, f.teamSpaceID, []string{stale.ConnectionID}, deleted, liveScope, false); err != nil {
		t.Fatal(err)
	}
	if err := f.dao.AddUsages(ctx, f.teamSpaceID, []string{used.ConnectionID}, ready, liveScope, false); err != nil {
		t.Fatal(err)
	}
	if err := f.dao.Delete(ctx, f.teamSpaceID, stale.ConnectionID, stale.Revision, f.account, deletedProjectStatus); err != nil {
		t.Fatalf("a connection only a DELETED Project used was not deleted: %v", err)
	}
	if err := f.dao.Delete(ctx, f.teamSpaceID, used.ConnectionID, used.Revision, f.account, deletedProjectStatus); !errors.Is(err, ErrInUse) {
		t.Fatalf("a connection a READY Project uses was deleted: %v", err)
	}
	usages, err := f.dao.Usages(ctx, f.teamSpaceID, []string{stale.ConnectionID, used.ConnectionID})
	if err != nil || len(usages) != 1 || usages[0].ProjectID != ready {
		t.Fatalf("the usages left are %+v: %v", usages, err)
	}
}
