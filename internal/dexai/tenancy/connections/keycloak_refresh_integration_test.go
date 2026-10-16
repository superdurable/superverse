// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connections

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// TestKeycloakRefreshAndReauthorizationIntegration authorizes an oauth2-http
// v0.1.0 connection with the lane's Keycloak (SUPERVERSE_TEST_KEYCLOAK_ORIGIN),
// whose access tokens live 60 seconds and whose refresh tokens rotate, over
// the lane's PostgreSQL and MinIO. A read the provider rejected renews the
// grant once the forced-refresh floor passed: a new access token and refresh
// token at the next credential revision. Once Keycloak ends the user's
// sessions, the renewal's invalid_grant is terminal: the connection requires
// reauthorization, records why, and a read returns ErrReauthorizationRequired.
func TestKeycloakRefreshAndReauthorizationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	f := authorizedKeycloakConnection(ctx, t)
	dao, reader, teamSpaceID, created, authorized := f.dao, f.reader, f.teamSpaceID, f.authorized, f.authorized

	first, err := reader.read(ctx, authorized, 0)
	if err != nil || first.Values["access_token"] == nil || first.Values["refresh_token"] == nil {
		t.Fatalf("read the authorized credential: %v", err)
	}
	// A rejection within the forced-refresh floor renews nothing; after it, the grant renews once.
	var renewed Credential
	deadline := time.Now().Add(90 * time.Second)
	for {
		row, err := dao.Connection(ctx, teamSpaceID, created.ConnectionID)
		if err != nil {
			t.Fatal(err)
		}
		if renewed, err = reader.read(ctx, row, first.revision); err != nil {
			t.Fatalf("a read of a rejected credential: %v", err)
		}
		if renewed.revision != first.revision || time.Now().After(deadline) {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if renewed.revision <= first.revision || string(renewed.Values["access_token"]) == string(first.Values["access_token"]) ||
		string(renewed.Values["refresh_token"]) == string(first.Values["refresh_token"]) {
		t.Fatalf("the rejected credential renewed to revision %d from %d", renewed.revision, first.revision)
	}

	f.logout()
	deadline = time.Now().Add(90 * time.Second)
	for {
		row, err := dao.Connection(ctx, teamSpaceID, created.ConnectionID)
		if err != nil {
			t.Fatal(err)
		}
		if row.CredentialStatus == connectiondao.CredentialReauthorizationRequired {
			if len(row.LastFailure) == 0 {
				t.Fatal("the terminal renewal recorded no failure")
			}
			if _, err = reader.read(ctx, row, 0); !errors.Is(err, ErrReauthorizationRequired) {
				t.Fatalf("a read of a connection that requires reauthorization: %v", err)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the connection is still %s after Keycloak ended the sessions", row.CredentialStatus)
		}
		if _, err = reader.read(ctx, row, row.CredentialRevision); err != nil && !errors.Is(err, ErrReauthorizationRequired) {
			t.Fatalf("a read after the logout: %v", err)
		}
		time.Sleep(3 * time.Second)
	}
}

// keycloakConnection is an oauth2-http v0.1.0 connection that the lane's
// Keycloak authorized, over the lane's PostgreSQL and MinIO.
type keycloakConnection struct {
	logout               func()
	dao                  *connectiondao.ConnectionDAO
	reader               *CredentialReader
	shared               *TeamSpaceConnections
	teamSpaceID, account string
	realm                string
	authorized           connectiondao.Connection
}

func authorizedKeycloakConnection(ctx context.Context, t *testing.T) keycloakConnection {
	t.Helper()
	keycloak := dexflowtest.KeycloakForTest(t)
	db := postgrestest.OpenForTest(t)
	dao := connectiondao.New(db)
	objects := artifactStore(ctx, t)
	releaseClient := connectormanifest.NewReleaseHTTPClient()
	releases := connectormanifest.NewReleaseResolver(releaseClient, connectorlibrary.OfficialReleaseDownloadURL)
	providers := NewProviderHTTPClient(true)
	reader := NewCredentialReader(dao, objects, releases, providers, PlatformClients{}, slog.Default())
	shared := NewTeamSpaceConnections(dao, objects, releases, connectormanifest.NewCatalog(releaseClient, connectorlibrary.NewPublishedCatalog(releaseClient)), PlatformClients{}, providers, reader, nil, true)
	usages := NewUsages(dao, objects)
	teamSpaceID, account := flowid.RandomID(8), flowid.RandomID(32)
	registerAccount(ctx, t, db, account, teamSpaceID)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if _, err := usages.DeleteTeamSpaceConnections(cleanup, teamSpaceID); err != nil {
			t.Error(err)
		}
	})

	realm := keycloak.Origin + dexflowtest.KeycloakRealmForTest + "/protocol/openid-connect"
	text := func(value any) json.RawMessage { return comm.Must(json.Marshal(value)) }
	creation := &Creation{TeamSpaceID: teamSpaceID, RequestID: "keycloak", ActorAccountID: account, Name: "Keycloak",
		Release: connectormanifest.Module{ConnectorID: "oauth2-http", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http", ModuleVersion: "v0.1.0"},
		Settings: map[string]json.RawMessage{"authorizationEndpoint": text(realm + "/auth"), "tokenEndpoint": text(realm + "/token"),
			"scopes": text([]string{"openid"}), "apiBaseUrl": text(realm)}}
	redirectURI := keycloak.StudioOrigin + "/api/connector-oauth/callback"
	authorization, _, err := shared.StartOAuth(ctx, OAuthStart{TeamSpaceID: teamSpaceID, ActorAccountID: account, Creation: creation,
		ClientID: "superverse-e2e", ClientSecret: keycloak.ClientSecret, RedirectURI: redirectURI})
	if err != nil {
		t.Fatalf("start the authorization of the new OAuth connection: %v", err)
	}
	redirect := keycloak.AuthorizeForTest(t, authorization)
	allowed := func(context.Context, string, string) error { return nil }
	authorized, err := shared.CompleteOAuth(ctx, OAuthCallback{ActorAccountID: account, State: redirect.Query().Get("state"),
		Code: redirect.Query().Get("code"), RedirectURI: redirectURI, MayChangeCredential: allowed})
	if err != nil || authorized.CredentialStatus != connectiondao.CredentialReady {
		t.Fatalf("complete the authorization: %v", err)
	}
	return keycloakConnection{logout: func() { keycloak.LogoutUserForTest(t) }, dao: dao, reader: reader, shared: shared, teamSpaceID: teamSpaceID, account: account, realm: realm, authorized: authorized}

}

// TestGrantSettingChangeNeedsCredentialRightIntegration edits the token
// endpoint setting of a connection Keycloak authorized. Without the right to
// change the credential the edit is refused and the connection stays as it
// was; with it, the connection requires reauthorization at the next
// credential revision in the same change, so no read sends the refresh token
// or the client secret to the new endpoint. A setting no grant reads changes
// without that right.
func TestGrantSettingChangeNeedsCredentialRightIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	f := authorizedKeycloakConnection(ctx, t)
	text := func(value any) json.RawMessage { return comm.Must(json.Marshal(value)) }
	settings := func(row connectiondao.Connection, name string, value any) map[string]json.RawMessage {
		var current map[string]json.RawMessage
		comm.MustDo(json.Unmarshal(row.Settings, &current))
		current[name] = text(value)
		return current
	}
	refused := func(context.Context) (bool, error) { return false, nil }
	admitted := func(context.Context) (bool, error) { return true, nil }
	row := f.authorized

	moved := settings(row, "tokenEndpoint", "https://tokens.example.invalid/token")
	if _, err := f.shared.Update(ctx, ConnectionUpdate{TeamSpaceID: f.teamSpaceID, ConnectionID: row.ConnectionID, ActorAccountID: f.account,
		ExpectedRevision: row.Revision, Settings: moved, MayChangeCredential: refused}); !errors.Is(err, ErrCredentialChangeForbidden) {
		t.Fatalf("a token endpoint change without the credential right: %v", err)
	}
	if unchanged, err := f.dao.Connection(ctx, f.teamSpaceID, row.ConnectionID); err != nil || unchanged.Revision != row.Revision ||
		unchanged.CredentialStatus != connectiondao.CredentialReady || !sameJSON(unchanged.Settings, row.Settings) {
		t.Fatalf("the refused change left %+v: %v", unchanged, err)
	}

	other, err := f.shared.Update(ctx, ConnectionUpdate{TeamSpaceID: f.teamSpaceID, ConnectionID: row.ConnectionID, ActorAccountID: f.account,
		ExpectedRevision: row.Revision, Settings: settings(row, "apiBaseUrl", f.realm+"/userinfo"), MayChangeCredential: refused})
	if err != nil || other.CredentialStatus != connectiondao.CredentialReady || other.CredentialRevision != row.CredentialRevision {
		t.Fatalf("a setting no grant reads: %+v: %v", other, err)
	}

	changed, err := f.shared.Update(ctx, ConnectionUpdate{TeamSpaceID: f.teamSpaceID, ConnectionID: row.ConnectionID, ActorAccountID: f.account,
		ExpectedRevision: other.Revision, Settings: settings(other, "tokenEndpoint", "https://tokens.example.invalid/token"), MayChangeCredential: admitted})
	if err != nil || changed.CredentialStatus != connectiondao.CredentialReauthorizationRequired || changed.CredentialRevision != row.CredentialRevision+1 ||
		len(changed.LastFailure) == 0 {
		t.Fatalf("an admitted token endpoint change: %+v: %v", changed, err)
	}
	// The forced renewal would call the new endpoint; the read stops first.
	if _, err = f.reader.read(ctx, changed, changed.CredentialRevision); !errors.Is(err, ErrReauthorizationRequired) {
		t.Fatalf("a read after the token endpoint moved: %v", err)
	}
}
