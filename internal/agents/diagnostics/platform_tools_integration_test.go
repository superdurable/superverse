// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	deploymentsdocker "github.com/superdurable/superversev2/internal/delivery/deployments/docker"
	deploymentskube "github.com/superdurable/superversev2/internal/delivery/deployments/kube"
	"github.com/superdurable/superversev2/internal/projects/infra"
	infradocker "github.com/superdurable/superversev2/internal/projects/infra/docker"
	"github.com/superdurable/superversev2/internal/projects/infra/kube/connect"
	"github.com/superdurable/superversev2/internal/projects/infra/kube/kubetest"
	"github.com/superdurable/superversev2/internal/studio/confighost"
	"github.com/superdurable/superversev2/internal/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/tenancy/connections"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/agents/sandbox/provision"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/session/toolinput/generated"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/delivery/deployments"
	"github.com/superdurable/superversev2/internal/delivery/releases"
	"github.com/superdurable/superversev2/internal/delivery/releases/build"
	buildlocal "github.com/superdurable/superversev2/internal/delivery/releases/build/local"
	"github.com/superdurable/superversev2/internal/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/platform/deploymentnames"
	"github.com/superdurable/superversev2/internal/platform/objectstore"
	"github.com/superdurable/superversev2/internal/projects"
	projectconfig "github.com/superdurable/superversev2/internal/projects/config"
	"github.com/superdurable/superversev2/internal/studio/runs"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/superdurable/superversev2/internal/delivery/deliverydao"
	"github.com/superdurable/superversev2/internal/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/projects/projectdao"
)

// These scenarios run the platform tools of the Session of an existing Project
// of the local lane that SUPERVERSE_TEST_LANE names against the lane's real platform Dex, the
// Project's Live engine, Kubernetes or Docker, MinIO and the local build fixture. Nothing is started, changed
// or substituted, and no tool output, configured value or credential is
// logged: only identities and pass or fail.

// platformTools is the reader a server composes, bound to the Session of an
// existing Project.
type platformTools struct {
	reader   *ProjectDiagnostics
	identity agentloop.SessionIdentity
	// secrets are the values the fixture's application printed and stored;
	// no tool result may contain any of them.
	secrets []string
}

