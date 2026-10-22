// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost/localgit"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel/summarymodel"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/dexai/projects/infra/docker"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra/kube/kubetest"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

// A Project's Preview configuration is its own (infra.PreviewKeysOf), so a
// connection the AppManifest declares is unconfigured on every Project a test
// creates.
// Local verification uses the platform summary model by default. Real
// provider verification requires the same explicit approval as browser cases.
const (
	llmConnector   = "llm"
	openAIProvider = "openai"
)

// connectionList is the part of the configuration host's connection list that
// a person's form reads before it selects: the revisions it was read at and
// the connections the AppManifest declares.
type connectionList struct {
	ConfigurationRevision string `json:"configurationRevision"`
	AppManifestRevision   uint64 `json:"appManifestRevision"`
	DefinitionRevision    string `json:"definitionRevision"`
	Connections           []struct {
		ConnectorID    string `json:"connectorId"`
		ConnectionName string `json:"connectionName"`
		ModulePath     string `json:"modulePath"`
		ModuleVersion  string `json:"moduleVersion"`
		AuthMethodID   string `json:"authMethodId"`
	} `json:"connections"`
}

// configurePreview configures the Project's Preview as a person does in
// Studio (configureConnections), with projectconfig.Configuration as the
// server composes it, here in the test's process. The configuration is the
// source's the Preview prepared for the Session's HEAD, so the Preview must
// have prepared the HEAD; it names no Sandbox instance, and no workspace is
// needed. The configuration host reads the AppManifest of the HEAD's prepared
// source from the platform's artifact bucket (artifactStore), where the
// Preview stored it, as the server's Preview configuration does, and release
// candidates from the lane's local Git host (laneReleaseCandidates).
func (s *mockSession) configurePreview(ctx context.Context) *pb.ProjectConfigurationSnapshot {
	s.t.Helper()
	s.previews.Flow.Preview = preview.NewPreviewConfiguration(nil, nil, artifactStore(ctx, s.t), nil, nil, nil, nil)
	configuration := &projectconfig.Configuration{Projects: s.projects, Storage: laneStorage(s.t), Preview: s.service(), ReleaseCandidates: laneReleaseCandidates(s.t), Shared: laneTeamSpaceConnections(ctx, s.t, s.projects)}
	s.t.Cleanup(configuration.Close)
	return s.configureConnections(ctx, configuration, &pb.FlowApiTarget{ProjectId: s.identity.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW})
}

// configurationRevision is the revision of the configuration of target as a
// person's form reads it before it saves (ListConnections).
func (s *mockSession) configurationRevision(ctx context.Context, configuration *projectconfig.Configuration, target *pb.FlowApiTarget) (connectionList, uint64) {
	s.t.Helper()
	var list connectionList
	s.invokeConfiguration(ctx, configuration, &pb.FlowApiContext{Target: target}, projectconfig.ReadAccess, http.MethodGet, "/api/v2/connector-connections", nil, &list)
	revision, err := strconv.ParseUint(list.ConfigurationRevision, 10, 64)
	if err != nil {
		s.t.Fatalf("the connection list names no configuration revision (%T)", err)
	}
	return list, revision
}

