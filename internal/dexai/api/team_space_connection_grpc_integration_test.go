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
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
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
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
)

// openaiModule is the published openai release whose one unnamed method takes
// an API key and lists its host-kept credentials.
var openaiModule = connectormanifest.Module{ConnectorID: "openai", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/openai", ModuleVersion: "v0.21.1"}

// TestTeamSpaceConnectionServiceIntegration drives TeamSpaceConnectionService
// through the actual gRPC API over a scratch PostgreSQL, the lane's MinIO and
// the published connector catalog with its logos and the openai release. The test writes the
// read model's TeamSpace and Project rows as Dex would. It covers reads only
// for members, management and the editor's forms only for an admin or a
// Project builder, creation
// replays and the taken name, a key that no response returns, the credential
// rule (a key replacement needs an admin or a builder of every Project whose
// Live version uses the connection, and a DELETED Project does not count),
// the stale revision's REVISION_CHANGED, keyset paging, the refused deletion
// while in use and the READY TeamSpace that every change needs. The catalog marks
// the connectors whose connections are tied to one address (endpoint_bound).
func TestTeamSpaceConnectionServiceIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	db := postgrestest.MigratedScratchDatabaseForTest(t)
	key := []byte(strings.Repeat("x", 32))
	dao, objects := connectiondao.New(db), artifactStore(t)
	releaseClient := connectormanifest.NewReleaseHTTPClient()
	releases := connectormanifest.NewReleaseResolver(releaseClient, connectorlibrary.OfficialReleaseDownloadURL)
	providers := connections.NewProviderHTTPClient(true)
	reader := connections.NewCredentialReader(dao, objects, releases, providers, connections.PlatformClients{}, slog.Default())
	shared := connections.NewTeamSpaceConnections(dao, objects, releases, connectormanifest.NewCatalog(releaseClient, connectorlibrary.NewPublishedCatalog(releaseClient)), connections.PlatformClients{}, providers, reader, key, true)
	usages := connections.NewUsages(dao, objects)
	b := &boundary.Boundary{Logger: slog.Default(), Accounts: &accounts.Accounts{DAO: accountdao.New(db, []string{"admin@superverse.test"}), Signer: comm.Must(accounts.NewTokenSigner(key))}, Cursors: watch.NewCursorCodec(key)}
	connection := serveLoopback(ctx, t, &Server{Boundary: b, Connections: tenancy.Connections{Connections: shared, Access: dao, StudioOrigin: "http://127.0.0.1:3000",
		Unrouted: slackrelay.New(slackrelay.Config{}, dao, memory.New(eventqueue.Limits{MaxItems: 1, MaxBytes: 1, MaxItemBytes: 1, MaxKeyItems: 1, Concurrency: 1}, slog.Default()), nil, nil, slog.Default())}})
	api, accountAPI := pb.NewTeamSpaceConnectionServiceClient(connection), pb.NewAccountServiceClient(connection)

	id := flowid.RandomID(10)
	alice, _ := registerNamedCaller(ctx, t, accountAPI, "alice-"+id)
	bob, _ := registerNamedCaller(ctx, t, accountAPI, "bob-"+id)
	carol, _ := registerNamedCaller(ctx, t, accountAPI, "carol-"+id)
	dave, _ := registerNamedCaller(ctx, t, accountAPI, "dave-"+id)
	eve, _ := registerNamedCaller(ctx, t, accountAPI, "eve-"+id)
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
	for _, member := range []struct {
		caller *pb.CallerContext
		role   teamspaces.Role
	}{{alice, teamspaces.RoleAdmin}, {bob, teamspaces.RoleMember}, {carol, teamspaces.RoleMember}, {dave, teamspaces.RoleMember}} {
		exec(`INSERT INTO team_space_members (team_space_id, account_id, role, added_at, added_by_account_id) VALUES ($1, $2, $3, $4, $5)`,
			teamSpaceID, member.caller.GetAccountId(), string(member.role), now, alice.GetAccountId())
	}
	for _, project := range []struct {
		projectID string
		owner     *pb.CallerContext
		role      string
	}{{bobsProject, bob, string(projects.RoleOwner)}, {davesProject, dave, string(projects.RoleBuilder)}} {
		exec(`INSERT INTO projects (project_id, team_space_id, project_status, created_at) VALUES ($1, $2, $3, $4)`, project.projectID, teamSpaceID, string(projects.StatusReady), now)
		exec(`INSERT INTO project_members (project_id, account_id, role, added_at, added_by_account_id) VALUES ($1, $2, $3, $4, $2)`, project.projectID, project.owner.GetAccountId(), project.role, now)
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

	catalog, err := api.ListConnectorCatalog(ctx, &pb.ListConnectorCatalogRequest{Caller: eve})
	if err != nil || !slices.ContainsFunc(catalog.GetConnectors(), func(c *pb.ConnectorCatalogEntry) bool { return c.GetConnectorId() == openaiModule.ConnectorID }) {
		t.Fatalf("an authenticated caller lists the catalog: %d entries: %v", len(catalog.GetConnectors()), err)
	}
	// A connector whose connections are tied to one address says so, with why; openai's do not.
	entry := func(connectorID string) *pb.ConnectorCatalogEntry {
		index := slices.IndexFunc(catalog.GetConnectors(), func(c *pb.ConnectorCatalogEntry) bool { return c.GetConnectorId() == connectorID })
		if index < 0 {
			t.Fatalf("the catalog lists no %s connector", connectorID)
		}
		return catalog.GetConnectors()[index]
	}
	if stripe, openai := entry("stripe"), entry(openaiModule.ConnectorID); !stripe.GetEndpointBound() || !strings.Contains(stripe.GetEndpointBoundReason(), "one address") ||
		openai.GetEndpointBound() || openai.GetEndpointBoundReason() != "" {
		t.Fatalf("the catalog marks stripe endpoint_bound=%t and openai endpoint_bound=%t", stripe.GetEndpointBound(), openai.GetEndpointBound())
	}
	// The library publishes openai's logo at its release tag; a read that ran out of its logo budget leaves
	// the rest to the next read.
	logo := func() string {
		index := slices.IndexFunc(catalog.GetConnectors(), func(c *pb.ConnectorCatalogEntry) bool { return c.GetConnectorId() == openaiModule.ConnectorID })
		return catalog.GetConnectors()[index].GetLogoSvg()
	}
	for read := 1; !strings.Contains(logo(), "<svg") && read < 3; read++ {
		if catalog, err = api.ListConnectorCatalog(ctx, &pb.ListConnectorCatalogRequest{Caller: eve}); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(logo(), "<svg") || strings.Contains(strings.ToLower(logo()), "<script") {
		t.Fatalf("the catalog's openai entry has no SVG logo")
	}
	_, err = api.ListTeamSpaceConnections(ctx, &pb.ListTeamSpaceConnectionsRequest{TeamSpaceId: teamSpaceID, Caller: eve})
	requireCode(t, err, codes.PermissionDenied, "a non-member's list")
	create := func(caller *pb.CallerContext, requestID, name, apiKey string) (*pb.CreateTeamSpaceConnectionResponse, error) {
		return api.CreateTeamSpaceConnection(ctx, &pb.CreateTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, RequestId: requestID, Name: name,
			ConnectorId: openaiModule.ConnectorID, ModulePath: openaiModule.ModulePath, Version: openaiModule.ModuleVersion, CredentialsJson: `{"api_key":"` + apiKey + `"}`, Caller: caller})
	}
	firstKey := "sk-first-" + flowid.RandomID(16)
	_, err = create(carol, "carol-"+id, "by-carol", firstKey)
	requireCode(t, err, codes.PermissionDenied, "a member who builds no Project creates a connection")
	_, err = api.StartTeamSpaceConnectionUiSession(ctx, &pb.StartTeamSpaceConnectionUiSessionRequest{TeamSpaceId: teamSpaceID,
		ConnectorId: openaiModule.ConnectorID, ModulePath: openaiModule.ModulePath, Version: openaiModule.ModuleVersion, Caller: carol})
	requireCode(t, err, codes.PermissionDenied, "a member who builds no Project opens the editor's form")
	_, err = api.CreateTeamSpaceConnection(ctx, &pb.CreateTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, RequestId: "bad-" + id, Name: "bad",
		ConnectorId: openaiModule.ConnectorID, ModulePath: openaiModule.ModulePath, Version: openaiModule.ModuleVersion, CredentialsJson: `["sk"]`, Caller: bob})
	requireCode(t, err, codes.InvalidArgument, "credentials that are not a JSON object")
	created, err := create(bob, "bob-"+id, "openai-main", firstKey)
	if err != nil {
		t.Fatalf("bob, the owner of a Project, creates a connection: %v", err)
	}
	connectionID := created.GetConnectionId()
	if again, againErr := create(bob, "bob-"+id, "openai-main", firstKey); againErr != nil || again.GetConnectionId() != connectionID {
		t.Fatalf("a replay of the creation answered %v: %v", again, againErr)
	}
	_, err = create(bob, "bob-"+id, "openai-renamed", firstKey)
	requireCode(t, err, codes.AlreadyExists, "the creation's request ID with another name")
	_, err = create(alice, "alice-"+id, "openai-main", firstKey)
	requireReason(t, err, codes.AlreadyExists, connections.ReasonConnectionNameTaken)

	got, err := api.GetTeamSpaceConnection(ctx, &pb.GetTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, ConnectionId: connectionID, Caller: carol})
	if err != nil || got.GetConnection().GetCredentialStatus() != pb.CredentialStatus_CREDENTIAL_STATUS_READY ||
		got.GetConnection().GetCreatorAccountId() != bob.GetAccountId() || !slices.Contains(got.GetStoredCredentialFields(), "api_key") {
		t.Fatalf("carol, a member, reads the connection: %v: %v", got, err)
	}
	if strings.Contains(prototext.Format(got), firstKey) {
		t.Fatal("the read returned the API key")
	}

	// Both Projects' Live versions use the connection.
	for _, projectID := range []string{bobsProject, davesProject} {
		if err = usages.EnsureConnectionUsages(ctx, teamSpaceID, projectID, []string{connectionID}); err != nil {
			t.Fatal(err)
		}
	}
	replaceKey := func(caller *pb.CallerContext, credentialRevision uint64) (*pb.UpdateTeamSpaceConnectionResponse, error) {
		return api.UpdateTeamSpaceConnection(ctx, &pb.UpdateTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, ConnectionId: connectionID,
			ExpectedCredentialRevision: credentialRevision, CredentialsJson: `{"api_key":"sk-new-` + flowid.RandomID(16) + `"}`, Caller: caller})
	}
	revision := got.GetConnection().GetCredentialRevision()
	_, err = replaceKey(bob, revision)
	requireCode(t, err, codes.PermissionDenied, "bob replaces the key that dave's Live version uses")
	_, err = replaceKey(dave, revision)
	requireCode(t, err, codes.PermissionDenied, "dave replaces the key that bob's Live version uses")
	replaced, err := replaceKey(alice, revision)
	if err != nil || replaced.GetConnection().GetCredentialRevision() <= revision || len(replaced.GetConnection().GetUsages()) != 2 {
		t.Fatalf("alice, a TeamSpace admin, replaces the key: %v: %v", replaced, err)
	}
	exec(`UPDATE projects SET project_status = $2 WHERE project_id = $1`, davesProject, string(projects.StatusDeleted))
	if replaced, err = replaceKey(bob, replaced.GetConnection().GetCredentialRevision()); err != nil {
		t.Fatalf("bob replaces the key once only his Project's Live version uses it: %v", err)
	}
	_, err = api.UpdateTeamSpaceConnection(ctx, &pb.UpdateTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, ConnectionId: connectionID,
		ExpectedRevision: replaced.GetConnection().GetRevision() - 1, SettingsJson: `{}`, Caller: bob})
	requireRevisionChanged(t, err)

	if _, err = create(alice, "alice-second-"+id, "openai-second", "sk-second-"+flowid.RandomID(16)); err != nil {
		t.Fatalf("alice creates a second connection: %v", err)
	}
	var names []string
	request := &pb.ListTeamSpaceConnectionsRequest{TeamSpaceId: teamSpaceID, PageSize: 1, Caller: carol}
	for range 3 {
		page, listErr := api.ListTeamSpaceConnections(ctx, request)
		if listErr != nil || len(page.GetConnections()) != 1 {
			t.Fatalf("carol lists a page of one: %v: %v", page, listErr)
		}
		names = append(names, page.GetConnections()[0].GetName())
		if request.PageToken = page.GetNextPageToken(); request.PageToken == "" {
			break
		}
	}
	if !slices.Equal(names, []string{"openai-second", "openai-main"}) {
		t.Fatalf("the pages list %v, newest first", names)
	}

	deleteConnection := func(caller *pb.CallerContext) error {
		current, readErr := api.GetTeamSpaceConnection(ctx, &pb.GetTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, ConnectionId: connectionID, Caller: caller})
		if readErr != nil {
			t.Fatal(readErr)
		}
		_, deleteErr := api.DeleteTeamSpaceConnection(ctx, &pb.DeleteTeamSpaceConnectionRequest{TeamSpaceId: teamSpaceID, ConnectionId: connectionID,
			ExpectedRevision: current.GetConnection().GetRevision(), Caller: caller})
		return deleteErr
	}
	requireReason(t, deleteConnection(bob), codes.FailedPrecondition, connections.ReasonConnectionInUse)
	if err = usages.ReleaseConnectionUsages(ctx, bobsProject); err != nil {
		t.Fatal(err)
	}
	if err = deleteConnection(bob); err != nil {
		t.Fatalf("bob deletes the connection that only a DELETED Project used: %v", err)
	}
	if listed, listErr := api.ListTeamSpaceConnections(ctx, &pb.ListTeamSpaceConnectionsRequest{TeamSpaceId: teamSpaceID, Caller: bob}); listErr != nil || len(listed.GetConnections()) != 1 {
		t.Fatalf("bob lists the remaining connection: %v: %v", listed, listErr)
	}

	exec(`UPDATE team_spaces SET team_space_status = $2 WHERE team_space_id = $1`, teamSpaceID, string(teamspaces.StatusDeleting))
	_, err = create(alice, "alice-late-"+id, "openai-late", "sk-late-"+flowid.RandomID(16))
	requireCode(t, err, codes.FailedPrecondition, "a creation in a TeamSpace being deleted")
	t.Logf("TeamSpace %s: member reads, builder management, replays, the hidden key, the Live credential rule, REVISION_CHANGED, paging, in-use deletion, READY changes", teamSpaceID)
}

// requireReason requires code with the single ErrorInfo reason.
func requireReason(t *testing.T, err error, code codes.Code, reason connections.RefusalReason) {
	t.Helper()
	requireCode(t, err, code, reason.String())
	requireRemedy(t, err, reason.String(), boundary.Enum[pb.FailureRemedy](reason.Remedy().String()))
}

// requireRemedy fails unless err carries the ErrorInfo reason with the remedy
// it declares in the metadata Studio reads.
func requireRemedy(t *testing.T, err error, reason string, remedy pb.FailureRemedy) {
	t.Helper()
	for _, detail := range status.Convert(err).Details() {
		if info, isInfo := detail.(*errdetails.ErrorInfo); isInfo && info.GetReason() == reason {
			if got := info.GetMetadata()[boundary.RemedyMetadataKeyForTest]; got != remedy.String() {
				t.Fatalf("%v carries the remedy %q, want %s", err, got, remedy)
			}
			return
		}
	}
	t.Fatalf("%v carries no ErrorInfo %s", err, reason)
}