func newPlatformTools(t *testing.T) *platformTools {
	t.Helper()
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	projectID := dexflowtest.Setting(t, "SUPERVERSE_TEST_DIAGNOSTICS_PROJECT_ID")
	runtime := newLocalRuntime(t)
	objects := artifactStore(t)
	token, err := os.ReadFile(dexflowtest.Setting(t, "SUPERVERSE_LOCAL_BUILD_TOKEN_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	builder, err := buildlocal.New(dexflowtest.Setting(t, "SUPERVERSE_LOCAL_BUILD_URL"), strings.TrimSpace(string(token)))
	if err != nil {
		t.Fatal(err)
	}
	db := postgrestest.OpenForTest(t)
	projectClient := &projects.ProjectFlowClient{Flow: &projects.ProjectFlow{}, Projections: projectdao.New(db), ProjectIDLength: len(projectID), PlatformAdminEmail: "admin@superverse.test"}
	releaseFlow := releases.NewProjectReleaseFlow(projectClient, nil, builder, build.NewProfiles(), objects, deploymentnames.EnvironmentTest, nil, nil)
	releaseClient := releases.NewProjectReleaseFlowClient(releaseFlow, projectClient, build.NewProfiles())
	deploymentClient := deployments.NewProjectDeployFlowClient(projectClient, nil, nil)
	deploymentClient.Releases = releaseClient
	releaseClient.Rows, deploymentClient.Rows = deliverydao.New(db), deliverydao.New(db)
	previewClient := &preview.PreviewFlowClient{Flow: &preview.PreviewFlow{}, Execution: dexflowtest.ExecutionReaderForTest(t, address)}
	client := dexflowtest.ClientForTest(t, address, nil, projectClient.Flow, previewClient.Flow, &provision.SandboxProvisionFlow{}, releaseFlow, &deployments.ProjectDeployFlow{})
	projectClient.Dex, previewClient.Dex = client, client
	// A Live configuration that selects TeamSpace connections redacts their stored values.
	providers := connections.NewProviderHTTPClient(true)
	credentials := connections.NewCredentialReader(connectiondao.New(db), objects,
		connectormanifest.NewReleaseResolver(connectormanifest.NewReleaseHTTPClient(), connectorlibrary.OfficialReleaseDownloadURL), providers, connections.PlatformClients{}, slog.Default())
	configuration := &projectconfig.Configuration{Projects: projectClient, Objects: objects, Storage: runtime.storage,
		Shared: confighost.SharedConnections{Providers: providers, Reader: credentials}}
	t.Cleanup(configuration.Close)
	reader := &ProjectDiagnostics{
		Projects: projectClient, Previews: previewClient, Releases: releaseClient, Deployments: deploymentClient,
		Configuration: configuration, Platform: dexflowtest.ExecutionReaderForTest(t, address),
		Serving: runtime.serving, Redactor: runtime.redactor,
		Publishing: deployments.NewPublishingFlowService(projectClient, deploymentClient, configuration, nil, deployments.BuildLogReads{Redactor: runtime.redactor, PerSecond: 2}),
		Runs:       &runs.Runs{Projects: projectClient, Deployments: deploymentClient, Objects: objects, Connections: runtime.connections, Names: deploymentnames.MustFor(deploymentnames.EnvironmentTest), Logger: slog.Default()},
	}
	tools := &platformTools{reader: reader, identity: agentloop.SessionIdentity{ProjectID: projectID, Profile: agentloop.ProfileSessionV1}}
	var fixture struct {
		Values []string `json:"values"`
	}
	data, err := os.ReadFile(dexflowtest.Setting(t, "SUPERVERSE_TEST_DIAGNOSTICS_SECRETS_FILE"))
	if err != nil || json.Unmarshal(data, &fixture) != nil || len(fixture.Values) == 0 {
		t.Fatal("requires the private file of the values the fixture application printed and stored")
	}
	tools.secrets = fixture.Values
	return tools
}

// call runs one tool call as a Session Step does and returns its outcome and
// decoded content.
func (tools *platformTools) call(t *testing.T, name conversation.ToolName, arguments string) (conversation.ToolOutcome, map[string]any, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	result, err := tools.reader.ReadDiagnostics(ctx, tools.identity, conversation.ToolCall{ID: conversation.CallID("call-" + string(name)), Name: name, Arguments: conversation.MustJSONObject(arguments)})
	if err != nil {
		t.Fatalf("%s failed (%T)", name, err)
	}
	if len(result.Content) > maximumOutputBytes {
		t.Fatalf("%s returned %d bytes, above the 16 KiB bound", name, len(result.Content))
	}
	if result.Outcome != conversation.ToolOutcomeSucceeded {
		// A failure is text; its structured content is its code.
		return result.Outcome, map[string]any{"code": result.FailureCode}, result.Content
	}
	var content map[string]any
	if err = json.Unmarshal([]byte(result.Content), &content); err != nil {
		t.Fatalf("%s returned content that is not one JSON object", name)
	}
	return result.Outcome, content, result.Content
}

// requireClean fails when output names a RunID, the Project's namespace or one
// of the fixture's secrets.
func (tools *platformTools) requireClean(t *testing.T, what, output string, runIDs []string) {
	t.Helper()
	if strings.Contains(output, deploymentnames.MustFor(deploymentnames.EnvironmentTest).ProjectNamespace(tools.identity.ProjectID)) {
		t.Fatalf("%s names the Project's internal namespace", what)
	}
	for _, runID := range runIDs {
		if runID != "" && strings.Contains(output, runID) {
			t.Fatalf("%s names a RunID", what)
		}
	}
	for _, secret := range tools.secrets {
		if strings.Contains(output, secret) {
			t.Fatalf("%s contains a configured value", what)
		}
	}
}

// platformRunIDs reads the RunIDs of the platform Flows, which no output may
// name.
func (tools *platformTools) platformRunIDs(t *testing.T, flowIDs []string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var runIDs []string
	for _, flowID := range flowIDs {
		summary, err := tools.reader.Platform.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
		if err != nil {
			t.Fatalf("read the summary of %s (%T)", flowID, err)
		}
		runIDs = append(runIDs, summary.GetFlowExecutionId().GetRunId(), summary.GetFirstRunId())
	}
	return runIDs
}

// maximumHistoryPages bounds the pages one history read follows.
const maximumHistoryPages = 60

// readHistoryIDs reads the history of a platform Flow as a model does: page by
// page, each page continuing from the cursor of the one before. It returns the
// event IDs in order and whether the cursors reached the end of the history
// within maximumHistoryPages. The IDs and the cursors only rise.
func (tools *platformTools) readHistoryIDs(t *testing.T, flowID string, pageSize int, runIDs []string) (ids []int64, ended bool) {
	t.Helper()
	after := int64(0)
	for range maximumHistoryPages {
		arguments := fmt.Sprintf(`{"flow_id":%q,"view":"history","page_size":%d,"after_event_id":%d}`, flowID, pageSize, after)
		outcome, page, output := tools.call(t, conversation.ToolReadPlatformFlow, arguments)
		if outcome != conversation.ToolOutcomeSucceeded {
			t.Fatalf("read_platform_flow history of %s after event %d answered %s", flowID, after, outcome)
		}
		tools.requireClean(t, "read_platform_flow history of "+flowID, output, runIDs)
		events, _ := page["events"].([]any)
		for _, item := range events {
			event, _ := item.(map[string]any)
			id, _ := event["event_id"].(float64)
			if len(ids) > 0 && int64(id) <= ids[len(ids)-1] {
				t.Fatalf("the history of %s repeats or reorders event %d", flowID, int64(id))
			}
			ids = append(ids, int64(id))
		}
		next, more := page["next_after_event_id"].(float64)
		if !more {
			return ids, true
		}
		if int64(next) <= after {
			t.Fatalf("the cursor of the history of %s stays at event %d", flowID, after)
		}
		after = int64(next)
	}
	return ids, false
}

// requireHistoryPaging reads the history of a platform Flow with small pages,
// which must show events and advance with every page. A closed Flow's history
// no longer changes, so pages of any size must show the same events: a cursor
// that stopped in front of the events beyond a stretch without any would end
// the read with small pages early.
func (tools *platformTools) requireHistoryPaging(t *testing.T, flowID string, runIDs []string) {
	t.Helper()
	small, smallEnded := tools.readHistoryIDs(t, flowID, 5, runIDs)
	if len(small) == 0 {
		t.Fatalf("the history of %s shows no event", flowID)
	}
	summary, err := tools.reader.Platform.GetFlowSummary(t.Context(), &dexpb.GetFlowSummaryRequest{FlowId: flowID})
	if err != nil {
		t.Fatalf("read the summary of %s (%T)", flowID, err)
	}
	if summary.GetFlowStatus() == dexpb.FlowStatus_FLOW_STATUS_RUNNING {
		return
	}
	large, largeEnded := tools.readHistoryIDs(t, flowID, 50, runIDs)
	if smallEnded && largeEnded && !slices.Equal(small, large) {
		t.Fatalf("the history of the closed Flow %s shows %d events in pages of 5 and %d in pages of 50", flowID, len(small), len(large))
	}
}

// The Project ran a Preview, and its Sandbox instance was reclaimed: the
// tools read the Project's delivery state and its platform Flows, its Preview
// and the completed provisioning of its instance included, without the
// workspace and without waking anything, refuse every FlowID outside the
// Project, the Session's own included, before any read, and name no RunID or
// internal resource.
func TestPlatformToolsProjectScopeIntegration(t *testing.T) {
	tools := newPlatformTools(t)
	foreignProjectID := dexflowtest.Setting(t, "SUPERVERSE_TEST_FOREIGN_PROJECT_ID")
	previewFlowID := preview.FlowID(tools.identity.ProjectID)
	before, err := tools.reader.Previews.Get(t.Context(), tools.identity.ProjectID)
	if err != nil || before.WorkspaceRequestID == "" {
		t.Fatal("requires a Project that ran a Preview")
	}
	provisionFlowID := provision.FlowIDForTest(tools.identity.ProjectID, before.WorkspaceRequestID)
	summary, err := tools.reader.Platform.GetFlowSummary(t.Context(), &dexpb.GetFlowSummaryRequest{FlowId: provisionFlowID})
	if err != nil || summary.GetFlowStatus() != dexpb.FlowStatus_FLOW_STATUS_COMPLETED || before.Resources.Instance.UID != "" {
		t.Fatal("requires a Project whose Preview released its reclaimed instance")
	}

	outcome, status, output := tools.call(t, conversation.ToolReadDeliveryStatus, `{}`)
	if outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("read_delivery_status answered %s", outcome)
	}
	previewStatus, _ := status["preview"].(map[string]any)
	if previewStatus["flow_id"] != previewFlowID || previewStatus["workspace_request_id"] != before.WorkspaceRequestID {
		t.Fatal("read_delivery_status did not read the Project's Preview")
	}
	flowIDs := []string{flowid.ProjectFlowID(tools.identity.ProjectID), previewFlowID, provisionFlowID}
	listed := []any{status["setup"]}
	for _, list := range []string{"releases", "deployments"} {
		items, _ := status[list].([]any)
		listed = append(listed, items...)
	}
	for _, item := range listed {
		if resource, isObject := item.(map[string]any); isObject {
			flowID, _ := resource["flow_id"].(string)
			flowIDs = append(flowIDs, flowID)
		}
	}
	runIDs := tools.platformRunIDs(t, flowIDs)
	tools.requireClean(t, "read_delivery_status", output, runIDs)

	for _, flowID := range flowIDs {
		for _, view := range []generated.ReadPlatformFlowView{generated.ReadPlatformFlowViewSummary, generated.ReadPlatformFlowViewState, generated.ReadPlatformFlowViewHistory} {
			outcome, _, output = tools.call(t, conversation.ToolReadPlatformFlow, `{"flow_id":"`+flowID+`","view":"`+string(view)+`"}`)
			if outcome != conversation.ToolOutcomeSucceeded && view != generated.ReadPlatformFlowViewState {
				t.Fatalf("read_platform_flow %s of %s answered %s", view, flowID, outcome)
			}
			tools.requireClean(t, "read_platform_flow "+string(view)+" of "+flowID, output, runIDs)
		}
		tools.requireHistoryPaging(t, flowID, runIDs)
	}

	refused := []string{
		flowid.ProjectFlowID(foreignProjectID),
		"setup-" + foreignProjectID + "-k3n8q5r1",
		"release-" + foreignProjectID + "-" + strings.Repeat("0", 40),
		"deploy-" + foreignProjectID + "-missing",
		preview.FlowID(foreignProjectID),
		provision.FlowIDForTest(foreignProjectID, before.WorkspaceRequestID),
		flowid.SessionFlowID(tools.identity.ProjectID),
		// The identities of a Session of the earlier format are not the
		// Project's.
		preview.FlowID(tools.identity.ProjectID) + "-othersession",
		provision.FlowIDForTest(tools.identity.ProjectID, "othersession-"+before.WorkspaceRequestID),
		provision.FlowIDForTest(tools.identity.ProjectID, "cleanup-1"),
		"text-summary-" + tools.identity.ProjectID,
	}
	for _, flowID := range refused {
		// A refused FlowID that does not exist is still flow_not_in_project, not
		// not_found: the refusal comes before any engine read.
		outcome, content, _ := tools.call(t, conversation.ToolReadPlatformFlow, `{"flow_id":"`+flowID+`","view":"history"}`)
		if outcome != conversation.ToolOutcomeKnownFailure || content["code"] != toolfailure.FlowNotInProject.String() {
			t.Fatalf("read_platform_flow of %s was not refused with flow_not_in_project", flowID)
		}
	}

	after, err := tools.reader.Previews.Get(t.Context(), tools.identity.ProjectID)
	if err != nil || after.Revision != before.Revision || after.WorkspaceRequestID != before.WorkspaceRequestID {
		t.Fatal("reading the Project changed its Preview")
	}
	t.Logf("project=%s read %d platform Flows with the instance reclaimed and refused %d FlowIDs", tools.identity.ProjectID, len(flowIDs), len(refused))
}

// The fixture's Live application printed a configured connection credential
// and the workload grant's storage key and stored a configured value in an
// Attribute of SUPERVERSE_TEST_DIAGNOSTICS_LEAKING_FLOW_ID: the Live run
// search, summary, state, history and log show placeholders instead, and no
// output names a RunID. The strict schema rejects a hydration field.
func TestLiveAppRunsRedactionIntegration(t *testing.T) {
	tools := newPlatformTools(t)
	flowID := dexflowtest.Setting(t, "SUPERVERSE_TEST_DIAGNOSTICS_LEAKING_FLOW_ID")
	outcome, content, _ := tools.call(t, conversation.ToolReadLiveAppRuns, `{"operation":"state","flow_id":"`+flowID+`","hydrate":true}`)
	if outcome != conversation.ToolOutcomeKnownFailure || content["code"] != toolfailure.InvalidArguments.String() {
		t.Fatal("read_live_app_runs accepted a hydration field")
	}
	runIDs := tools.liveRunIDs(t, flowID)

	outcome, summary, output := tools.call(t, conversation.ToolReadLiveAppRuns, `{"operation":"summary","flow_id":"`+flowID+`"}`)
	if outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("read_live_app_runs summary answered %s", outcome)
	}
	tools.requireClean(t, "Live summary", output, runIDs)
	flowType, _ := summary["flow_type"].(string)
	outcome, _, output = tools.call(t, conversation.ToolReadLiveAppRuns, `{"operation":"search","flow_type":"`+flowType+`"}`)
	if outcome != conversation.ToolOutcomeSucceeded || !strings.Contains(output, flowID) {
		t.Fatal("read_live_app_runs search did not list the fixture run")
	}
	tools.requireClean(t, "Live search", output, runIDs)
	for _, operation := range []generated.ReadLiveAppRunsOperation{generated.ReadLiveAppRunsOperationState, generated.ReadLiveAppRunsOperationHistory, generated.ReadLiveAppRunsOperationLogs} {
		outcome, _, output = tools.call(t, conversation.ToolReadLiveAppRuns, `{"operation":"`+string(operation)+`","flow_id":"`+flowID+`"}`)
		if outcome != conversation.ToolOutcomeSucceeded {
			t.Fatalf("read_live_app_runs %s answered %s", operation, outcome)
		}
		tools.requireClean(t, "Live "+string(operation), output, runIDs)
		if operation != generated.ReadLiveAppRunsOperationHistory && !strings.Contains(output, "[REDACTED]") {
			t.Fatalf("the Live %s shows no placeholder for the values the application printed and stored", operation)
		}
	}
	t.Logf("project=%s flow=%s: Live search, summary, state, history and log are redacted", tools.identity.ProjectID, flowID)
}

