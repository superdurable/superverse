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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	connectorconfig "github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

// slackTriggerPath is the trigger binding route of the Slack example's reply binding.
const slackTriggerPath = "/api/v2/connector-trigger-bindings/slack/slack-workspace/threadReplyCreated/slack-thread-approval-reply"

// TestTriggerBindingsUseTheFlowsConnectorUnits runs the production
// configuration host against a real versioned MinIO bucket and the official
// Slack v0.22.1 release, with the FDG that the pinned dexcli generated from
// that release's thread-approval example. The release publishes no trigger
// fields; each binding's configuration is the units its Flow composes.
func TestTriggerBindingsUseTheFlowsConnectorUnits(t *testing.T) {
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
	prefix := "studio-trigger-binding-integration/" + id
	objects, err := connectorconfig.NewS3ObjectStore(ctx, &connectorconfig.S3StoreConfig{Client: client, Bucket: bucket, Prefix: prefix, AllowUnencrypted: true})
	storageRequire(t, err, "verify real bucket versioning")
	t.Cleanup(func() { cleanupConfigurationStorage(t, client, bucket, prefix+"/") })
	providers := connections.NewProviderHTTPClient(true)
	host, err := NewConfigurationHost(objects, connectorconfig.Scope{ProjectID: id, Kind: "preview"}, connectorlibrary.OfficialReleaseDownloadURL, SharedConnections{Providers: providers,
		// The selection names no TeamSpace connection of the lane's PostgreSQL, so the gaps keep the connection's own.
		Connections: connections.NewTeamSpaceConnections(connectiondao.New(postgrestest.OpenForTest(t)), nil, nil, nil, connections.PlatformClients{}, providers, nil, nil, true)})
	storageRequire(t, err, "construct production Preview configuration host")
	defer host.Close()

	authMethodID := ""
	manifestBytes, err := json.Marshal(map[string]any{
		"schemaVersion":   "superverse.dev/dex-app/v1",
		"application":     map[string]any{"port": 8080, "healthPath": "/healthz"},
		"flowDefinitions": []any{map[string]any{"sourcePath": "flow/workflow.go"}},
		"connectors": []any{map[string]any{
			"connectionName": "slack-workspace", "connectorId": "slack", "authMethodId": &authMethodID,
			"modulePath": "github.com/superdurable/dex-connectors-library/connectors/slack", "version": "v0.22.1",
			"operations": []string{"listThreadMessages", "postThreadReply"},
			"triggerBindings": []any{
				map[string]string{"triggerName": "channelThreadCreated", "bindingName": "slack-thread-approval-start"},
				map[string]string{"triggerName": "threadReplyCreated", "bindingName": "slack-thread-approval-reply"},
			},
		}},
	})
	storageRequire(t, err, "encode manifest")
	manifest, digest := string(manifestBytes), objectstore.Digest(manifestBytes)
	commit := strings.Repeat("d", 40)
	prepared := configurationCall(t, ctx, host, ConfigurationAdmission{Internal: true}, http.MethodPut, "/api/v2/project-configuration/sources",
		map[string]any{"expectedRevision": 0, "sourceCommit": commit, "manifestDigest": digest, "manifest": manifest, "definitions": []json.RawMessage{graph}}, 200)
	var source struct {
		DefinitionRevision string `json:"definitionRevision"`
	}
	storageRequire(t, json.Unmarshal(prepared, &source), "decode prepared definition fence")
	admission := func(revision uint64) ConfigurationAdmission {
		return ConfigurationAdmission{
			ActorID: "trigger-integration-operator", SourceCommit: commit,
			DefinitionRevision: source.DefinitionRevision, AppManifestRevision: 1, ConfigurationRevision: revision,
		}
	}
	gapsAdmission := ConfigurationAdmission{Internal: true, SourceCommit: commit}

	// The view offers each binding the units its Flow composes.
	var list struct {
		Connections []struct {
			TriggerUses []struct {
				TriggerName     string `json:"triggerName"`
				ConfigurationUI *struct {
					Units []struct {
						ID string `json:"id"`
					} `json:"units"`
				} `json:"configurationUI"`
			} `json:"triggerUses"`
		} `json:"connections"`
	}
	storageRequire(t, json.Unmarshal(configurationCall(t, ctx, host, admission(1), http.MethodGet, "/api/v2/connector-connections", nil, 200), &list), "decode the connections view")
	units := map[string][]string{}
	for _, use := range list.Connections[0].TriggerUses {
		if use.ConfigurationUI == nil {
			t.Fatalf("trigger %s has no configuration units", use.TriggerName)
		}
		for _, unit := range use.ConfigurationUI.Units {
			units[use.TriggerName] = append(units[use.TriggerName], unit.ID)
		}
	}
	if !slices.Equal(units["threadReplyCreated"], []string{"channel", "message", "approvers"}) || len(units["channelThreadCreated"]) == 0 {
		t.Fatalf("the view offers the trigger units %v", units)
	}
	assertTriggerGaps(t, ctx, host, gapsAdmission, digest, "slack-thread-approval-start", "slack-thread-approval-reply")
	// A Step's operation use with a required unit is a gap until saved with a
	// value for it; listThreadMessages has no unit and is never one.
	completion := completionUse(t, ctx, host, admission(1))
	completionGap := "slack-workspace step " + completion.FlowName + "/PostSlackCompletion operation postThreadReply (operation configuration required: Completion reply)"
	assertOperationUseGaps(t, ctx, host, gapsAdmission, digest, completionGap)

	// A binding belongs to a selected connection name. The selection of a
	// TeamSpace connection writes this entry; the test writes it directly.
	draftRequest := host.project.admit(admission(1))
	draft, err := host.project.loadConnectionDraft(ctx, draftRequest)
	storageRequire(t, err, "read the commit draft through real S3")
	draft.replaceConnection(connectorconfig.ConnectionConfiguration{ConnectorID: "slack", ConnectionName: "slack-workspace",
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/slack", Provider: "slack", SharedConnectionID: "ctriggertest",
		Configuration: json.RawMessage(`{}`)})
	reference, err := draftRequest.drafts.CreateCredentialBrokerToken(ctx, "trigger-test-"+id)
	storageRequire(t, err, "store the scope token")
	draft.document.CredentialBroker = &connectorconfig.CredentialBrokerConfiguration{Endpoint: "http://127.0.0.1:1/internal/connection-credentials/read", TokenSecretRef: reference}
	storageRequire(t, draft.save(ctx, draftRequest), "save the connection name's selection")
	// A path that no unit binds is refused; the units' paths are accepted.
	refused := configurationCall(t, ctx, host, admission(2), http.MethodPut, slackTriggerPath, map[string]any{"configuration": map[string]any{"channel": "C1"}}, 400)
	if !strings.Contains(string(refused), "CONNECTOR_TRIGGER_CONFIGURATION_INVALID") {
		t.Fatal("an undeclared trigger configuration path was not refused as invalid")
	}
	// Each unit saves on its own, so a saved binding may still lack a
	// required unit: it stays a gap naming that unit, and unconfigured.
	partial := []map[string]any{
		{"threadReplyMatcher": map[string]any{"posterUserIds": []string{"U2"}}},
		{"channelId": " ", "threadReplyMatcher": map[string]any{"messageContains": "approve", "posterUserIds": []string{"U2"}}},
		{"channelId": "C1", "threadReplyMatcher": map[string]any{"posterUserIds": []string{}}},
	}
	missing := []string{"Approval channel", "Approval channel", "Members allowed to approve"}
	revision := uint64(2)
	for index, reply := range partial {
		configurationCall(t, ctx, host, admission(revision), http.MethodPut, slackTriggerPath, map[string]any{"configuration": reply}, 200)
		revision++
		assertTriggerGaps(t, ctx, host, gapsAdmission, digest, "slack-thread-approval-start", "slack-thread-approval-reply: "+missing[index])
		assertReplyConfigured(t, ctx, host, admission(revision), false)
	}
	reply := map[string]any{"channelId": "C1", "threadReplyMatcher": map[string]any{"messageContains": "approve", "posterUserIds": []string{"U2"}}}
	configurationCall(t, ctx, host, admission(revision), http.MethodPut, slackTriggerPath, map[string]any{"configuration": reply}, 200)
	revision++
	assertTriggerGaps(t, ctx, host, gapsAdmission, digest, "slack-thread-approval-start")
	assertReplyConfigured(t, ctx, host, admission(revision), true)
	start := map[string]any{"channelId": "C1", "threadTriggerMatcher": map[string]any{"messageContains": "request approval"}}
	configurationCall(t, ctx, host, admission(revision), http.MethodPut,
		"/api/v2/connector-trigger-bindings/slack/slack-workspace/channelThreadCreated/slack-thread-approval-start", map[string]any{"configuration": start}, 200)
	assertTriggerGaps(t, ctx, host, gapsAdmission, digest)
	revision++
	assertOperationUseGaps(t, ctx, host, gapsAdmission, digest, completionGap)
	completionPath := "/api/v2/connector-use-configurations/slack/slack-workspace/postThreadReply/" + completion.FlowName + "/PostSlackCompletion"
	// A saved use whose required unit is blank stays a gap and unconfigured.
	configurationCall(t, ctx, host, admission(revision), http.MethodPut, completionPath, map[string]any{"configuration": map[string]any{"text": " "}}, 200)
	revision++
	assertOperationUseGaps(t, ctx, host, gapsAdmission, digest, completionGap)
	if completionUse(t, ctx, host, admission(revision)).Configured {
		t.Fatal("the view shows the completion use configured with a blank required unit")
	}
	configurationCall(t, ctx, host, admission(revision), http.MethodPut, completionPath, map[string]any{"configuration": map[string]any{"text": "Approved"}}, 200)
	revision++
	assertOperationUseGaps(t, ctx, host, gapsAdmission, digest)
	if !completionUse(t, ctx, host, admission(revision)).Configured {
		t.Fatal("the view does not show the saved completion use configured")
	}
	// Only Live's next publish follows Preview: on Preview a binding and a Step setting refuse it, and a
	// request that both saves and follows is malformed.
	for _, path := range []string{slackTriggerPath, completionPath} {
		refusedFollow := configurationCall(t, ctx, host, admission(revision), http.MethodPut, path, map[string]any{"followPreview": true}, 409)
		if !strings.Contains(string(refusedFollow), itemCannotFollowCode) {
			t.Fatalf("following Preview on Preview at %s was not refused with %s", path, itemCannotFollowCode)
		}
		configurationCall(t, ctx, host, admission(revision), http.MethodPut, path, map[string]any{"followPreview": true, "configuration": map[string]any{}}, 400)
	}
	assertOperationUseGaps(t, ctx, host, gapsAdmission, digest)
	t.Logf("real MinIO + official Slack v0.22.1 release + generated FDG: project=%s; trigger units offered, undeclared path refused, "+
		"bindings missing a required unit reported by unit, both bindings saved and their gaps closed; the PostSlackCompletion use was a gap "+
		"naming its Flow, Step, operation and unit until saved with a value; following Preview refused on Preview", id)
}

