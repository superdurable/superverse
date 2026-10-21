// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue"
	"github.com/superdurable/superversev2/internal/comm/platform/eventqueue/memory"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/tenancy"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/slackrelay"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc/codes"
)

// oauthModule is the published generic OAuth connector the Keycloak tests authorize.
var oauthModule = connectormanifest.Module{ConnectorID: "oauth2-http", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http", ModuleVersion: "v0.1.0"}

// TestTeamSpaceConnectionOAuthCompletionIntegration authorizes oauth2-http
// v0.1.0 connections through the actual gRPC API against the lane's Keycloak
// (SUPERVERSE_TEST_KEYCLOAK_ORIGIN), with a scratch PostgreSQL and the lane's
// MinIO. A new connection exists only once its authorization completes:
// CreateTeamSpaceConnection refuses one, starting stores none, and neither a
// completion refused for an actor removed from the TeamSpace nor a code
// Keycloak refuses leaves one, while the attempt stays usable for the right
// code, which creates it READY. Its name then refuses another new connection.
// A reauthorization is authorized again where it takes effect: a Live usage
// by a Project bob does not build refuses his completion, and a refused code
// records AUTHORIZATION_FAILED on the connection, which keeps its credential
// until the right code replaces it; the platform's read returns an access
// token.
func TestTeamSpaceConnectionOAuthCompletionIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	keycloak := dexflowtest.KeycloakForTest(t)
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	key := []byte(strings.Repeat("x", 32))
	dao, objects := connectiondao.New(db), artifactStore(t)
	releaseClient := connectormanifest.NewReleaseHTTPClient()
	releases := connectormanifest.NewReleaseResolver(releaseClient, connectorlibrary.OfficialReleaseDownloadURL)
	providers := connections.NewProviderHTTPClient(true)
	reader := connections.NewCredentialReader(dao, objects, releases, providers, connections.PlatformClients{}, slog.Default())
	shared := connections.NewTeamSpaceConnections(dao, objects, releases, connectormanifest.NewCatalog(releaseClient, connectorlibrary.NewPublishedCatalog(releaseClient)), connections.PlatformClients{}, providers, reader, nil, true)
	usages := connections.NewUsages(dao, objects)
	b := &boundary.Boundary{Logger: slog.Default(), Accounts: &accounts.Accounts{DAO: accountdao.New(db, []string{"admin@superverse.test"}), Signer: comm.Must(accounts.NewTokenSigner(key))}, Cursors: watch.NewCursorCodec(key)}
	connection := serveLoopback(ctx, t, &Server{Boundary: b, Connections: tenancy.Connections{Connections: shared, Access: dao, StudioOrigin: keycloak.StudioOrigin,
		Unrouted: slackrelay.New(slackrelay.Config{}, dao, memory.New(eventqueue.Limits{MaxItems: 1, MaxBytes: 1, MaxItemBytes: 1, MaxKeyItems: 1, Concurrency: 1}, slog.Default()), nil, nil, slog.Default())}})
	api, accountAPI := pb.NewTeamSpaceConnectionServiceClient(connection), pb.NewAccountServiceClient(connection)

	id := flowid.RandomID(10)
	alice, _ := registerNamedCaller(ctx, t, accountAPI, "alice-"+id)
	bob, _ := registerNamedCaller(ctx, t, accountAPI, "bob-"+id)
	dave, _ := registerNamedCaller(ctx, t, accountAPI, "dave-"+id)
	teamSpaceID, bobsProject, davesProject := flowid.RandomID(8), "p"+flowid.RandomID(7), "p"+flowid.RandomID(7)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	exec(`INSERT INTO team_spaces (team_space_id, name, team_space_status, creator_account_id, revision, created_at, updated_at)
		VALUES ($1, 'Team', $2, $3, 1, $4, $4)`, teamSpaceID, string(teamspaces.StatusReady), alice.GetAccountId(), now)
	addMember := func(caller *pb.CallerContext, role teamspaces.Role) {
		exec(`INSERT INTO team_space_members (team_space_id, account_id, role, added_at, added_by_account_id) VALUES ($1, $2, $3, $4, $5)`,
			teamSpaceID, caller.GetAccountId(), string(role), now, alice.GetAccountId())
	}
	addMember(alice, teamspaces.RoleAdmin)
	addMember(bob, teamspaces.RoleMember)
	addMember(dave, teamspaces.RoleMember)
	for _, project := range []struct {
		projectID string
		owner     *pb.CallerContext
	}{{bobsProject, bob}, {davesProject, dave}} {
		exec(`INSERT INTO projects (project_id, team_space_id, project_status, created_at) VALUES ($1, $2, $3, $4)`, project.projectID, teamSpaceID, string(projects.StatusReady), now)
		exec(`INSERT INTO project_members (project_id, account_id, role, added_at, added_by_account_id) VALUES ($1, $2, $3, $4, $2)`, project.projectID, project.owner.GetAccountId(), string(projects.RoleOwner), now)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		for _, projectID := range []string{bobsProject, davesProject} {
			if err := usages.ReleaseConnectionUsages(cleanup, projectID); err != nil {
				t.Error(err)
			}
		}
		if _, err := usages.DeleteTeamSpaceConnections(cleanup, teamSpaceID); err != nil {
			t.Error(err)
		}
	})

	realm := keycloak.Origin + dexflowtest.KeycloakRealmForTest + "/protocol/openid-connect"
	settings := comm.Must(json.Marshal(map[string]any{"authorizationEndpoint": realm + "/auth", "tokenEndpoint": realm + "/token",
		"scopes": []string{"openid"}, "apiBaseUrl": realm}))
	newConnection := func(requestID string) *pb.NewTeamSpaceConnection {
		return &pb.NewTeamSpaceConnection{RequestId: requestID, Name: "Keycloak", ConnectorId: oauthModule.ConnectorID, ModulePath: oauthModule.ModulePath,
			Version: oauthModule.ModuleVersion, SettingsJson: string(settings)}
	}
	_, err := api.CreateTeamSpaceConnection(ctx, &pb.CreateTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, RequestId: "create-" + id, Name: "Keycloak",
		ConnectorId: oauthModule.ConnectorID, ModulePath: oauthModule.ModulePath, Version: oauthModule.ModuleVersion, SettingsJson: string(settings), Caller: bob})
	requireCode(t, err, codes.InvalidArgument, "the creation of an OAuth connection before its authorization")
	authorize := func(start *pb.StartTeamSpaceConnectionOAuthRequest) (state, code string) {
		t.Helper()
		started, err := api.StartTeamSpaceConnectionOAuth(ctx, start)
		if err != nil {
			t.Fatalf("bob starts the authorization: %v", err)
		}
		redirect := keycloak.AuthorizeForTest(t, started.GetAuthorizationUrl())
		if !strings.HasPrefix(redirect.String(), keycloak.StudioOrigin+"/api/connector-oauth/callback") {
			t.Fatalf("Keycloak returned to %s, not to Studio's callback", redirect.Host)
		}
		return redirect.Query().Get("state"), redirect.Query().Get("code")
	}
	complete := func(caller *pb.CallerContext, state, code string) (*pb.CompleteTeamSpaceConnectionOAuthResponse, error) {
		return api.CompleteTeamSpaceConnectionOAuth(ctx, &pb.CompleteTeamSpaceConnectionOAuthRequest{State: state, Code: code, Caller: caller})
	}
	connectionID := flowid.TeamSpaceConnectionCreationID(bob.GetAccountId(), "oauth-"+id)
	absent := func(what string) {
		t.Helper()
		_, err := api.GetTeamSpaceConnection(ctx, &pb.GetTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, ConnectionId: connectionID, Caller: alice})
		requireCode(t, err, codes.NotFound, what+" left a connection")
	}

	state, code := authorize(&pb.StartTeamSpaceConnectionOAuthRequest{TeamSpaceId: teamSpaceID, NewConnection: newConnection("oauth-" + id),
		ClientId: "superverse-e2e", ClientSecret: keycloak.ClientSecret, Caller: bob})
	absent("starting the authorization of a new connection")
	exec(`DELETE FROM team_space_members WHERE team_space_id = $1 AND account_id = $2`, teamSpaceID, bob.GetAccountId())
	_, err = complete(bob, state, code)
	requireCode(t, err, codes.PermissionDenied, "a completion by an actor removed from the TeamSpace")
	absent("the refused completion of a removed member")
	addMember(bob, teamspaces.RoleMember)
	if _, err = complete(bob, state, "not-the-code"); err == nil {
		t.Fatal("a refused code completed the authorization")
	}
	absent("a code Keycloak refuses")
	created, err := complete(bob, state, code)
	if err != nil || created.GetConnection().GetConnectionId() != connectionID || created.GetConnection().GetCredentialStatus() != pb.CredentialStatus_CREDENTIAL_STATUS_READY {
		t.Fatalf("bob completes the new connection's authorization: %v", err)
	}
	_, err = api.StartTeamSpaceConnectionOAuth(ctx, &pb.StartTeamSpaceConnectionOAuthRequest{TeamSpaceId: teamSpaceID, NewConnection: newConnection("again-" + id),
		ClientId: "superverse-e2e", ClientSecret: keycloak.ClientSecret, Caller: bob})
	requireCode(t, err, codes.AlreadyExists, "a new connection under the created one's name")

	read := func() *pb.TeamSpaceConnectionSnapshot {
		t.Helper()
		got, err := api.GetTeamSpaceConnection(ctx, &pb.GetTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, ConnectionId: connectionID, Caller: alice})
		if err != nil {
			t.Fatal(err)
		}
		return got.GetConnection()
	}
	first := read()
	unchanged := func(what string) {
		t.Helper()
		if got := read(); got.GetCredentialStatus() != pb.CredentialStatus_CREDENTIAL_STATUS_READY || got.GetCredentialRevision() != first.GetCredentialRevision() {
			t.Fatalf("%s left the connection %v at credential revision %d", what, got.GetCredentialStatus(), got.GetCredentialRevision())
		}
	}
	state, code = authorize(&pb.StartTeamSpaceConnectionOAuthRequest{TeamSpaceId: teamSpaceID, ConnectionId: connectionID, Version: oauthModule.ModuleVersion, Caller: bob})
	for _, projectID := range []string{bobsProject, davesProject} {
		if err = usages.EnsureConnectionUsages(ctx, teamSpaceID, projectID, []string{connectionID}); err != nil {
			t.Fatal(err)
		}
	}
	_, err = complete(bob, state, code)
	requireCode(t, err, codes.PermissionDenied, "a reauthorization of a connection a Project bob does not build uses on Live")
	unchanged("the refused reauthorization under another builder's Live usage")
	if err = usages.ReleaseConnectionUsages(ctx, davesProject); err != nil {
		t.Fatal(err)
	}

	// A code Keycloak refuses records why on the connection and keeps the attempt for the right code.
	if _, err = complete(bob, state, "not-the-code"); err == nil {
		t.Fatal("a refused code completed the reauthorization")
	}
	unchanged("a refused code")
	if failed := read(); failed.GetLastFailure().GetCode() != "AUTHORIZATION_FAILED" || strings.Contains(failed.GetLastFailure().GetMessage(), keycloak.ClientSecret) {
		t.Fatalf("the refused code recorded %v", failed.GetLastFailure().GetCode())
	}
	if _, err = complete(bob, state, code); err != nil {
		t.Fatalf("bob completes the reauthorization once he may change the credential: %v", err)
	}
	if ready := read(); ready.GetCredentialStatus() != pb.CredentialStatus_CREDENTIAL_STATUS_READY || ready.GetLastFailure() != nil ||
		ready.GetCredentialRevision() <= first.GetCredentialRevision() {
		t.Fatalf("the reauthorized connection is %v at credential revision %d with failure %v", ready.GetCredentialStatus(), ready.GetCredentialRevision(), ready.GetLastFailure())
	}
	credential, err := reader.ReadConnection(ctx, connectionID)
	if err != nil || len(credential.Values["access_token"]) < 3 {
		t.Fatalf("the platform's read of the authorized connection: %v", err)
	}
	t.Logf("TeamSpace %s: a new connection only once authorized; its reauthorization refused under a foreign Live usage, then READY", teamSpaceID)
}