// liveRunIDs reads the RunIDs of a Live run straight from the Live engine.
func (tools *platformTools) liveRunIDs(t *testing.T, flowID string) []string {
	t.Helper()
	var runIDs []string
	err := tools.reader.Runs.InvokeLive(t.Context(), tools.identity.ProjectID, func(ctx context.Context, live runs.LiveEngine) error {
		summary, err := live.Flows.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
		runIDs = append(runIDs, summary.GetFlowExecutionId().GetRunId(), summary.GetFirstRunId())
		return err
	})
	if err != nil {
		t.Fatalf("requires a Live run %s on a READY Live deployment (%T)", flowID, err)
	}
	return runIDs
}

// The fixture's failed Release build echoed a frozen configuration value: its
// log tail in read_delivery_status shows a placeholder instead.
func TestBuildLogTailRedactionIntegration(t *testing.T) {
	tools := newPlatformTools(t)
	releaseID := dexflowtest.Setting(t, "SUPERVERSE_TEST_DIAGNOSTICS_FAILED_RELEASE_ID")
	outcome, status, output := tools.call(t, conversation.ToolReadDeliveryStatus, `{"release_id":"`+releaseID+`"}`)
	releasesRead, _ := status["releases"].([]any)
	if outcome != conversation.ToolOutcomeSucceeded || len(releasesRead) != 1 {
		t.Fatal("read_delivery_status did not read the named Release")
	}
	buildRead, _ := releasesRead[0].(map[string]any)["build"].(map[string]any)
	tail, _ := buildRead["log_tail"].(string)
	if buildRead["status"] != releases.Failed || tail == "" || len(tail) > maximumBuildLogBytes {
		t.Fatal("requires a Release whose latest build failed with a log")
	}
	if !strings.Contains(tail, "[REDACTED]") {
		t.Fatal("the build log tail shows no placeholder for the configured value the build echoed")
	}
	tools.requireClean(t, "build log tail", output, nil)
	t.Logf("project=%s release=%s: the failed build's log tail is redacted", tools.identity.ProjectID, releaseID)
}