// configureConnections configures the configuration of target, the Preview's
// or Live's next-publish layer, as a person does in Studio, for the Session's owner, whom
// the API would have authenticated. It lists the connections of target's
// source (ListConnections); creates in the Project's TeamSpace, as the
// TeamSpace's Connections page does, a connection of every declared llm
// connection with the browser cases' mock summary model by default, and of
// every connection of a connector that
// declares no authentication method, such as a public feed, with its defaults
// and no credential, unless the test seeded the connection name's connection
// itself (seeded); selects each one for its connection name
// (SelectTeamSpaceConnection); saves the test's trigger bindings; and validates the revision it wrote
// (ValidateProjectConfiguration), which returns the READY configuration. It
// needs SUPERVERSE_TEST_LANE with the lane's storage inputs (laneStorage). The
// key stays in this process: no failure, log or document it reports carries
// the key or a configuration document.
func (s *mockSession) configureConnections(ctx context.Context, configuration *projectconfig.Configuration, target *pb.FlowApiTarget) *pb.ProjectConfigurationSnapshot {
	s.t.Helper()
	key := summaryKey(s.t)
	list, revision := s.configurationRevision(ctx, configuration, target)
	var err error
	selected := 0
	for _, connection := range list.Connections {
		var settings, credentials map[string]json.RawMessage
		connectionID, seeded := s.seeded[connection.ConnectionName]
		switch {
		case seeded:
		case connection.ConnectorID == llmConnector:
			settings = map[string]json.RawMessage{"provider": comm.Must(json.Marshal(openAIProvider)), "model": comm.Must(json.Marshal(summaryModel(s.t)))}
			credentials = map[string]json.RawMessage{"api_key": comm.Must(json.Marshal(key))}
		case connection.AuthMethodID == "":
			settings, credentials = map[string]json.RawMessage{}, map[string]json.RawMessage{}
		default:
			s.t.Fatalf("the source declares the %s connection %s, which only a person can configure", connection.ConnectorID, connection.ConnectionName)
		}
		if !seeded {
			connectionID = s.createConnection(ctx, configuration, connection.ConnectionName, connection.ConnectorID, connection.ModulePath, connection.ModuleVersion, connection.AuthMethodID, settings, credentials)
		}
		form := &pb.FlowApiContext{Target: target, DefinitionRevision: list.DefinitionRevision, AppManifestRevision: list.AppManifestRevision, ConfigurationRevision: revision}
		var answer struct {
			ConfigurationRevision string `json:"configurationRevision"`
		}
		route := "/api/v2/connector-selections/" + url.PathEscape(connection.ConnectorID) + "/" + url.PathEscape(connection.ConnectionName)
		s.invokeConfiguration(ctx, configuration, form, projectconfig.WriteAccess, http.MethodPut, route, map[string]string{"teamSpaceConnectionId": connectionID}, &answer)
		if revision, err = strconv.ParseUint(answer.ConfigurationRevision, 10, 64); err != nil {
			s.t.Fatalf("the selection of %s names no configuration revision", connection.ConnectionName)
		}
		selected++
	}
	if selected == 0 {
		s.t.Fatal("the source declares no connection to configure")
	}
	for _, binding := range s.bindings {
		form := &pb.FlowApiContext{Target: target, DefinitionRevision: list.DefinitionRevision, AppManifestRevision: list.AppManifestRevision, ConfigurationRevision: revision}
		var answer struct {
			ConfigurationRevision string `json:"configurationRevision"`
		}
		route := "/api/v2/connector-trigger-bindings/" + url.PathEscape(binding.connectorID) + "/" + url.PathEscape(binding.connectionName) + "/" +
			url.PathEscape(binding.triggerName) + "/" + url.PathEscape(binding.bindingName)
		s.invokeConfiguration(ctx, configuration, form, projectconfig.WriteAccess, http.MethodPut, route, map[string]any{"configuration": binding.configuration}, &answer)
		if revision, err = strconv.ParseUint(answer.ConfigurationRevision, 10, 64); err != nil {
			s.t.Fatalf("the trigger binding %s names no configuration revision", binding.bindingName)
		}
	}
	requestID := uuid.NewString()
	validated := &pb.ProjectConfigurationTarget{ProjectId: target.GetProjectId(), Scope: target.GetScope(), ReleaseId: target.GetReleaseId(), NextPublish: target.GetNextPublish()}
	snapshot, err := configuration.Validate(ctx, s.account, validated, requestID, revision)
	if err != nil {
		s.t.Fatalf("the configuration revision %d did not validate (%T)", revision, err)
	}
	if snapshot.GetStatus() != pb.ConfigurationStatus_CONFIGURATION_STATUS_READY || snapshot.GetValidation().GetRequestId() != requestID || snapshot.GetValidation().GetConfigurationRevision() != revision {
		s.t.Fatalf("the validated configuration is %s at revision %d", snapshot.GetStatus(), snapshot.GetValidation().GetConfigurationRevision())
	}
	s.t.Logf("FlowID=%s configured %s of %s: %d connection(s), configuration revision %d READY", s.id, target.GetScope(), snapshot.GetSourceCommit(), selected, revision)
	return snapshot
}

