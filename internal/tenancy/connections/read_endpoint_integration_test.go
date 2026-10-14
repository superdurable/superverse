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
	"database/sql"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects/infra"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
)

// TestReadEndpointAuthorizationIntegration serves the credential read
// endpoint on a loopback listener over the lane's PostgreSQL and MinIO and an
// actual published API key release, and proves who may read what: a scope
// token reads only the connections its scope uses, a Preview token cannot
// read a connection only Live uses, an unknown or missing token and a deleted
// Project's tokens are refused, and malformed requests are refused before any
// read. Without the usage's scope condition the Preview token would read the
// Live connection, which the test catches. The API key release openai
// v0.21.1 lists its one unnamed method as keeping nothing, so its key reads
// READY; openai v0.8.0 predates the list, so an authorized read of it is
// refused with HTTP 409 and no credential. A connection of gmail v0.23.0,
// whose release lists its OAuth client secret and refresh token as
// host-kept, reads READY without them.
func TestReadEndpointAuthorizationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db := postgrestest.OpenForTest(t)
	dao := connectiondao.New(db)
	objects := artifactStore(ctx, t)
	releases := connectormanifest.NewReleaseResolver(connectormanifest.NewReleaseHTTPClient(), connectorlibrary.OfficialReleaseDownloadURL)
	reader := NewCredentialReader(dao, objects, releases, NewProviderHTTPClient(true), PlatformClients{}, slog.Default())
	usages := NewUsages(dao, objects)
	server := httptest.NewServer(NewReadEndpoint(reader))
	t.Cleanup(server.Close)

	teamSpaceID, account, projectID := "ts"+flowid.RandomID(10), flowid.RandomID(32), "p"+flowid.RandomID(7)
	registerAccount(ctx, t, db, account, teamSpaceID)
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if err := usages.ReleaseConnectionUsages(cleanup, projectID); err != nil {
			t.Error(err)
		}
		if _, err := usages.DeleteTeamSpaceConnections(cleanup, teamSpaceID); err != nil {
			t.Error(err)
		}
	})
	preview := staticConnection(ctx, t, dao, objects, teamSpaceID, account, "preview-openai", "v0.21.1", "sk-preview-"+flowid.RandomID(12))
	live := staticConnection(ctx, t, dao, objects, teamSpaceID, account, "live-openai", "v0.21.1", "sk-live-"+flowid.RandomID(12))
	legacy := staticConnection(ctx, t, dao, objects, teamSpaceID, account, "legacy-openai", "v0.8.0", "sk-legacy-"+flowid.RandomID(12))
	gmail := gmailConnection(ctx, t, dao, objects, teamSpaceID, account)
	for _, c := range []connectiondao.Connection{preview, legacy, gmail} {
		if err := usages.addUsage(ctx, teamSpaceID, c.ConnectionID, projectID, infra.ScopePreview); err != nil {
			t.Fatal(err)
		}
	}
	if err := usages.EnsureConnectionUsages(ctx, teamSpaceID, projectID, []string{live.ConnectionID}); err != nil {
		t.Fatal(err)
	}
	previewToken, liveToken := scopeToken(ctx, t, usages, projectID, infra.ScopePreview), scopeToken(ctx, t, usages, projectID, infra.ScopeLive)

	if answer, status := postRead(t, server.URL, previewToken, preview.ConnectionID); status != http.StatusOK || answer.Status != readReady ||
		credentialText(answer.Credential, "api_key") == "" || answer.Revision != preview.CredentialRevision || answer.ExpiresAt != nil {
		t.Fatalf("the Preview token reading its connection: %d %+v", status, answer.Status)
	}
	if answer, status := postRead(t, server.URL, previewToken, legacy.ConnectionID); status != http.StatusConflict || answer.Credential != nil {
		t.Fatalf("the Preview token reading its connection of a release without host-kept credentials: %d %+v", status, answer.Status)
	}
	if answer, status := postRead(t, server.URL, previewToken, gmail.ConnectionID); status != http.StatusOK || answer.Status != readReady ||
		credentialText(answer.Credential, "access_token") == "" || credentialText(answer.Credential, "oauth_client_id") == "" ||
		answer.Credential["refresh_token"] != nil || answer.Credential["oauth_client_secret"] != nil || answer.Revision != gmail.CredentialRevision || answer.ExpiresAt == nil {
		t.Fatalf("the Preview token reading its OAuth connection: %d %s with fields %v", status, answer.Status, slices.Sorted(maps.Keys(answer.Credential)))
	}
	if answer, status := postRead(t, server.URL, previewToken, live.ConnectionID); status != http.StatusOK || answer.Status != readConnectionNotSelected || answer.Credential != nil {
		t.Fatalf("the Preview token reading the Live connection: %d %s", status, answer.Status)
	}
	if answer, status := postRead(t, server.URL, liveToken, live.ConnectionID); status != http.StatusOK || answer.Status != readReady {
		t.Fatalf("the Live token reading its connection: %d %s", status, answer.Status)
	}
	if _, status := postRead(t, server.URL, "unknown-"+previewToken, preview.ConnectionID); status != http.StatusUnauthorized {
		t.Fatalf("an unknown token: %d", status)
	}
	if _, status := postRead(t, server.URL, "", preview.ConnectionID); status != http.StatusUnauthorized {
		t.Fatalf("a missing token: %d", status)
	}
	oversized := bytes.Repeat([]byte(" "), maxReadRequestBytes+1)
	if status := post(t, server.URL+CredentialReadPath, previewToken, oversized); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized request: %d", status)
	}
	if status := post(t, server.URL+CredentialReadPath, previewToken, []byte(`{"sharedConnectionId":"x","other":1}`)); status != http.StatusBadRequest {
		t.Fatalf("a request with an unknown member: %d", status)
	}
	if response, err := http.Get(server.URL + CredentialReadPath); err != nil || response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("a GET: %v", err)
	} else {
		_ = response.Body.Close()
	}

	if err := usages.ReleaseConnectionUsages(ctx, projectID); err != nil {
		t.Fatal(err)
	}
	if _, status := postRead(t, server.URL, previewToken, preview.ConnectionID); status != http.StatusUnauthorized {
		t.Fatalf("a deleted Project's token: %d", status)
	}
}