// The three tools form one parallel wave without the workspace, as a model
// reply that calls them together does, and every branch of the wave reads
// concurrently.
func TestPlatformToolsParallelWaveIntegration(t *testing.T) {
	tools := newPlatformTools(t)
	calls := []conversation.ToolCall{
		{ID: "wave-1", Name: conversation.ToolReadDeliveryStatus, Arguments: conversation.MustJSONObject(`{}`)},
		{ID: "wave-2", Name: conversation.ToolReadPlatformFlow, Arguments: conversation.MustJSONObject(`{"flow_id":"` + flowid.ProjectFlowID(tools.identity.ProjectID) + `","view":"summary"}`)},
		{ID: "wave-3", Name: conversation.ToolReadLiveAppRuns, Arguments: conversation.MustJSONObject(`{"operation":"logs"}`)},
	}
	toolNames := agentloop.SessionPromptFor(conversation.ModelOpus55).ToolNames
	batch, needsWorkspace := agentloop.ParallelToolBatch(toolNames, agentloop.AgentState{PendingToolCalls: calls})
	if len(batch.Records) != len(calls) || needsWorkspace {
		t.Fatal("the platform tools did not form one wave without the workspace")
	}
	var wave sync.WaitGroup
	outcomes := make([]conversation.ToolOutcome, len(calls))
	for index, call := range calls {
		wave.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			result, err := tools.reader.ReadDiagnostics(ctx, tools.identity, call)
			if err == nil {
				outcomes[index] = result.Outcome
			}
		})
	}
	wave.Wait()
	for index, outcome := range outcomes {
		if outcome != conversation.ToolOutcomeSucceeded {
			t.Fatalf("wave branch %s answered %q", calls[index].Name, outcome)
		}
	}
	t.Logf("project=%s: a wave of %d platform tools read in parallel", tools.identity.ProjectID, len(calls))
}