// laneTeamSpaceConnections is the TeamSpace connections of the lane's PostgreSQL and
// platform bucket as the server composes them: releases and release
// candidates of the lane's local Git host, and the credential read endpoint
// as the lane's applications reach it, which the configuration's credential
// broker names. A freeze reads the Project's TeamSpace from projects.
func laneTeamSpaceConnections(ctx context.Context, t *testing.T, projectClient *projects.ProjectFlowClient) confighost.SharedConnections {
	t.Helper()
	dao, objects := connectiondao.New(postgrestest.OpenForTest(t)), artifactStore(ctx, t)
	releaseClient := connectormanifest.NewReleaseHTTPClient()
	releases := connectormanifest.NewReleaseResolver(releaseClient, laneReleaseCandidates(t))
	providers := connections.NewProviderHTTPClient(true)
	reader := connections.NewCredentialReader(dao, objects, releases, providers, connections.PlatformClients{}, slog.Default())
	return confighost.SharedConnections{
		Connections: connections.NewTeamSpaceConnections(dao, objects, releases, connectormanifest.NewCatalog(releaseClient, connectorlibrary.NewPublishedCatalog(releaseClient)), connections.PlatformClients{}, providers, reader, nil, true),
		Reader:      reader, Usages: connections.NewUsages(dao, objects), ReadURL: dexflowtest.CredentialReadURLForTest(t), Providers: providers,
		ProjectTeamSpace: func(ctx context.Context, projectID string) (string, error) {
			project, err := projectClient.Read(ctx, projectID)
			return project.TeamSpaceID, err
		},
	}
}

// invokeConfiguration sends one configuration operation as the API maps it:
// the fixed route, the JSON body and the form's revisions, for the Session's
// owner. It decodes the answered document into output; a failure names the
// route, the status and the host's safe code, never a document.
func (s *mockSession) invokeConfiguration(ctx context.Context, configuration *projectconfig.Configuration, form *pb.FlowApiContext, access projectconfig.Access, method, route string, input, output any) {
	s.t.Helper()
	var body []byte
	if input != nil {
		var err error
		if body, err = json.Marshal(input); err != nil {
			s.t.Fatalf("encode the %s %s body (%T)", method, route, err)
		}
	}
	document, err := configuration.InvokeForAccount(ctx, s.account, form, access, method, route, body)
	if err != nil {
		s.t.Fatalf("configuration %s %s failed (%T)", method, route, err)
	}
	if document.StatusCode != http.StatusOK {
		var coded struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(document.JSON, &coded)
		s.t.Fatalf("configuration %s %s answered %d %s", method, route, document.StatusCode, coded.Code)
	}
	if err = json.Unmarshal(document.JSON, output); err != nil {
		s.t.Fatalf("configuration %s %s answered an unreadable document (%T)", method, route, err)
	}
}

func usesMockSummary(t *testing.T) bool {
	t.Helper()
	switch os.Getenv("SUPERVERSE_E2E_LLM") {
	case "", "mock":
		return true
	case "real":
		if strings.TrimSpace(os.Getenv("SUPERVERSE_E2E_REAL_LLM_APPROVAL")) == "" {
			t.Fatal("real LLM verification requires the platform developer's recorded approval reason")
		}
		return false
	default:
		t.Fatal("SUPERVERSE_E2E_LLM must be mock or real")
		return false
	}
}

func summaryModel(t *testing.T) string {
	t.Helper()
	if usesMockSummary(t) {
		return summarymodel.ModelForTest
	}
	return "gpt-5-mini"
}

