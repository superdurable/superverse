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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/google/uuid"
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/app/appmanifest"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/studio/confighost/configitems"
	"github.com/superdurable/superversev2/internal/studio/flowapi"
	"github.com/superdurable/superversev2/internal/studio/flowapi/fdg"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
)

// This runs the production management host and official SDK against an existing,
// versioned MinIO bucket. Every state read, conditional write and exact-version
// read crosses the real S3 API; no HTTP or object-store substitute is installed.
func TestConfigurationStorageMinIOIntegration(t *testing.T) {
	endpoint, bucket := os.Getenv("SUPERVERSE_STUDIO_TEST_S3_ENDPOINT"), os.Getenv("SUPERVERSE_STUDIO_TEST_S3_BUCKET")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.User != nil || parsed.Path != "" || bucket == "" {
		t.Fatal("requires explicit real loopback MinIO endpoint and existing versioned bucket in private test environment")
	}
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
		t.Fatal("requires S3 credentials from the private test environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	graphPath := os.Getenv("SUPERVERSE_STUDIO_TEST_FDG")
	if graphPath == "" {
		graphPath = filepath.Join("..", "..", "..", "docs", "flows", "sv2-project-flow.json")
	}
	graph := configurationBuilderGraph(t, ctx, graphPath)
	generatedDefinitions, err := fdg.ParseBundle([]json.RawMessage{graph})
	storageRequire(t, err, "parse generated production FDG")
	var graphHeader struct {
		Source struct {
			Path string `json:"path"`
		} `json:"source"`
	}
	storageRequire(t, json.Unmarshal(graph, &graphHeader), "read FDG source identity")
	if graphHeader.Source.Path == "" {
		t.Fatal("generated FDG source identity is missing")
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	storageRequire(t, err, "load private AWS configuration")
	client := s3.NewFromConfig(cfg, func(options *s3.Options) { options.BaseEndpoint = aws.String(endpoint); options.UsePathStyle = true })
	id := uuid.NewString()
	prefix := "studio-configuration-integration/" + id
	t.Logf("real MinIO scope: endpoint=%s project=%s prefix=%s", endpoint, id, prefix)
	objects, err := connectorconfig.NewS3ObjectStore(ctx, &connectorconfig.S3StoreConfig{Client: client, Bucket: bucket, Prefix: prefix, AllowUnencrypted: true})
	storageRequire(t, err, "verify real bucket versioning")
	t.Cleanup(func() { cleanupConfigurationStorage(t, client, bucket, prefix+"/") })
	t.Run("generated_FDG_exact_S3_bytes", func(t *testing.T) {
		artifacts, storeErr := objectstore.NewStore(client, bucket, "", prefix, true)
		storageRequire(t, storeErr, "construct production artifact store")
		// The fixture's text holds "<", ">" and "&", which an HTML-escaping encoder would rewrite.
		htmlGraph := configurationBuilderGraph(t, ctx, filepath.Join("testdata", "html-bytes-fdg.json"))
		if !bytes.Contains(htmlGraph, []byte("<source> text & keep")) {
			t.Fatal("the encoded fixture FDG does not carry its HTML-sensitive bytes")
		}
		for index, data := range [][]byte{graph, configurationBuilderGraph(t, ctx, filepath.Join("..", "..", "..", "docs", "flows", "sv2-repository-flow.json")), htmlGraph} {
			ref, putErr := artifacts.PutJSON(ctx, fmt.Sprintf("generated-definitions/%d.json", index), map[string]json.RawMessage{"graph": data})
			storageRequire(t, putErr, "store generated FDG envelope in real S3")
			var stored map[string]json.RawMessage
			storageRequire(t, artifacts.GetJSON(ctx, ref, &stored), "read exact generated FDG envelope version")
			if !bytes.Equal(data, stored["graph"]) {
				t.Errorf("generated FDG bytes changed in S3 envelope: before=%s after=%s", objectstore.Digest(data), objectstore.Digest(stored["graph"]))
			}
		}
	})
	liveScope := connectorconfig.Scope{ProjectID: id, Kind: "live"}
	// The manifest declares no connection, so no TeamSpace connection is read.
	shared := SharedConnections{Providers: connections.NewProviderHTTPClient(true)}
	live, err := NewConfigurationHost(objects, liveScope, connectorlibrary.OfficialReleaseDownloadURL, shared)
	storageRequire(t, err, "construct production Live configuration host")
	defer live.Close()
	manifestBytes, err := json.Marshal(map[string]any{
		"schemaVersion":   "superverse.dev/dex-app/v1",
		"application":     map[string]any{"port": 8080, "healthPath": "/healthz", "environment": []any{map[string]any{"name": "APP_MESSAGE", "required": true}, map[string]any{"name": "SIGNING_SECRET", "required": true, "secret": true, "minLength": 32}}},
		"flowDefinitions": []any{map[string]any{"sourcePath": graphHeader.Source.Path}}, "connectors": []any{},
	})
	storageRequire(t, err, "encode manifest")
	manifest, digest := string(manifestBytes), objectstore.Digest(manifestBytes)
	first, second := strings.Repeat("a", 40), strings.Repeat("b", 40)
	internal := ConfigurationAdmission{Internal: true}
	preparedDefinitionRevision := ""
	admission := func(commit string, revision uint64) ConfigurationAdmission {
		return ConfigurationAdmission{ActorID: "storage-integration-operator", SourceCommit: commit, DefinitionRevision: preparedDefinitionRevision, AppManifestRevision: 1, ConfigurationRevision: revision}
	}
	prepare := func(host *ConfigurationHost, commit string) []byte {
		result := configurationCall(t, ctx, host, internal, http.MethodPut, "/api/v2/project-configuration/sources", map[string]any{"expectedRevision": 0, "sourceCommit": commit, "manifestDigest": digest, "manifest": manifest, "definitions": []json.RawMessage{graph}}, 200)
		var prepared struct {
			DefinitionRevision string `json:"definitionRevision"`
		}
		storageRequire(t, json.Unmarshal(result, &prepared), "decode prepared definition fence")
		if prepared.DefinitionRevision != generatedDefinitions.Revision || preparedDefinitionRevision != "" && preparedDefinitionRevision != prepared.DefinitionRevision {
			t.Fatal("identical prepared source changed its definition revision")
		}
		preparedDefinitionRevision = prepared.DefinitionRevision
		return result
	}
	prepare(live, first)
	freezeAdmission := internal
	freezeAdmission.SourceCommit = first
	// The gap report lists both missing variables at once, by name only, and
	// reads nothing it would have to write.
	assertRequirementGaps(t, ctx, live, freezeAdmission, digest, nil, "APP_MESSAGE", "SIGNING_SECRET")
	secret := uuid.NewString() + uuid.NewString()
	firstRead := configurationCall(t, ctx, live, admission(first, 1), http.MethodPut, "/api/v2/application-environment", map[string]any{"values": map[string]string{"APP_MESSAGE": "first"}, "secrets": map[string]string{"SIGNING_SECRET": secret}}, 200)
	assertPrivateEnvironment(t, firstRead, secret, "first", 2)
	assertRequirementGaps(t, ctx, live, freezeAdmission, digest, nil)
	buildID := "build-" + uuid.NewString()
	freezeInput := map[string]any{"requestId": buildID, "appManifestRevision": 1, "manifestDigest": digest, "configurationRevision": 2}
	frozenBytes := configurationCall(t, ctx, live, freezeAdmission, http.MethodPost, "/api/v2/project-configuration/validate", freezeInput, 200)
	var frozen struct {
		Snapshot connectorconfig.SnapshotRef `json:"snapshot"`
		Revision uint64                      `json:"snapshotConfigurationRevision"`
	}
	storageRequire(t, json.Unmarshal(frozenBytes, &frozen), "decode exact freeze reference")
	if frozen.Snapshot.Key != "projects/"+id+"/live/configuration/head" || frozen.Snapshot.Version == "" || frozen.Snapshot.Digest == "" || frozen.Revision != 1 {
		t.Fatal("freeze did not return an exact canonical version and revision")
	}
	store, err := connectorconfig.NewConfigurationStore(&connectorconfig.ConfigurationStoreConfig{Objects: objects, Scope: liveScope})
	storageRequire(t, err, "construct official canonical store")
	assertSnapshotEnvironment(t, ctx, store, frozen.Snapshot, "first", secret)
	configurationCall(t, ctx, live, admission(first, 2), http.MethodPut, "/api/v2/application-environment", map[string]any{"values": map[string]string{"APP_MESSAGE": "edited"}}, 200)
	recovered := configurationCall(t, ctx, live, freezeAdmission, http.MethodPost, "/api/v2/project-configuration/validate", freezeInput, 200)
	if !bytes.Equal(frozenBytes, recovered) {
		t.Fatal("same-build recovery changed the canonical snapshot after a later draft edit")
	}
	// Another commit's configuration starts empty: a version's settings come from Live's next-publish layer.
	prepare(live, second)
	configurationCall(t, ctx, live, admission(second, 1), http.MethodPut, "/api/v2/application-environment", map[string]any{"values": map[string]string{"APP_MESSAGE": "second"}, "secrets": map[string]string{"SIGNING_SECRET": secret}}, 200)
	prepare(live, second)
	assertPrivateEnvironment(t, configurationCall(t, ctx, live, admission(second, 2), http.MethodGet, "/api/v2/application-environment", nil, 200), secret, "second", 2)
	assertPrivateEnvironment(t, configurationCall(t, ctx, live, admission(first, 3), http.MethodGet, "/api/v2/application-environment", nil, 200), secret, "edited", 3)
	secondFreeze := internal
	secondFreeze.SourceCommit = second
	configurationCall(t, ctx, live, secondFreeze, http.MethodPost, "/api/v2/project-configuration/validate", map[string]any{"requestId": "build-" + uuid.NewString(), "appManifestRevision": 1, "manifestDigest": digest, "configurationRevision": 2}, 200)
	assertSnapshotEnvironment(t, ctx, store, frozen.Snapshot, "first", secret)
	conflicting := map[string]any{"requestId": buildID, "appManifestRevision": 1, "manifestDigest": digest, "configurationRevision": 3}
	configurationCall(t, ctx, live, freezeAdmission, http.MethodPost, "/api/v2/project-configuration/validate", conflicting, 409)
	configurationCall(t, ctx, live, admission(first, 2), http.MethodPut, "/api/v2/application-environment", map[string]any{"values": map[string]string{"APP_MESSAGE": "stale"}}, 409)

	// Four independent requests race the same real S3 ETag via the production handler.
	type concurrentResult struct {
		result flowapi.Document
		err    error
	}
	results := make(chan concurrentResult, 4)
	start := make(chan struct{})
	for index := 0; index < cap(results); index++ {
		go func(index int) {
			<-start
			body := comm.Must(json.Marshal(map[string]any{"values": map[string]string{"APP_MESSAGE": fmt.Sprintf("race-%d", index)}}))
			result, invokeErr := live.Invoke(ctx, admission(first, 3), http.MethodPut, "/api/v2/application-environment", body)
			results <- concurrentResult{result, invokeErr}
		}(index)
	}
	close(start)
	winners := 0
	for index := 0; index < cap(results); index++ {
		result := <-results
		storageRequire(t, result.err, "concurrent production CAS request")
		if result.result.StatusCode == http.StatusOK {
			winners++
		} else if result.result.StatusCode != http.StatusConflict {
			t.Fatalf("CAS returned unexpected status %d; private body omitted", result.result.StatusCode)
		}
	}
	if winners != 1 {
		t.Fatalf("real conditional-write race had %d winners, expected one", winners)
	}
	previewScope := connectorconfig.Scope{ProjectID: id, Kind: "preview"}
	preview, err := NewConfigurationHost(objects, previewScope, connectorlibrary.OfficialReleaseDownloadURL, shared)
	storageRequire(t, err, "construct Preview configuration host")
	defer preview.Close()
	prepare(preview, first)
	previewAdmission := admission(first, 1)
	previewRequest := internal
	previewRequest.SourceCommit = first
	previewStore, err := connectorconfig.NewConfigurationStore(&connectorconfig.ConfigurationStoreConfig{Objects: objects, Scope: previewScope})
	storageRequire(t, err, "construct Preview canonical store")
	// The version before the environment was set, frozen as an earlier Apply
	// would have, lacks both variables.
	emptySnapshot, err := previewStore.FreezeConfiguration(ctx, 1)
	storageRequire(t, err, "freeze the Preview's first version")
	configurationCall(t, ctx, preview, previewAdmission, http.MethodPut, "/api/v2/application-environment", map[string]any{"values": map[string]string{"APP_MESSAGE": "preview"}, "secrets": map[string]string{"SIGNING_SECRET": secret}}, 200)
	previewRead := configurationCall(t, ctx, preview, previewAdmission, http.MethodGet, "/api/v2/application-environment", nil, 200)
	assertPrivateEnvironment(t, previewRead, secret, "preview", 2)
	staleDefinition := previewAdmission
	staleDefinition.ConfigurationRevision = 2
	staleDefinition.DefinitionRevision = "sha256:" + strings.Repeat("0", 64)
	configurationCall(t, ctx, preview, staleDefinition, http.MethodPut, "/api/v2/application-environment", map[string]any{"values": map[string]string{"APP_MESSAGE": "wrong-definition"}}, 409)
	assertPrivateEnvironment(t, configurationCall(t, ctx, preview, previewAdmission, http.MethodGet, "/api/v2/application-environment", nil, 200), secret, "preview", 2)
	assertPrivateEnvironment(t, configurationCall(t, ctx, live, admission(second, 2), http.MethodGet, "/api/v2/application-environment", nil, 200), secret, "second", 2)
	previewSnapshot, err := previewStore.FreezeConfiguration(ctx, 2)
	storageRequire(t, err, "freeze exact Preview version")
	if _, err = store.ReadSnapshot(ctx, previewSnapshot); err == nil {
		t.Fatal("Live store accepted a Preview snapshot")
	}
	assertSnapshotEnvironment(t, ctx, previewStore, previewSnapshot, "preview", secret)
	t.Run("preview_applied_version", func(t *testing.T) {
		// Apply freezes exactly the revision its sender saw; another one is
		// refused with its own code.
		apply := map[string]any{"appManifestRevision": 1, "manifestDigest": digest, "configurationRevision": 1}
		configurationCall(t, ctx, preview, previewRequest, http.MethodPost, "/api/v2/project-configuration/validate", apply, http.StatusConflict)
		refused, err := preview.Invoke(ctx, previewRequest, http.MethodPost, "/api/v2/project-configuration/validate", comm.Must(json.Marshal(apply)))
		storageRequire(t, err, "invoke the Preview freeze")
		var coded struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(refused.JSON, &coded) != nil || coded.Code != PreviewRevisionChangedCode {
			t.Fatalf("an Apply of an older revision answered %s", coded.Code)
		}
		items := func(applied *connectorconfig.SnapshotRef) PreviewItemsReport {
			var report PreviewItemsReport
			storageRequire(t, json.Unmarshal(configurationCall(t, ctx, preview, previewRequest, http.MethodPost, "/api/v2/project-configuration/preview-items",
				PreviewItemsRequest{ManifestDigest: digest, Applied: applied}, 200), &report), "decode the Preview items")
			return report
		}
		states := func(report PreviewItemsReport) map[string]configitems.State {
			found := map[string]configitems.State{}
			for _, item := range report.Items {
				if item.Kind != configitems.KindEnvironment {
					t.Fatalf("the Preview holds an unexpected item %+v", item.Key)
				}
				found[item.EnvironmentName] = item.State
			}
			return found
		}
		if before := items(nil); before.UnappliedChangeCount != 2 || !maps.Equal(states(before), map[string]configitems.State{"APP_MESSAGE": configitems.Added, "SIGNING_SECRET": configitems.Added}) {
			t.Fatalf("before an Apply every item is ADDED: %+v", before)
		}
		if applied := items(&previewSnapshot); applied.UnappliedChangeCount != 0 || applied.ConfigurationRevision != 2 {
			t.Fatalf("the applied head differs from itself: %+v", applied)
		}
		// A changed value counts; the secret, compared by its reference, does not.
		configurationCall(t, ctx, preview, admission(first, 2), http.MethodPut, "/api/v2/application-environment", map[string]any{"values": map[string]string{"APP_MESSAGE": "edited"}}, 200)
		edited := items(&previewSnapshot)
		if edited.UnappliedChangeCount != 1 || !maps.Equal(states(edited), map[string]configitems.State{"APP_MESSAGE": configitems.Changed, "SIGNING_SECRET": configitems.Unchanged}) {
			t.Fatalf("the edit must be the one change: %+v", edited)
		}
		// The gaps of an exact version are that version's, not the head's.
		assertRequirementGaps(t, ctx, preview, previewRequest, digest, nil)
		assertRequirementGaps(t, ctx, preview, previewRequest, digest, &emptySnapshot, "APP_MESSAGE", "SIGNING_SECRET")
	})
	t.Logf("real MinIO + SDK projectconfig + ConfigurationHost: project=%s prefix=%s FDG=%s; real generated FDG byte preservation, source prepare, commit draft isolation, empty initialization of a new commit, canonical freeze/recovery, exact versions, one concurrent CAS winner and definition fence verified", id, prefix, objectstore.Digest(graph))
}

// Read an actual checked-in generated FDG, then call the production builder's
// actual encoding function. This preserves its ordering, Unicode and HTML byte
// policy without synthesizing or modifying any Flow definition or dependency.
func configurationBuilderGraph(t *testing.T, ctx context.Context, path string) []byte {
	t.Helper()
	program := `import json, pathlib, runpy, sys
encode = runpy.run_path(sys.argv[1])["encoded"]
sys.stdout.buffer.write(encode(json.loads(pathlib.Path(sys.argv[2]).read_bytes())))`
	result, err := exec.CommandContext(ctx, "python3", "-c", program, filepath.Join("..", "..", "delivery", "releases", "build", "hosted", "runner.py"), path).Output()
	storageRequire(t, err, "encode real generated FDG using production builder")
	return result
}

func configurationCall(t *testing.T, ctx context.Context, host *ConfigurationHost, admission ConfigurationAdmission, method, path string, input any, want int) []byte {
	t.Helper()
	var body []byte
	var err error
	if input != nil {
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		encoder.SetEscapeHTML(false)
		storageRequire(t, encoder.Encode(input), "encode management input without modifying FDG bytes")
		body = bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
	}
	result, err := host.Invoke(ctx, admission, method, path, body)
	storageRequire(t, err, "invoke production management host")
	if result.StatusCode != want {
		var failure struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(result.JSON, &failure)
		t.Fatalf("%s %s returned %d, expected %d; code=%s; private body omitted", method, path, result.StatusCode, want, failure.Code)
	}
	return result.JSON
}
func storageRequire(t *testing.T, err error, operation string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s failed (%T); private error details omitted", operation, err)
	}
}

// assertRequirementGaps requires the gap report of the prepared source that
// admission names, for the head or the exact version snapshot names, to list
// exactly one environment gap per name in want, and readiness when want is
// empty.
func assertRequirementGaps(t *testing.T, ctx context.Context, host *ConfigurationHost, admission ConfigurationAdmission, digest string, snapshot *connectorconfig.SnapshotRef, want ...string) {
	t.Helper()
	data := configurationCall(t, ctx, host, admission, http.MethodPost, "/api/v2/project-configuration/requirements", RequirementsRequest{ManifestDigest: digest, Snapshot: snapshot}, 200)
	var report RequirementsReport
	storageRequire(t, json.Unmarshal(data, &report), "decode the requirement gap report")
	names := make([]string, 0, len(report.Gaps))
	for _, gap := range report.Gaps {
		if gap.Kind != appmanifest.GapEnvironmentRequired || gap.ConnectionName != "" {
			t.Fatalf("the report holds a gap other than a missing environment variable: %s", gap.Name())
		}
		names = append(names, gap.EnvironmentName)
	}
	if report.Ready != (len(want) == 0) || !slices.Equal(names, want) {
		t.Fatalf("the gap report is ready=%t with %v, want the gaps %v", report.Ready, names, want)
	}
}
func assertPrivateEnvironment(t *testing.T, data []byte, secret, want string, revision uint64) {
	t.Helper()
	if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte(`"csrfToken"`)) {
		t.Fatal("public environment response exposed private content or internal CSRF capability")
	}
	var view struct {
		Revision uint64 `json:"configurationRevision"`
		Fields   []struct {
			Name       string  `json:"name"`
			Value      *string `json:"value"`
			Configured bool    `json:"configured"`
		} `json:"fields"`
	}
	storageRequire(t, json.Unmarshal(data, &view), "decode public environment")
	if view.Revision != revision {
		t.Fatalf("configuration revision %d, expected %d", view.Revision, revision)
	}
	foundValue, foundSecret := false, false
	for _, field := range view.Fields {
		if field.Name == "APP_MESSAGE" {
			foundValue = field.Value != nil && *field.Value == want
		}
		if field.Name == "SIGNING_SECRET" {
			foundSecret = field.Configured && field.Value == nil
		}
	}
	if !foundValue || !foundSecret {
		t.Fatal("environment value or write-only secret presence mismatch")
	}
}
func assertSnapshotEnvironment(t *testing.T, ctx context.Context, store *connectorconfig.ConfigurationStore, ref connectorconfig.SnapshotRef, want, secret string) {
	t.Helper()
	snapshot, err := store.ReadSnapshot(ctx, ref)
	storageRequire(t, err, "read exact immutable snapshot")
	if value := snapshot.Environment["APP_MESSAGE"].Value; value == nil || *value != want {
		t.Fatal("exact snapshot environment changed")
	}
	resolved, err := store.ResolveApplicationEnvironment(ctx, snapshot)
	storageRequire(t, err, "resolve exact immutable environment")
	actual, found := resolved.Lookup("SIGNING_SECRET")
	if !found || actual != secret {
		t.Fatal("exact private secret version changed")
	}
}
func cleanupConfigurationStorage(t *testing.T, client *s3.Client, bucket, prefix string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pages := s3.NewListObjectVersionsPaginator(client, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	deleted := 0
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			t.Errorf("owned-prefix cleanup listing failed (%T)", err)
			return
		}
		var keys []types.ObjectIdentifier
		for _, entry := range page.Versions {
			if !strings.HasPrefix(aws.ToString(entry.Key), prefix) {
				t.Error("cleanup scope mismatch")
				return
			}
			keys = append(keys, types.ObjectIdentifier{Key: entry.Key, VersionId: entry.VersionId})
		}
		for _, entry := range page.DeleteMarkers {
			if !strings.HasPrefix(aws.ToString(entry.Key), prefix) {
				t.Error("cleanup marker scope mismatch")
				return
			}
			keys = append(keys, types.ObjectIdentifier{Key: entry.Key, VersionId: entry.VersionId})
		}
		if len(keys) > 0 {
			result, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: aws.String(bucket), Delete: &types.Delete{Objects: keys}})
			if err != nil || len(result.Errors) > 0 {
				t.Error("owned-prefix version cleanup failed")
				return
			}
			deleted += len(keys)
		}
	}
	remaining, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	if err != nil || len(remaining.Versions) > 0 || len(remaining.DeleteMarkers) > 0 {
		t.Error("owned-prefix cleanup absence unconfirmed")
		return
	}
	t.Logf("removed %d test-owned object versions; exact prefix absence confirmed", deleted)
}
