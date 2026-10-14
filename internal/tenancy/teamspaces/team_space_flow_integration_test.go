// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package teamspaces

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
)

// TestTeamSpaceReadModelTableIntegration checks the migrated team_spaces and
// team_space_members tables against TeamSpaceFlow: a column of the right type
// for every synced Attribute and Members field, the row key's columns as the
// primary key, and no other column.
func TestTeamSpaceReadModelTableIntegration(t *testing.T) {
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	columns, maps := postgrestest.SyncedColumnsForTest((&TeamSpaceFlow{}).GetPersistenceSchema().Attributes)
	if len(maps) != 1 || maps["Members"] == nil {
		t.Fatalf("TeamSpaceFlow syncs the AttributeMaps %v; its store declares only Members", maps)
	}
	postgrestest.RequireReadModelTableForTest(t, db, postgrestest.ReadModelTableForTest{
		Name: "team_spaces", Key: []string{"team_space_id"}, Values: columns,
	})
	postgrestest.RequireReadModelTableForTest(t, db, postgrestest.ReadModelTableForTest{
		Name: "team_space_members", Key: []string{"team_space_id", "account_id"}, Values: postgrestest.FieldColumnsForTest(maps["Members"]),
	})
}

// TestTeamSpaceDexIntegration runs one TeamSpace on a Worker of the test
// against the lane's platform Dex, whose Attribute Store writes the read
// model: the start and its replays, member changes that race to remove every
// admin, the rename fence, the Project registry and the deletion command, and
// rows that follow the Flow to DELETED, where its members stay. No Project is
// registered when the deletion starts, so it needs no Project cleanup; it
// deletes the TeamSpace's connection and the objects under its prefix.
func TestTeamSpaceDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	host := dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	objects := artifactStore(ctx, t)
	flow := &TeamSpaceFlow{Connections: connections.NewUsages(connectiondao.New(db), objects)}
	client := &TeamSpaceFlowClient{Dex: dexflowtest.WorkerClientForTest(ctx, t, address, host, flow), Flow: flow}
	teamSpaceID := flowid.RandomID(8)
	account := func(name string) string { return name + "-" + teamSpaceID }
	alice, bob := account("alice"), account("bob")
	connection, credentialKey := teamSpaceConnection(ctx, t, db, objects, teamSpaceID, alice)
	creator := Actor{AccountID: alice}
	platformAdmin := Actor{AccountID: account("admin"), PlatformAdmin: true}

	in := Start{Name: "Team " + teamSpaceID, CreatorAccountID: alice}
	in.Identity.RequestID = flowid.Fingerprint(in)
	in.Identity.Fingerprint, in.Identity.ActorAccountID = in.Identity.RequestID, alice
	if err := client.Create(ctx, teamSpaceID, in); err != nil {
		t.Fatalf("create TeamSpace %s: %s", teamSpaceID, dexflowtest.ErrorCategory(err))
	}
	// The test owns the deletion that completes the Flow before its Worker stops.
	t.Cleanup(func() { completeDeletion(t, client, teamSpaceID, platformAdmin) })
	if err := client.Create(ctx, teamSpaceID, in); err != nil {
		t.Fatalf("a replay of the creation was not joined: %s", dexflowtest.ErrorCategory(err))
	}
	renamed := in
	renamed.Name = "Another name"
	renamed.Identity.RequestID = flowid.Fingerprint(renamed)
	var started *dex.FlowAlreadyStartedError
	if err := client.Create(ctx, teamSpaceID, renamed); !errors.As(err, &started) {
		t.Fatalf("the same ID with another name started or joined: %v", err)
	}

	result, err := client.AddMember(ctx, teamSpaceID, MemberChange{Actor: Actor{AccountID: bob}, AccountID: account("eve"), Role: RoleMember})
	requireRejected(t, err, dexflow.RejectionPermissionDenied, "a member who is not an admin adds a member")
	result, err = client.AddMember(ctx, teamSpaceID, MemberChange{Actor: creator, AccountID: bob, Role: RoleMember})
	if err != nil || len(result.Members) != 2 || result.Members[1].AccountID != bob || result.Members[1].AddedByAccountID != alice {
		t.Fatalf("add bob: %+v: %v", result.Members, err)
	}
	if again, err := client.AddMember(ctx, teamSpaceID, MemberChange{Actor: creator, AccountID: bob, Role: RoleMember}); err != nil || again.TeamSpace.Revision != result.TeamSpace.Revision {
		t.Fatalf("adding a member again with the same role changed the TeamSpace or failed: %v", err)
	}
	_, err = client.AddMember(ctx, teamSpaceID, MemberChange{Actor: creator, AccountID: bob, Role: RoleAdmin})
	requireRejected(t, err, dexflow.RejectionAlreadyExists, "adding a member again with another role")

	// Five admins race to be removed or demoted; exactly one admin must remain.
	admins := []string{alice}
	for i := range 4 {
		admins = append(admins, account("admin"+string(rune('a'+i))))
		if _, err = client.AddMember(ctx, teamSpaceID, MemberChange{Actor: creator, AccountID: admins[i+1], Role: RoleAdmin}); err != nil {
			t.Fatalf("add admin %s: %v", admins[i+1], err)
		}
	}
	var wait sync.WaitGroup
	var mutex sync.Mutex
	var accepted, kept int
	for i, admin := range admins {
		wait.Go(func() {
			change := MemberChange{Actor: platformAdmin, AccountID: admin, Role: RoleMember}
			write := client.SetMemberRole
			if i%2 == 0 {
				write = client.RemoveMember
			}
			_, err := write(ctx, teamSpaceID, change)
			var rejected *dexflow.Rejection
			mutex.Lock()
			defer mutex.Unlock()
			switch {
			case err == nil:
				accepted++
			case errors.As(err, &rejected) && rejected.HasCode(dexflow.RejectionFailedPrecondition):
				kept++
			default:
				t.Errorf("a racing change of admin %s failed: %v", admin, err)
			}
		})
	}
	wait.Wait()
	result, err = client.AddMember(ctx, teamSpaceID, MemberChange{Actor: platformAdmin, AccountID: bob, Role: RoleMember})
	if err != nil || accepted != len(admins)-1 || kept != 1 || countAdmins(result.Members) != 1 {
		t.Fatalf("after the race %d changes passed and %d were refused, leaving %d admins in %+v: %v", accepted, kept, countAdmins(result.Members), result.Members, err)
	}
	survivor := slices.IndexFunc(result.Members, func(m MemberEntry) bool { return m.Role == RoleAdmin })
	admin := Actor{AccountID: result.Members[survivor].AccountID}

	_, err = client.Rename(ctx, teamSpaceID, Renaming{Actor: admin, ExpectedRevision: result.TeamSpace.Revision - 1, Name: "Stale"})
	var stale *dexflow.Rejection
	if !errors.As(err, &stale) || !stale.Stale {
		t.Fatalf("a rename at an older revision was not refused as stale: %v", err)
	}
	team, err := client.Rename(ctx, teamSpaceID, Renaming{Actor: admin, ExpectedRevision: result.TeamSpace.Revision, Name: "Renamed"})
	if err != nil || team.Name != "Renamed" || team.Revision != result.TeamSpace.Revision+1 {
		t.Fatalf("rename at the current revision: %+v: %v", team, err)
	}
	membersOf := func() []string {
		var rows []string
		for _, member := range result.Members {
			rows = append(rows, member.AccountID+":"+member.Role)
		}
		slices.Sort(rows)
		return rows
	}
	awaitRows(ctx, t, db, teamSpaceID, team, membersOf())

	projectID := flowid.RandomID(4)
	err = client.RegisterProject(ctx, teamSpaceID, projectID, Actor{AccountID: account("outsider")})
	requireRejected(t, err, dexflow.RejectionPermissionDenied, "an outsider registers a Project")
	for _, registrant := range []Actor{{AccountID: bob}, {AccountID: bob}, platformAdmin} {
		if err = client.RegisterProject(ctx, teamSpaceID, projectID, registrant); err != nil {
			t.Fatalf("%s registers Project %s again: %v", registrant.AccountID, projectID, err)
		}
	}
	err = client.RegisterProject(ctx, flowid.RandomID(8), projectID, creator)
	requireRejected(t, err, dexflow.RejectionNotFound, "a Project registers in a TeamSpace that never existed")
	for range 2 {
		if err = client.UnregisterProject(ctx, teamSpaceID, projectID); err != nil {
			t.Fatalf("unregister Project %s: %v", projectID, err)
		}
	}

	_, err = client.RequestDeletion(ctx, teamSpaceID, DeletionRequest{Actor: admin, ID: "not-allowed"})
	requireRejected(t, err, dexflow.RejectionPermissionDenied, "a TeamSpace admin requests the deletion")
	deleted := completeDeletion(t, client, teamSpaceID, platformAdmin)
	if deleted.Status != StatusDeleted || deleted.LastCommand == nil || deleted.ActiveCommand != nil {
		t.Fatalf("the completed TeamSpace is %+v, want DELETED", deleted)
	}
	if err = client.UnregisterProject(ctx, teamSpaceID, projectID); err != nil {
		t.Fatalf("unregister from a deleted TeamSpace: %v", err)
	}
	err = client.RegisterProject(ctx, teamSpaceID, flowid.RandomID(4), admin)
	requireRejected(t, err, dexflow.RejectionNotFound, "a Project registers in a deleted TeamSpace")
	awaitRows(ctx, t, db, teamSpaceID, deleted, membersOf())
	if left, err := connectiondao.New(db).ConnectionByID(ctx, connection); err != nil || left.ConnectionStatus != connectiondao.ConnectionDeleted {
		t.Fatalf("the TeamSpace's connection is %s after its deletion: %v", left.ConnectionStatus, err)
	}
	if _, err = objects.GetCurrentJSON(ctx, credentialKey, &map[string]any{}); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("the TeamSpace's connection object survived its deletion: %v", err)
	}
	t.Logf("TeamSpace %s: replays joined, one admin kept, stale rename refused, registry kept, DELETED with its members and without its connections", teamSpaceID)
}

