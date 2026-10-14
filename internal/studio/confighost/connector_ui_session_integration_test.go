// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

//go:build live

package confighost

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/signedtoken"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
)

// TestConnectorUISessionAcrossReplicasIntegration serves one Preview scope
// from two configuration hosts over a real versioned MinIO bucket and the
// official Slack v0.22.1 release, as two replicas with the same signing key,
// and another Project's scope from a third. A UI session opened on one host
// serves its commands on the other. Its nonce is refused for another actor,
// in another Project's scope, after its expiry, after tampering, when signed
// for another purpose and when signed with another key.
func TestConnectorUISessionAcrossReplicasIntegration(t *testing.T) {
	endpoint, bucket := os.Getenv("SUPERVERSE_STUDIO_TEST_S3_ENDPOINT"), os.Getenv("SUPERVERSE_STUDIO_TEST_S3_BUCKET")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || bucket == "" {
		t.Fatal("requires explicit real loopback MinIO endpoint and existing versioned bucket in private test environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	graph, err := os.ReadFile(filepath.Join("testdata", "slack-thread-approval-fdg.json"))
	storageRequire(t, err, "read the generated Slack example FDG")
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	storageRequire(t, err, "load private AWS configuration")
	client := s3.NewFromConfig(cfg, func(options *s3.Options) { options.BaseEndpoint = aws.String(endpoint); options.UsePathStyle = true })
	id := uuid.NewString()
	prefix := "studio-ui-session-integration/" + id
	objects, err := connectorconfig.NewS3ObjectStore(ctx, &connectorconfig.S3StoreConfig{Client: client, Bucket: bucket, Prefix: prefix, AllowUnencrypted: true})
	storageRequire(t, err, "verify real bucket versioning")
	t.Cleanup(func() { cleanupConfigurationStorage(t, client, bucket, prefix+"/") })
	key := []byte(strings.Repeat("u", 32))
	host := func(projectID string) *ConfigurationHost {
		providers := connections.NewProviderHTTPClient(true)
		h, hostErr := NewConfigurationHost(objects, connectorconfig.Scope{ProjectID: projectID, Kind: "preview"}, connectorlibrary.OfficialReleaseDownloadURL,
			SharedConnections{Providers: providers, FormKey: key})
		storageRequire(t, hostErr, "construct a Preview configuration host")
		t.Cleanup(h.Close)
		return h
	}
	first, second, other := host(id), host(id), host(uuid.NewString())

	authMethodID := ""
	manifestBytes := comm.Must(json.Marshal(map[string]any{
		"schemaVersion":   "superverse.dev/dex-app/v1",
		"application":     map[string]any{"port": 8080, "healthPath": "/healthz"},
		"flowDefinitions": []any{map[string]any{"sourcePath": "flow/workflow.go"}},
		"connectors": []any{map[string]any{
			"connectionName": "slack-workspace", "connectorId": "slack", "authMethodId": &authMethodID,
			"modulePath": "github.com/superdurable/dex-connectors-library/connectors/slack", "version": "v0.22.1",
			"operations": []string{"listThreadMessages", "postThreadReply"},
		}},
	}))
	commit := strings.Repeat("e", 40)
	var source struct {
		DefinitionRevision string `json:"definitionRevision"`
	}
	for _, h := range []*ConfigurationHost{first, other} {
		prepared := configurationCall(t, ctx, h, ConfigurationAdmission{Internal: true}, http.MethodPut, "/api/v2/project-configuration/sources",
			map[string]any{"expectedRevision": 0, "sourceCommit": commit, "manifestDigest": objectstore.Digest(manifestBytes), "manifest": string(manifestBytes),
				"definitions": []json.RawMessage{graph}}, 200)
		storageRequire(t, json.Unmarshal(prepared, &source), "decode prepared definition fence")
	}
	admission := func(actorID string) ConfigurationAdmission {
		return ConfigurationAdmission{ActorID: actorID, SourceCommit: commit, DefinitionRevision: source.DefinitionRevision, AppManifestRevision: 1, ConfigurationRevision: 1}
	}
	var opened struct {
		SessionNonce string `json:"sessionNonce"`
	}
	storageRequire(t, json.Unmarshal(configurationCall(t, ctx, first, admission("ui-session-alice"), http.MethodPost, "/api/v2/connector-native-sessions",
		map[string]any{"connectorId": "slack", "connectionName": "slack-workspace"}, 200), &opened), "decode the opened UI session")

	// A command the release does not declare passes the nonce's checks and
	// stops before any credential read; a refused nonce stops at its check.
	command := func(h *ConfigurationHost, actorID, nonce string, status int, code string, what string) {
		t.Helper()
		body := configurationCall(t, ctx, h, admission(actorID), http.MethodPost, "/api/v2/connector-ui-sessions/"+nonce+"/commands/notDeclared",
			map[string]any{"parameters": map[string]string{}}, status)
		if !strings.Contains(string(body), code) {
			t.Fatalf("%s answered %s, want %s", what, body, code)
		}
	}
	accepted := func(h *ConfigurationHost, nonce, what string) {
		t.Helper()
		command(h, "ui-session-alice", nonce, http.StatusNotFound, "CONNECTOR_STUDIO_COMMAND_NOT_FOUND", what)
	}
	refused := func(h *ConfigurationHost, nonce, what string) {
		t.Helper()
		command(h, "ui-session-alice", nonce, http.StatusNotFound, "CONNECTOR_UI_SESSION_NOT_FOUND", what)
	}
	accepted(second, opened.SessionNonce, "the second host runs the first host's session")
	accepted(first, opened.SessionNonce, "the first host runs its own session")
	command(second, "ui-session-bob", opened.SessionNonce, http.StatusConflict, "CONNECTOR_SESSION_TARGET_CHANGED", "another actor replays the session")
	refused(other, opened.SessionNonce, "the session in another Project's scope")

	module := connectormanifest.Module{ConnectorID: "slack", ConnectionName: "slack-workspace",
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/slack", ModuleVersion: "v0.22.1"}
	claims := map[string]any{"actor": "ui-session-alice", "project": id, "scope": "preview", "source": commit, "module": module}
	sessions := signedtoken.New(key, signedtoken.ConnectorUISession)
	accepted(second, sessions.Seal(claims, time.Now().Add(time.Minute)), "a nonce the test seals with the key")
	refused(second, sessions.Seal(claims, time.Now().Add(-time.Second)), "an expired nonce")
	refused(second, signedtoken.New(key, signedtoken.ConnectionForm).Seal(claims, time.Now().Add(time.Minute)), "a nonce of another purpose")
	refused(second, signedtoken.New([]byte(strings.Repeat("o", 32)), signedtoken.ConnectorUISession).Seal(claims, time.Now().Add(time.Minute)), "a nonce signed with another key")
	// The connection is inside the MAC: naming another connection keeps the
	// signature and fails it.
	_, signature, _ := strings.Cut(opened.SessionNonce, ".")
	moved := module
	moved.ConnectionName = "another-workspace"
	payload := map[string]any{"exp": time.Now().Add(time.Minute).Unix(),
		"claims": map[string]any{"actor": "ui-session-alice", "project": id, "scope": "preview", "source": commit, "module": moved}}
	refused(second, base64.RawURLEncoding.EncodeToString(comm.Must(json.Marshal(payload)))+"."+signature, "a nonce whose connection was changed")
}