// summaryKey never reads a provider key on the default mock path.
func summaryKey(t *testing.T) string {
	t.Helper()
	if usesMockSummary(t) {
		return summarymodel.APIKeyForTest
	}
	data, err := os.ReadFile(dexflowtest.Setting(t, "SUPERVERSE_E2E_OPENAI_FILE")) //nolint:gosec // the operator's private credential file
	var file struct {
		APIKey string `json:"apiKey"`
	}
	if err != nil || json.Unmarshal(data, &file) != nil || !strings.HasPrefix(file.APIKey, "sk-") {
		t.Fatal(`SUPERVERSE_E2E_OPENAI_FILE must name a private file {"apiKey":"sk-..."}`)
	}
	return file.APIKey
}

// laneReleaseCandidates is where the local lane's server reads the release
// candidates of its contributions: the release store of its local Git host.
func laneReleaseCandidates(t *testing.T) string {
	return localgit.ConnectorLibraryReleaseDownloadURLForTest(dexflowtest.LocalGitURLForTest(t))
}

// laneStorage is the project storage of the local lane's runtime as the
// lane's server composes it, through which the configuration host opens a
// project's configuration: Local Kind's, as the platform server reached
// through port-forwards (SUPERVERSE_TEST_KIND_KUBECONFIG), or Local Native's
// Docker runtime at SUPERVERSE_TEST_DOCKER_SOCKET with the lane's state
// directory SUPERVERSE_TEST_NATIVE_STATE_DIR, whose credential files open a
// project's storage. It creates no network, so the network prefix is only the
// lane's default.
func laneStorage(t *testing.T) infra.Storage {
	t.Helper()
	if dexflowtest.LocalLaneForTest(t) == dexflowtest.LaneKindForTest {
		return kubetest.InfrastructureForTest(t)
	}
	k, err := infradocker.New(infradocker.Config{Socket: dexflowtest.Setting(t, "SUPERVERSE_TEST_DOCKER_SOCKET"), Environment: deploymentnames.EnvironmentTest,
		StateDir: dexflowtest.Setting(t, "SUPERVERSE_TEST_NATIVE_STATE_DIR"), Instance: dexflowtest.NativeInstanceForTest(t),
		NetworkPrefix: netip.MustParsePrefix(dexflowtest.NativeNetworkPrefixForTest(t))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	return k
}

// artifactStore is the platform's versioned artifact bucket, which holds the
// Preview's prepared sources: SUPERVERSE_S3_ENDPOINT and
// SUPERVERSE_ARTIFACT_BUCKET of the local lane, with the platform MinIO's
// credentials in the AWS environment, and the server's key prefix.
func artifactStore(ctx context.Context, t *testing.T) *objectstore.Store {
	t.Helper()
	endpoint, bucket := dexflowtest.Setting(t, "SUPERVERSE_S3_ENDPOINT"), dexflowtest.Setting(t, "SUPERVERSE_ARTIFACT_BUCKET")
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

// createConnection creates a connection of the declared connection in the
// Project's TeamSpace, as the TeamSpace's Connections page does, and returns
// its ID.
func (s *mockSession) createConnection(ctx context.Context, configuration *projectconfig.Configuration, connectionName, connectorID, modulePath, moduleVersion, authMethodID string,
	settings, credentials map[string]json.RawMessage) string {
	s.t.Helper()
	created, err := configuration.Shared.Connections.Create(ctx, connections.Creation{
		TeamSpaceID: s.teamSpaceID, RequestID: uuid.NewString(), ActorAccountID: s.account, Name: connectionName + "-" + uuid.NewString()[:8],
		Release:      connectormanifest.Module{ConnectorID: connectorID, ModulePath: modulePath, ModuleVersion: moduleVersion},
		AuthMethodID: authMethodID, Settings: settings, Credentials: credentials,
	})
	if err != nil {
		reason, _ := connections.Refusal(err)
		s.t.Fatalf("the TeamSpace connection of %s was not created (%T, refusal %q)", connectionName, err, reason.String())
	}
	return created.ConnectionID
}
