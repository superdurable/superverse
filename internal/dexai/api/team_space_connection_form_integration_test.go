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
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/comm/signedtoken"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/api/tenancy"
	"github.com/superdurable/superversev2/internal/dexai/api/watch"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestTeamSpaceConnectionFormAcrossReplicasIntegration serves the
// TeamSpaceConnectionService from two servers over one scratch PostgreSQL,
// the lane's MinIO and the published openai release, as two replicas with the
// same signing key. A form opened on one runs its commands on the other. Its
// nonce is refused when another account replays it, against another
// TeamSpace's connection, after its expiry, after tampering, when signed for
// another purpose and when signed with another key.
func TestTeamSpaceConnectionFormAcrossReplicasIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	key := []byte(strings.Repeat("k", 32))
	dao, objects := connectiondao.New(db), artifactStore(t)
	replica := func() (pb.TeamSpaceConnectionServiceClient, pb.AccountServiceClient) {
		releaseClient := connectormanifest.NewReleaseHTTPClient()
		releases := connectormanifest.NewReleaseResolver(releaseClient, connectorlibrary.OfficialReleaseDownloadURL)
		providers := connections.NewProviderHTTPClient(true)
		reader := connections.NewCredentialReader(dao, objects, releases, providers, connections.PlatformClients{}, slog.Default())
		shared := connections.NewTeamSpaceConnections(dao, objects, releases, connectormanifest.NewCatalog(releaseClient, connectorlibrary.NewPublishedCatalog(releaseClient)), connections.PlatformClients{},
			providers, reader, key, true)
		b := &boundary.Boundary{Logger: slog.Default(), Accounts: &accounts.Accounts{DAO: accountdao.New(db, "admin@superverse.test"), Signer: comm.Must(accounts.NewTokenSigner(key))}, Cursors: watch.NewCursorCodec(key)}
		connection := serveLoopback(ctx, t, &Server{Boundary: b, Connections: tenancy.Connections{Connections: shared, Access: dao, StudioOrigin: "http://127.0.0.1:3000"}})
		return pb.NewTeamSpaceConnectionServiceClient(connection), pb.NewAccountServiceClient(connection)
	}
	first, accountAPI := replica()
	second, _ := replica()

	id := flowid.RandomID(10)
	alice, _ := registerNamedCaller(ctx, t, accountAPI, "form-alice-"+id)
	bob, _ := registerNamedCaller(ctx, t, accountAPI, "form-bob-"+id)
	usages := connections.NewUsages(dao, objects)
	now := time.Now().UTC()
	teamSpaces := []string{flowid.RandomID(8), flowid.RandomID(8)}
	connectionIDs := make([]string, len(teamSpaces))
	for index, teamSpaceID := range teamSpaces {
		if _, err := db.ExecContext(ctx, `INSERT INTO team_spaces (team_space_id, name, team_space_status, creator_account_id, revision, created_at, updated_at)
			VALUES ($1, 'Team', $2, $3, 1, $4, $4)`, teamSpaceID, string(teamspaces.StatusReady), alice.GetAccountId(), now); err != nil {
			t.Fatal(err)
		}
		for _, member := range []*pb.CallerContext{alice, bob} {
			if _, err := db.ExecContext(ctx, `INSERT INTO team_space_members (team_space_id, account_id, role, added_at, added_by_account_id) VALUES ($1, $2, $3, $4, $5)`,
				teamSpaceID, member.GetAccountId(), string(teamspaces.RoleAdmin), now, alice.GetAccountId()); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() {
			cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
			defer done()
			if _, err := usages.DeleteTeamSpaceConnections(cleanup, teamSpaceID); err != nil {
				t.Error(err)
			}
		})
		created, err := first.CreateTeamSpaceConnection(ctx, &pb.CreateTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, RequestId: "form-" + teamSpaceID, Name: "openai-main",
			ConnectorId: openaiModule.ConnectorID, ModulePath: openaiModule.ModulePath, Version: openaiModule.ModuleVersion,
			CredentialsJson: `{"api_key":"sk-form-` + flowid.RandomID(16) + `"}`, Caller: alice})
		if err != nil {
			t.Fatalf("alice creates the connection of TeamSpace %d: %v", index, err)
		}
		connectionIDs[index] = created.GetConnectionId()
	}

	form, err := first.StartTeamSpaceConnectionUiSession(ctx, &pb.StartTeamSpaceConnectionUiSessionRequest{TeamSpaceId: teamSpaces[0], ConnectionId: connectionIDs[0],
		ConnectorId: openaiModule.ConnectorID, ModulePath: openaiModule.ModulePath, Version: openaiModule.ModuleVersion, Caller: alice})
	if err != nil {
		t.Fatalf("alice opens the form on the first replica: %v", err)
	}
	// A command the release does not declare passes the nonce's checks and
	// stops before any provider call; a refused nonce stops at the first check.
	run := func(api pb.TeamSpaceConnectionServiceClient, caller *pb.CallerContext, teamSpaceID, nonce string) error {
		_, err := api.InvokeTeamSpaceConnectionCommand(ctx, &pb.InvokeTeamSpaceConnectionCommandRequest{TeamSpaceId: teamSpaceID, SessionNonce: nonce,
			CommandId: "notDeclared", ParametersJson: "{}", Caller: caller})
		return err
	}
	accepted := func(err error, what string) {
		t.Helper()
		if status.Code(err) != codes.InvalidArgument || !strings.Contains(status.Convert(err).Message(), "declares no Studio command") {
			t.Fatalf("%s: the nonce was not accepted: %v", what, err)
		}
	}
	refused := func(err error, what string) {
		t.Helper()
		if status.Code(err) != codes.InvalidArgument || !strings.Contains(status.Convert(err).Message(), "missing or expired") {
			t.Fatalf("%s: the nonce was not refused: %v", what, err)
		}
	}
	accepted(run(second, alice, teamSpaces[0], form.GetSessionNonce()), "the second replica runs the first replica's form")
	accepted(run(first, alice, teamSpaces[0], form.GetSessionNonce()), "the first replica runs its own form")
	refused(run(second, bob, teamSpaces[0], form.GetSessionNonce()), "another account replays the form")
	refused(run(second, alice, teamSpaces[1], form.GetSessionNonce()), "the form against another TeamSpace's connection")

	claims := map[string]any{"actor": alice.GetAccountId(), "teamSpace": teamSpaces[0], "connection": connectionIDs[0], "release": openaiModule}
	forms := signedtoken.New(key, signedtoken.ConnectionForm)
	accepted(run(second, alice, teamSpaces[0], forms.Seal(claims, time.Now().Add(time.Minute))), "a nonce the test seals with the key")
	refused(run(second, alice, teamSpaces[0], forms.Seal(claims, time.Now().Add(-time.Second))), "an expired nonce")
	refused(run(second, alice, teamSpaces[0], signedtoken.New(key, signedtoken.ConnectorUISession).Seal(claims, time.Now().Add(time.Minute))), "a nonce of another purpose")
	refused(run(second, alice, teamSpaces[0], signedtoken.New([]byte(strings.Repeat("o", 32)), signedtoken.ConnectionForm).Seal(claims, time.Now().Add(time.Minute))),
		"a nonce signed with another key")
	// The connection is inside the MAC: moving the nonce to the other
	// TeamSpace's connection keeps the signature and fails it.
	_, signature, _ := strings.Cut(form.GetSessionNonce(), ".")
	moved := map[string]any{"exp": time.Now().Add(time.Minute).Unix(),
		"claims": map[string]any{"actor": alice.GetAccountId(), "teamSpace": teamSpaces[1], "connection": connectionIDs[1], "release": openaiModule}}
	tampered := base64.RawURLEncoding.EncodeToString(comm.Must(json.Marshal(moved))) + "." + signature
	refused(run(second, alice, teamSpaces[1], tampered), "a nonce whose claims were changed")
}