// stepUseView is a Step's operation use in the connections view.
type stepUseView struct {
	FlowName    string `json:"flowName"`
	StepName    string `json:"stepName"`
	OperationID string `json:"operationId"`
	Configured  bool   `json:"configured"`
}

// completionUse is the view's PostSlackCompletion use of postThreadReply.
func completionUse(t *testing.T, ctx context.Context, host *ConfigurationHost, admission ConfigurationAdmission) stepUseView {
	t.Helper()
	var list struct {
		Connections []struct {
			Uses []stepUseView `json:"uses"`
		} `json:"connections"`
	}
	storageRequire(t, json.Unmarshal(configurationCall(t, ctx, host, admission, http.MethodGet, "/api/v2/connector-connections", nil, 200), &list), "decode the connections view")
	for _, use := range list.Connections[0].Uses {
		if use.StepName == "PostSlackCompletion" && use.OperationID == "postThreadReply" {
			return use
		}
	}
	t.Fatalf("the view lists no PostSlackCompletion use among %+v", list.Connections[0].Uses)
	return stepUseView{}
}

// assertOperationUseGaps requires the gap report to list exactly the
// operation use gaps named in want, in order.
func assertOperationUseGaps(t *testing.T, ctx context.Context, host *ConfigurationHost, admission ConfigurationAdmission, digest string, want ...string) {
	t.Helper()
	data := configurationCall(t, ctx, host, admission, http.MethodPost, "/api/v2/project-configuration/requirements", RequirementsRequest{ManifestDigest: digest}, 200)
	var report RequirementsReport
	storageRequire(t, json.Unmarshal(data, &report), "decode the requirement gap report")
	names := []string{}
	for _, gap := range report.Gaps {
		if gap.Kind == appmanifest.GapOperationConfigurationRequired {
			names = append(names, gap.Name())
		}
	}
	if !slices.Equal(names, want) {
		t.Fatalf("the gap report lists the operation use gaps %v, want %v", names, want)
	}
}

