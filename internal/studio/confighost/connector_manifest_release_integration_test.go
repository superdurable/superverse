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
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
)

// Resolve actual checksum-verified public release assets; a fixture resolver or
// unreachable GitHub is not successful release compatibility coverage.
func TestPublishedSingleMethodConnectorManifestAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	httpClient := connectormanifest.NewReleaseHTTPClient()
	defer httpClient.CloseIdleConnections()
	resolver := connectormanifest.NewReleaseResolver(httpClient, connectorlibrary.OfficialReleaseDownloadURL)
	for _, item := range []struct{ id, version, operation string }{
		{"github", "v0.10.0", "listReleases"},
		{"openai", "v0.8.0", "generateText"},
	} {
		t.Run(item.id, func(t *testing.T) {
			module := connectormanifest.Module{ConnectorID: item.id, ModulePath: "github.com/superdurable/dex-connectors-library/connectors/" + item.id, ModuleVersion: item.version}
			release, err := resolver.Resolve(ctx, module)
			if err != nil {
				t.Fatalf("actual published release: %v", err)
			}
			auth := release.Manifest.Spec.Auth
			method, found := auth.Method("")
			if !found || len(auth.Methods) != 0 || method.Type == "" {
				t.Fatal("release no longer has the expected single-method contract")
			}
			if _, found := auth.Method(method.Type); found {
				t.Fatal("auth type was accepted as a fabricated method ID")
			}
			credentials := map[string]json.RawMessage{}
			connectormanifest.StampAuthMethod(credentials, method.ID)
			if _, stamped := credentials[connectormanifest.AuthMethodCredential]; stamped || method.ID != "" {
				t.Fatal("released single-method identity was replaced with a synthetic public/provider method")
			}
			connector := map[string]any{"connectionName": "release-check", "connectorId": item.id, "modulePath": module.ModulePath, "version": item.version, "authMethodId": "", "operations": []string{item.operation}}
			manifest := map[string]any{"schemaVersion": "superverse.dev/dex-app/v1", "application": map[string]any{"port": 8080, "healthPath": "/health"}, "flowDefinitions": []any{map[string]any{"sourcePath": "internal/process/example_flow.go"}}, "connectors": []any{connector}}
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			source := release.SourceSHA
			snapshot, err := appmanifest.ExtractAppManifest(source, data)
			if err != nil || len(snapshot.GetConnectors()) != 1 || snapshot.Connectors[0].AuthMethodId != "" {
				t.Fatalf("source admission lost exact method identity: %v", err)
			}
			record, err := parseProjectManifest(ManifestRegistration{SourceCommit: source, ManifestDigest: objectstore.Digest(data), Manifest: string(data)})
			if err != nil || len(record.Manifest.Connectors) != 1 || record.Manifest.Connectors[0].AuthMethodID != "" {
				t.Fatalf("configuration admission lost exact method identity: %v", err)
			}
			for _, invalid := range []string{"absent", "null"} {
				if invalid == "absent" {
					delete(connector, "authMethodId")
				} else {
					connector["authMethodId"] = nil
				}
				data, err = json.Marshal(manifest)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = appmanifest.ExtractAppManifest(source, data); err == nil {
					t.Fatalf("source accepted %s authMethodId", invalid)
				}
				if _, err = parseProjectManifest(ManifestRegistration{SourceCommit: source, ManifestDigest: objectstore.Digest(data), Manifest: string(data)}); err == nil {
					t.Fatalf("configuration accepted %s authMethodId", invalid)
				}
			}
			t.Logf("actual release=%s source=%s manifest_sha256=%s method_id=empty auth_type=%s", release.Tag, source, release.ManifestSHA256, method.Type)
		})
	}
}