// localRuntime is the project runtime of the local lane that the scenarios
// read, as the lane's server composes it.
type localRuntime struct {
	storage     infra.Storage
	redactor    infra.Redactor
	connections infra.Connections
	serving     deployments.Serving
}

// newLocalRuntime reaches the projects of the lane that SUPERVERSE_TEST_LANE
// names. Local Kind is the owned V2 Kind cluster, with the server's own scoped
// credential and its Services through port-forwards. Local Native is the local
// Docker Engine at SUPERVERSE_TEST_DOCKER_SOCKET with the lane's state
// directory SUPERVERSE_TEST_NATIVE_STATE_DIR, whose credential files the
// redaction reads; these scenarios create no network, so the network prefix
// is only the lane's default.
func newLocalRuntime(t *testing.T) localRuntime {
	t.Helper()
	if dexflowtest.LocalLaneForTest(t) == dexflowtest.LaneKindForTest {
		k := kubetest.InfrastructureForTest(t)
		return localRuntime{storage: k, redactor: k, connections: &connect.Connections{Infrastructure: k}, serving: &deploymentskube.LiveServing{Infrastructure: k}}
	}
	k, err := infradocker.New(infradocker.Config{Socket: dexflowtest.Setting(t, "SUPERVERSE_TEST_DOCKER_SOCKET"), Environment: deploymentnames.EnvironmentTest,
		StateDir: dexflowtest.Setting(t, "SUPERVERSE_TEST_NATIVE_STATE_DIR"), Instance: dexflowtest.NativeInstanceForTest(t),
		NetworkPrefix: netip.MustParsePrefix(dexflowtest.NativeNetworkPrefixForTest(t))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = k.Close() })
	return localRuntime{storage: k, redactor: k, connections: k, serving: &deploymentsdocker.LiveServing{Infrastructure: k}}
}

// artifactStore is the platform's versioned artifact bucket, which holds the
// Live deployment's definitions.
func artifactStore(t *testing.T) *objectstore.Store {
	t.Helper()
	endpoint, bucket := dexflowtest.Setting(t, "SUPERVERSE_S3_ENDPOINT"), dexflowtest.Setting(t, "SUPERVERSE_ARTIFACT_BUCKET")
	config, err := awsconfig.LoadDefaultConfig(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	store, err := objectstore.NewStore(s3.NewFromConfig(config, func(o *s3.Options) { o.BaseEndpoint = &endpoint; o.UsePathStyle = true }), bucket, "", "sv2-test", true)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