// assertReplyConfigured requires the connections view to show the reply
// binding as configured exactly when want.
func assertReplyConfigured(t *testing.T, ctx context.Context, host *ConfigurationHost, admission ConfigurationAdmission, want bool) {
	t.Helper()
	var list struct {
		Connections []struct {
			TriggerUses []struct {
				BindingName string `json:"bindingName"`
				Configured  bool   `json:"configured"`
			} `json:"triggerUses"`
		} `json:"connections"`
	}
	storageRequire(t, json.Unmarshal(configurationCall(t, ctx, host, admission, http.MethodGet, "/api/v2/connector-connections", nil, 200), &list), "decode the connections view")
	for _, use := range list.Connections[0].TriggerUses {
		if use.BindingName == "slack-thread-approval-reply" && use.Configured != want {
			t.Fatalf("the view shows the reply binding configured=%t, want %t", use.Configured, want)
		}
	}
}

// assertTriggerGaps requires the gap report to list a trigger binding gap for
// exactly the bindings in want, each with ": <unit label>" when it names a
// missing required unit, in declaration order, besides the connection's own gap.
func assertTriggerGaps(t *testing.T, ctx context.Context, host *ConfigurationHost, admission ConfigurationAdmission, digest string, want ...string) {
	t.Helper()
	data := configurationCall(t, ctx, host, admission, http.MethodPost, "/api/v2/project-configuration/requirements", RequirementsRequest{ManifestDigest: digest}, 200)
	var report RequirementsReport
	storageRequire(t, json.Unmarshal(data, &report), "decode the requirement gap report")
	bindings := []string{}
	for _, gap := range report.Gaps {
		if gap.Kind == appmanifest.GapTriggerBindingRequired || gap.Kind == appmanifest.GapTriggerBindingInvalid {
			name := gap.BindingName
			if gap.UnitLabel != "" {
				name += ": " + gap.UnitLabel
			}
			bindings = append(bindings, name)
		}
	}
	if !slices.Equal(bindings, want) {
		t.Fatalf("the gap report lists the trigger bindings %v, want %v", bindings, want)
	}
}