// Resolve an actual published OAuth release and read its refresh declarations as a host does: the release's
// literal endpoints and scopes resolve without settings, its refresh token mapping is the one default grant, its
// metadata lists exactly the credentials the host keeps from applications, and the manifest survives the round trip
// through the JSON that Studio receives. The release before it lists none (internal/tenancy/connections refuses it).
func TestPublishedOAuthRefreshDeclarations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	httpClient := connectormanifest.NewReleaseHTTPClient()
	defer httpClient.CloseIdleConnections()
	releases := connectormanifest.NewReleaseResolver(httpClient, connectorlibrary.OfficialReleaseDownloadURL)
	gmail := func(version string) connectormanifest.Module {
		return connectormanifest.Module{ConnectorID: "gmail", ModulePath: "github.com/superdurable/dex-connectors-library/connectors/google/gmail", ModuleVersion: version}
	}
	release, err := releases.Resolve(ctx, gmail("v0.23.0"))
	if err != nil {
		t.Fatalf("actual published release: %v", err)
	}
	method, found := release.Manifest.Spec.Auth.Method("google-oauth")
	if !found || method.OAuth2 == nil {
		t.Fatal("the release has no google-oauth method")
	}
	if err = connectormanifest.ValidateOAuthMethod(*method.OAuth2); err != nil {
		t.Fatalf("validate the OAuth declarations: %v", err)
	}
	resolved, err := connectormanifest.ResolveOAuth2(*method.OAuth2, connectormanifest.ConfigurationFields(release.Manifest, method), map[string]json.RawMessage{})
	if err != nil || resolved.TokenEndpoint != "https://oauth2.googleapis.com/token" || len(resolved.Scopes) == 0 {
		t.Fatalf("resolve the literal declarations: %+v: %v", resolved, err)
	}
	grants := connectormanifest.Grants(method)
	if len(grants) != 1 || grants[0].Kind != connectormanifest.GrantRefreshToken || grants[0].RefreshTokenCredential != "refresh_token" ||
		len(grants[0].TerminalErrorCodes) == 0 {
		t.Fatalf("the refresh grants are %+v", grants)
	}
	for methodID, want := range map[string][]string{"google-oauth": {"oauth_client_secret", "refresh_token"}, "workspace-domain-delegation": {"service_account_key"}} {
		if kept, declared := release.HostKeptCredentials(methodID); !declared || !slices.Equal(kept, want) {
			t.Fatalf("method %s keeps %v (declared %t), not %v", methodID, kept, declared, want)
		}
	}
	encoded, err := json.Marshal(release.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	var decoded connectormanifest.ReleaseManifest
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode the re-encoded manifest: %v", err)
	}
	if again, err := json.Marshal(decoded); err != nil || string(again) != string(encoded) {
		t.Fatalf("the manifest changed in its round trip: %v", err)
	}
	t.Logf("actual release=%s source=%s manifest_sha256=%s", release.Tag, release.SourceSHA, release.ManifestSHA256)

	previous, err := releases.Resolve(ctx, gmail("v0.22.0"))
	if err != nil {
		t.Fatalf("actual published release: %v", err)
	}
	if kept, declared := previous.HostKeptCredentials(method.ID); declared || kept != nil {
		t.Fatalf("a release without the list declares %v", kept)
	}
}

// testLibraryReleaseCandidates are the releases of the library's test copy, where the manual release candidate check
// published connectors/hackernews/v0.21.0-rc.1 from commit 5327b021 with the Release Connector Candidate workflow.
const testLibraryReleaseCandidates = "https://github.com/superdurable/dex-connectors-library-test/releases/download"

// Resolve an actual published release candidate from the server's candidate releases, and admit an AppManifest that
// declares it; the connectors library itself never serves that candidate.
func TestPublishedReleaseCandidateManifestAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	httpClient := connectormanifest.NewReleaseHTTPClient()
	defer httpClient.CloseIdleConnections()
	module := connectormanifest.Module{ConnectorID: "hacker-news-daily", ConnectionName: "news",
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/hackernews", ModuleVersion: "v0.21.0-rc.1"}
	release, err := connectormanifest.NewReleaseResolver(httpClient, testLibraryReleaseCandidates).Resolve(ctx, module)
	if err != nil {
		t.Fatalf("actual published release candidate: %v", err)
	}
	if release.Version != module.ModuleVersion || release.Tag != "connectors/hackernews/v0.21.0-rc.1" ||
		release.SourceSHA != "5327b0210e5973593eede012ae229216ee4795bc" {
		t.Fatalf("release candidate identity: version=%s tag=%s source=%s", release.Version, release.Tag, release.SourceSHA)
	}
	connector := map[string]any{"connectionName": module.ConnectionName, "connectorId": module.ConnectorID, "modulePath": module.ModulePath,
		"version": module.ModuleVersion, "authMethodId": "", "operations": []string{"listStoryIDs"}}
	manifest := map[string]any{"schemaVersion": "superverse.dev/dex-app/v1", "application": map[string]any{"port": 8080, "healthPath": "/health"},
		"flowDefinitions": []any{map[string]any{"sourcePath": "internal/process/example_flow.go"}}, "connectors": []any{connector}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	record, err := parseProjectManifest(ManifestRegistration{SourceCommit: release.SourceSHA, ManifestDigest: objectstore.Digest(data), Manifest: string(data)})
	if err != nil || len(record.Manifest.Connectors) != 1 || record.Manifest.Connectors[0].Version != module.ModuleVersion {
		t.Fatalf("configuration admission of the release candidate: %v", err)
	}
	pseudoVersion := module
	pseudoVersion.ModuleVersion = "v0.21.1-0.20261010180000-5327b0210e59"
	if _, err = connectormanifest.NewReleaseResolver(httpClient, testLibraryReleaseCandidates).Resolve(ctx, pseudoVersion); err == nil {
		t.Fatal("a pseudo-version resolved as a published release")
	}
	if _, err = connectormanifest.NewReleaseResolver(httpClient, connectorlibrary.OfficialReleaseDownloadURL).Resolve(ctx, module); err == nil {
		t.Fatal("the connectors library served a release candidate that only its test copy published")
	}
	t.Logf("actual release candidate=%s source=%s manifest_sha256=%s", release.Tag, release.SourceSHA, release.ManifestSHA256)
}