// staticConnection creates a READY connection of a published openai
// release, whose API key no grant renews.
func staticConnection(ctx context.Context, t *testing.T, dao *connectiondao.ConnectionDAO, objects *objectstore.Store, teamSpaceID, account, name, version, apiKey string) connectiondao.Connection {
	t.Helper()
	module := connectormanifest.Module{ConnectorID: "openai", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/openai", ModuleVersion: version}
	return readyConnection(ctx, t, dao, objects, teamSpaceID, account, name, module, "",
		storedCredential{Credentials: map[string]json.RawMessage{"api_key": comm.Must(json.Marshal(apiKey))}})
}

// gmailConnection creates a READY connection of the published gmail v0.23.0
// google-oauth method whose access token is valid for an hour, so a read
// renews nothing.
func gmailConnection(ctx context.Context, t *testing.T, dao *connectiondao.ConnectionDAO, objects *objectstore.Store, teamSpaceID, account string) connectiondao.Connection {
	t.Helper()
	module := connectormanifest.Module{ConnectorID: "gmail", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/google/gmail", ModuleVersion: "v0.23.0"}
	text := func(value string) json.RawMessage { return comm.Must(json.Marshal(value)) }
	issued := time.Now().UTC()
	expires := issued.Add(time.Hour)
	credentials := map[string]json.RawMessage{
		"oauth_client_id": text("client-" + flowid.RandomID(8)), "oauth_client_secret": text("secret-" + flowid.RandomID(16)),
		"access_token": text("access-" + flowid.RandomID(16)), "refresh_token": text("refresh-" + flowid.RandomID(16)), "primary_email": text("user@example.com"),
	}
	connectormanifest.StampAuthMethod(credentials, "google-oauth")
	return readyConnection(ctx, t, dao, objects, teamSpaceID, account, "gmail", module, "google-oauth",
		storedCredential{Credentials: credentials, Grants: map[string]grantTimes{"refresh_token": {IssuedAt: issued, ExpiresAt: &expires}}})
}

// readyConnection creates a connection of module's method and commits
// credential as its READY credential.
func readyConnection(ctx context.Context, t *testing.T, dao *connectiondao.ConnectionDAO, objects *objectstore.Store, teamSpaceID, account, name string,
	module connectormanifest.Module, methodID string, credential storedCredential) connectiondao.Connection {
	t.Helper()
	now := time.Now().UTC()
	c, inserted, err := dao.Create(ctx, connectiondao.Connection{
		TeamSpaceID: teamSpaceID, ConnectionID: "c" + flowid.RandomID(11), CreationFingerprint: name, Name: name, ConnectorID: module.ConnectorID,
		ModulePath: module.ModulePath, VersionLine: connectormanifest.VersionLine(module.ModuleVersion), ManifestVersion: module.ModuleVersion, AuthMethodID: methodID,
		ClientSource: connectiondao.ClientUser, Settings: json.RawMessage(`{}`), CredentialStatus: connectiondao.CredentialAuthorizing,
		ConnectionStatus: connectiondao.ConnectionActive, CreatorAccountID: account, UpdatedByAccountID: account, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || !inserted {
		t.Fatalf("create %s: %v", name, err)
	}
	store := credentialObjects{objects: objects}
	written, err := store.write(ctx, teamSpaceID, c.ConnectionID, credential)
	if err != nil {
		t.Fatal(err)
	}
	owner := flowid.RandomID(24)
	if err = dao.ClaimAuthorizationLease(ctx, teamSpaceID, c.ConnectionID, c.CredentialRevision, owner, time.Minute); err != nil {
		t.Fatal(err)
	}
	committed, err := store.commit(ctx, dao, c, owner, written, credential, account)
	if err != nil {
		t.Fatal(err)
	}
	return committed
}

func scopeToken(ctx context.Context, t *testing.T, usages *Usages, projectID string, scope infra.ScopeKind) string {
	t.Helper()
	var token string
	if _, err := usages.ScopeToken(ctx, projectID, scope, func(created string) (json.RawMessage, error) {
		token = created
		return json.RawMessage(`{"key":"` + projectID + "/" + scope.String() + `"}`), nil
	}); err != nil {
		t.Fatal(err)
	}
	return token
}

func postRead(t *testing.T, base, token, connectionID string) (readResponse, int) {
	t.Helper()
	body := comm.Must(json.Marshal(readRequest{SharedConnectionID: connectionID}))
	request := comm.Must(http.NewRequestWithContext(t.Context(), http.MethodPost, base+CredentialReadPath, bytes.NewReader(body)))
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	var answer readResponse
	if response.StatusCode == http.StatusOK {
		if err = json.NewDecoder(response.Body).Decode(&answer); err != nil {
			t.Fatal(err)
		}
	}
	return answer, response.StatusCode
}

func post(t *testing.T, target, token string, body []byte) int {
	t.Helper()
	request := comm.Must(http.NewRequestWithContext(t.Context(), http.MethodPost, target, bytes.NewReader(body)))
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	return response.StatusCode
}

// registerAccount inserts the account that creates the test's connections
// and deletes it with the TeamSpace's rows when the test ends.
func registerAccount(ctx context.Context, t *testing.T, db *sql.DB, account, teamSpaceID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, _, err := accountdao.New(db, "admin@superverse.test").Register(ctx, accountdao.Account{
		AccountID: account, CanonicalEmail: "connections-" + account + "@example.com", Status: accountdao.StatusActive,
		PasswordRecord: json.RawMessage(`{}`), CredentialRevision: 1, AuthEpoch: 1, RegistrationFingerprint: account, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		for _, statement := range []string{`DELETE FROM team_space_connection_usages WHERE team_space_id = $1`, `DELETE FROM team_space_connections WHERE team_space_id = $1`} {
			if _, err := db.ExecContext(cleanup, statement, teamSpaceID); err != nil {
				t.Errorf("clean up: %v", err)
			}
		}
		if _, err := db.ExecContext(cleanup, `DELETE FROM accounts WHERE account_id = $1`, account); err != nil {
			t.Errorf("clean up the account: %v", err)
		}
	})
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
