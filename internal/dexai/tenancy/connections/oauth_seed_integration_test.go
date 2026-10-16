// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package connections

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// TestOAuthSeedKeepsNoPlatformSecretIntegration starts authorizations that
// create a gmail v0.23.0 connection with the platform's OAuth application,
// over the lane's PostgreSQL and MinIO, the published release and Google's
// token endpoint. Starting stores no connection; the seed holds the creation,
// names the platform source and holds no client secret, which the callback
// takes from the platform's configuration. A second attempt deletes the
// first, whose callback is then refused as unknown. The provider's refusal
// and a code Google refuses leave no connection, and a name another
// connection took meanwhile refuses a new start.
func TestOAuthSeedKeepsNoPlatformSecretIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	dao := connectiondao.New(db)
	objects := artifactStore(ctx, t)
	releaseClient := connectormanifest.NewReleaseHTTPClient()
	releases := connectormanifest.NewReleaseResolver(releaseClient, connectorlibrary.OfficialReleaseDownloadURL)
	gmail := connectormanifest.Module{ConnectorID: "gmail", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/google/gmail", ModuleVersion: "v0.23.0"}
	release, err := releases.Resolve(ctx, gmail)
	if err != nil {
		t.Fatal(err)
	}
	method, _ := release.Manifest.Spec.Auth.Method("google-oauth")
	secret := "platform-secret-" + flowid.RandomID(24)
	clients := PlatformClients{platformClientKey("gmail", method.ID): {ClientID: "gmail-platform-client", clientSecret: secret,
		Scopes: append(append([]string{}, method.OAuth2.Scopes.List...), method.OAuth2.UserScopes...)}}
	providers := NewProviderHTTPClient(true)
	reader := NewCredentialReader(dao, objects, releases, providers, clients, slog.Default())
	shared := NewTeamSpaceConnections(dao, objects, releases, connectormanifest.NewCatalog(releaseClient), clients, providers, reader, nil, true)
	teamSpaceID, account := "ts"+flowid.RandomID(10), flowid.RandomID(32)
	registerAccount(ctx, t, db, account, teamSpaceID)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if _, err := NewUsages(dao, objects).DeleteTeamSpaceConnections(cleanup, teamSpaceID); err != nil {
			t.Error(err)
		}
	})
	creation := &Creation{TeamSpaceID: teamSpaceID, RequestID: "platform", ActorAccountID: account, Name: "gmail",
		Release: gmail, AuthMethodID: method.ID, ClientSource: connectiondao.ClientPlatform}
	if _, err = shared.Create(ctx, *creation); !errors.Is(err, ErrInvalid) {
		t.Fatalf("an OAuth connection was created before its authorization: %v", err)
	}
	connectionID := flowid.TeamSpaceConnectionCreationID(account, creation.RequestID)
	absent := func(what string) {
		t.Helper()
		if _, err := dao.Connection(ctx, teamSpaceID, connectionID); !errors.Is(err, connectiondao.ErrNotFound) {
			t.Fatalf("%s left a connection: %v", what, err)
		}
	}
	redirectURI := "http://localhost:3000/api/connector-oauth/callback"
	start := func() string {
		authorization, _, err := shared.StartOAuth(ctx, OAuthStart{TeamSpaceID: teamSpaceID, ActorAccountID: account, Creation: creation, RedirectURI: redirectURI})
		if err != nil {
			t.Fatalf("start the authorization: %v", err)
		}
		return comm.Must(url.Parse(authorization)).Query().Get("state")
	}
	seedKey := func(state string) string {
		teamSpace, connection, attempt, valid := parseTeamSpaceState(state)
		if !valid {
			t.Fatalf("the authorization's state is invalid")
		}
		return oauthAttemptPrefix(teamSpace, connection, oauthAttempt(attempt)) + "seed"
	}

	first := start()
	absent("starting the authorization of a new connection")
	var stored json.RawMessage
	if _, err = objects.GetCurrentJSON(ctx, seedKey(first), &stored); err != nil {
		t.Fatal(err)
	}
	var seed oauthSeed
	if err = json.Unmarshal(stored, &seed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(secret)) || seed.ClientSecret != "" || seed.ClientSource != connectiondao.ClientPlatform || seed.ClientID != "gmail-platform-client" {
		t.Fatalf("the seed of a platform client holds its secret or misses its source")
	}
	if seed.ConnectionID != connectionID || seed.Creation == nil || seed.Creation.Name != "gmail" {
		t.Fatalf("the seed of a new connection misses its creation")
	}

	second := start()
	if _, err = objects.GetCurrentJSON(ctx, seedKey(first), &stored); !errors.Is(err, objectstore.ErrNotFound) {
		t.Fatalf("the earlier attempt's seed survived a new attempt: %v", err)
	}
	if _, err = objects.GetCurrentJSON(ctx, seedKey(second), &stored); err != nil {
		t.Fatalf("the new attempt's seed: %v", err)
	}
	allowed := func(context.Context, string, string) error { return nil }
	if _, err = shared.CompleteOAuth(ctx, OAuthCallback{ActorAccountID: account, State: first, Code: "code", RedirectURI: redirectURI,
		MayChangeCredential: allowed}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("the callback of a voided attempt: %v", err)
	}

	if _, err = shared.CompleteOAuth(ctx, OAuthCallback{ActorAccountID: account, State: second, ProviderError: "access_denied", RedirectURI: redirectURI,
		MayChangeCredential: allowed}); !errors.Is(err, errOAuthDenied) {
		t.Fatalf("the provider's refusal: %v", err)
	}
	absent("the provider's refusal")
	if _, err = shared.CompleteOAuth(ctx, OAuthCallback{ActorAccountID: account, State: second, Code: "not-a-code", RedirectURI: redirectURI,
		MayChangeCredential: allowed}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a code Google refuses: %v", err)
	}
	absent("a refused code")

	now := time.Now().UTC()
	if _, _, err = dao.Create(ctx, connectiondao.Connection{TeamSpaceID: teamSpaceID, ConnectionID: "c" + flowid.RandomID(11), CreationFingerprint: "taken",
		Name: "gmail", ConnectorID: gmail.ConnectorID, ModulePath: gmail.ModulePath, VersionLine: connectormanifest.VersionLine(gmail.ModuleVersion),
		ManifestVersion: gmail.ModuleVersion, AuthMethodID: method.ID, ClientSource: connectiondao.ClientPlatform, Settings: json.RawMessage(`{}`),
		CredentialStatus: connectiondao.CredentialAuthorizing, ConnectionStatus: connectiondao.ConnectionActive, CreatorAccountID: account,
		UpdatedByAccountID: account, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = shared.StartOAuth(ctx, OAuthStart{TeamSpaceID: teamSpaceID, ActorAccountID: account, Creation: creation,
		RedirectURI: redirectURI}); !errors.Is(err, connectiondao.ErrNameTaken) {
		t.Fatalf("a new connection's authorization under a taken name: %v", err)
	}
}