// teamSpaceConnection creates an ACTIVE connection of the TeamSpace with an
// object under its prefix, and the creator's account the row refers to, and
// removes the rows when the test ends.
func teamSpaceConnection(ctx context.Context, t *testing.T, db *sql.DB, objects *objectstore.Store, teamSpaceID, creator string) (string, string) {
	t.Helper()
	now := time.Now().UTC()
	if _, _, err := accountdao.New(db, "admin@superverse.test").Register(ctx, accountdao.Account{
		AccountID: creator, CanonicalEmail: creator + "@example.com", Status: accountdao.StatusActive, PasswordRecord: json.RawMessage(`{}`),
		CredentialRevision: 1, AuthEpoch: 1, RegistrationFingerprint: creator, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	connectionID := "c" + flowid.RandomID(11)
	if _, _, err := connectiondao.New(db).Create(ctx, connectiondao.Connection{
		TeamSpaceID: teamSpaceID, ConnectionID: connectionID, CreationFingerprint: connectionID, Name: "probe", ConnectorID: "openai",
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/openai", VersionLine: "v0", ManifestVersion: "v0.21.1",
		ClientSource: connectiondao.ClientUser, Settings: json.RawMessage(`{}`), CredentialStatus: connectiondao.CredentialAuthorizing,
		ConnectionStatus: connectiondao.ConnectionActive, CreatorAccountID: creator, UpdatedByAccountID: creator, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		if _, err := db.ExecContext(cleanup, `DELETE FROM team_space_connections WHERE team_space_id = $1`, teamSpaceID); err != nil {
			t.Errorf("clean up the connection: %v", err)
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM accounts WHERE account_id = $1`, creator); err != nil {
			t.Errorf("clean up the account: %v", err)
		}
	})
	key := "teamspaces/" + teamSpaceID + "/connections/" + connectionID + "/credentials/probe"
	if _, err := objects.PutJSON(ctx, key, map[string]string{"probe": "value"}); err != nil {
		t.Fatal(err)
	}
	return connectionID, key
}

// artifactStore is the lane's platform bucket under the tests' key prefix:
// SUPERVERSE_S3_ENDPOINT and SUPERVERSE_ARTIFACT_BUCKET, with the platform
// MinIO's credentials in the AWS environment.
func artifactStore(ctx context.Context, t *testing.T) *objectstore.Store {
	t.Helper()
	endpoint, bucket := dexflowtest.Setting(t, "SUPERVERSE_S3_ENDPOINT"), dexflowtest.Setting(t, "SUPERVERSE_ARTIFACT_BUCKET")
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatal("the platform bucket must be the lane's loopback MinIO")
	}
	config, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	store, err := objectstore.NewStore(s3.NewFromConfig(config, func(o *s3.Options) { o.BaseEndpoint = &endpoint; o.UsePathStyle = true }), bucket, "", "sv2-test", true)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// TestTeamSpaceDeletionRaceDexIntegration sends deletion requests with distinct
// IDs at once: exactly one is admitted, the others are refused, and a repeat of
// the admitted ID answers with the current state.
func TestTeamSpaceDeletionRaceDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	host := dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	flow := &TeamSpaceFlow{Connections: connections.NewUsages(connectiondao.New(postgrestest.OpenForTest(t)), artifactStore(ctx, t))}
	client := &TeamSpaceFlowClient{Dex: dexflowtest.WorkerClientForTest(ctx, t, address, host, flow), Flow: flow}
	teamSpaceID := flowid.RandomID(8)
	in := Start{Name: "Race", CreatorAccountID: "creator-" + teamSpaceID}
	in.Identity.RequestID = flowid.Fingerprint(in)
	if err := client.Create(ctx, teamSpaceID, in); err != nil {
		t.Fatalf("create TeamSpace %s: %s", teamSpaceID, dexflowtest.ErrorCategory(err))
	}
	platformAdmin := Actor{AccountID: "admin-" + teamSpaceID, PlatformAdmin: true}
	var wait sync.WaitGroup
	admitted := make(chan string, 8)
	for i := range 8 {
		id := "deletion-" + string(rune('a'+i))
		wait.Go(func() {
			_, err := client.RequestDeletion(ctx, teamSpaceID, DeletionRequest{Actor: platformAdmin, ID: id})
			var rejected *dexflow.Rejection
			var inactive *dex.FlowNotActiveOrNotFoundError
			switch {
			case err == nil:
				admitted <- id
			case errors.As(err, &rejected) && rejected.HasCode(dexflow.RejectionFailedPrecondition), errors.As(err, &inactive):
			default:
				t.Errorf("deletion request %s failed: %v", id, err)
			}
		})
	}
	wait.Wait()
	close(admitted)
	var ids []string
	for id := range admitted {
		ids = append(ids, id)
	}
	if len(ids) != 1 {
		t.Fatalf("the deletion requests %v were admitted; exactly one may be", ids)
	}
	if deleted := completeDeletion(t, client, teamSpaceID, platformAdmin); deleted.Status != StatusDeleted || deleted.LastCommand == nil || deleted.LastCommand.ID != ids[0] {
		t.Fatalf("the completed TeamSpace is %+v, want DELETED by %s", deleted, ids[0])
	}
}

// completeDeletion deletes the TeamSpace as the platform admin, unless its
// deletion already runs or completed, and returns the snapshot its Flow
// completes with.
func completeDeletion(t *testing.T, client *TeamSpaceFlowClient, teamSpaceID string, platformAdmin Actor) TeamSpace {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Minute)
	defer cancel()
	_, err := client.RequestDeletion(ctx, teamSpaceID, DeletionRequest{Actor: platformAdmin, ID: "test-cleanup"})
	var rejected *dexflow.Rejection
	var inactive *dex.FlowNotActiveOrNotFoundError
	if err != nil && !errors.As(err, &inactive) && !(errors.As(err, &rejected) && rejected.HasCode(dexflow.RejectionFailedPrecondition)) {
		t.Errorf("request the deletion of TeamSpace %s: %v", teamSpaceID, err)
		return TeamSpace{}
	}
	result, err := client.Dex.WaitForFlow(ctx, flowid.TeamSpaceFlowID(teamSpaceID), dex.WaitForFlowOptions{NeedsResults: true})
	var deleted TeamSpace
	if err == nil {
		err = result.DecodeSingleOutput(&deleted)
	}
	if err != nil {
		t.Errorf("TeamSpace %s did not complete its deletion: %v", teamSpaceID, err)
	}
	return deleted
}

// awaitRows waits until the team_spaces row equals team and the
// team_space_members rows hold exactly members, as account:role.
func awaitRows(ctx context.Context, t *testing.T, db *sql.DB, teamSpaceID string, team TeamSpace, members []string) {
	t.Helper()
	postgrestest.AwaitRowForTest(ctx, t, db, "team_spaces", func() map[string]any {
		return map[string]any{
			"team_space_id": teamSpaceID, "name": team.Name, "team_space_status": team.Status, "creator_account_id": team.CreatorAccountID,
			"revision": team.Revision, "last_failure": team.LastFailure, "created_at": team.CreatedAt, "updated_at": team.UpdatedAt,
		}
	}, "team_space_id = $1", teamSpaceID)
	for {
		rows := postgrestest.StringsForTest(ctx, t, db, `SELECT account_id || ':' || role FROM team_space_members
			WHERE team_space_id = $1 ORDER BY account_id COLLATE "C"`, teamSpaceID)
		if slices.Equal(rows, members) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the team_space_members rows of %s are %v, want %v", teamSpaceID, rows, members)
		case <-time.After(2 * time.Second):
		}
	}
}

func requireRejected(t *testing.T, err error, code dexflow.RejectionCode, what string) {
	t.Helper()
	var rejected *dexflow.Rejection
	if !errors.As(err, &rejected) || !rejected.HasCode(code) {
		t.Fatalf("%s was not refused with %s: %v", what, code, err)
	}
}

func countAdmins(members []MemberEntry) int {
	count := 0
	for _, member := range members {
		if member.Role == RoleAdmin {
			count++
		}
	}
	return count
}
