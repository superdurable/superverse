// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/gen/dexpb"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	projectsconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
	"github.com/superdurable/superversev2/internal/dexai/studio/confighost/connectormanifest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connections"
)

// SlackRelayTimeoutForTest bounds a SlackRelayPreviewForTest scenario and its
// events.
const SlackRelayTimeoutForTest = mockTurnTimeout + 3*previewCommandTimeout + 10*time.Minute

// The application of the slack-relay-trigger scenario: its Slack connection,
// the binding that starts SlackMessageFlow, and the Slack release.
const (
	SlackRelayConnectionForTest = "support"
	slackRelayBinding           = "support-message"
	slackRelayTrigger           = "channelThreadCreated"
	slackModulePath             = "github.com/superdurable/dex-connectors-library/connectors/slack"
	slackVersion                = "v1.0.0"
)

// SlackRelayPreviewForTest is a throwaway Project whose Preview serves the
// application of the slack-relay-trigger scenario on a seeded platform Slack
// connection of a random workspace, with a channel binding. The Studio Runs
// that the external test reads the Preview's Flows with import this package,
// so only these exports reach the in-package helpers.
type SlackRelayPreviewForTest struct {
	Service   *SessionFlowService
	Projects  *projects.ProjectFlowClient
	Objects   *objectstore.Store
	DAO       *connectiondao.ConnectionDAO
	ProjectID string
	Account   string
	// PreviewURL is the Preview's public URL; Team and Channel are the Slack
	// workspace and channel the binding names; Connection is the seeded
	// platform connection.
	PreviewURL, Team, Channel string
	Connection                connectiondao.Connection
	session                   *mockSession
}

// NewSlackRelayPreviewForTest runs the slack-relay-trigger scenario, seeds a
// platform Slack connection with placeholder tokens through connectiondao and
// the credential store (the local lanes have no Slack), selects it for the
// Preview with a channel binding as a person does in Studio, starts the
// Preview, and waits until the application's start registered its connection
// name.
func NewSlackRelayPreviewForTest(t *testing.T, ctx context.Context) *SlackRelayPreviewForTest {
	t.Helper()
	session := newMockSession(t, ctx, mockInput(t, "slack-relay-trigger", 1).Content)
	requireNoFailure(t, session.finishedTurn(ctx).History)
	_, messages := session.history(ctx)
	t.Logf("the scenario's turn:\n%s", describeMessages(messages[max(0, len(messages)-8):]))
	if pushed := lastToolResult(t, conversation.HistoryPage{Messages: messages}, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded ||
		pushed.ResultCode != agentloop.CommitBlockedForTest {
		t.Fatalf("the Slack trigger was not pushed as a checkpoint: %+v; the turn ended with:\n%s", pushed, describeMessages(messages[max(0, len(messages)-12):]))
	}
	db := postgrestest.OpenForTest(t)
	dao, objects := connectiondao.New(db), artifactStore(ctx, t)
	team, channel := "T"+strings.ToUpper(flowid.RandomID(9)), "C"+strings.ToUpper(flowid.RandomID(9))
	slack := seedPlatformSlackConnection(ctx, t, dao, objects, session.teamSpaceID, session.account, connectiondao.ProviderAccount{AccountID: team})
	session.seeded = map[string]string{SlackRelayConnectionForTest: slack.ConnectionID}
	session.bindings = []triggerBindingConfiguration{{connectorID: connections.SlackConnectorID, connectionName: SlackRelayConnectionForTest, triggerName: slackRelayTrigger,
		bindingName: slackRelayBinding, configuration: map[string]any{"channelId": channel}}}
	session.preparePreview(ctx)
	session.configurePreview(ctx)
	started := session.startPreview(ctx)
	project := session.identity.ProjectID
	session.await(ctx, "the Slack trigger registration", func() bool {
		var count int
		comm.MustDo(db.QueryRowContext(ctx, `SELECT count(*) FROM slack_trigger_registrations WHERE project_id = $1 AND scope = $2
			AND connection_id = $3 AND connection_name = $4`, project, infra.ScopePreview.String(), slack.ConnectionID, SlackRelayConnectionForTest).Scan(&count))
		return count == 1
	})
	t.Logf("FlowID=%s Preview of %s serves %s; Slack team %s channel %s connection %s registered", session.id, project, started.Preview.URL, team, channel, slack.ConnectionID)
	return &SlackRelayPreviewForTest{Service: session.service(), Projects: session.projects, Objects: objects, DAO: dao, ProjectID: project, Account: session.account,
		PreviewURL: started.Preview.URL, Team: team, Channel: channel, Connection: slack, session: session}
}

// ScopeTokens reads scope tokens from the Projects' configuration storage, as
// the server's relay does.
func (p *SlackRelayPreviewForTest) ScopeTokens(t *testing.T) *projectsconfig.Configuration {
	return &projectsconfig.Configuration{Projects: p.Projects, Storage: laneStorage(t)}
}

// Stop stops the Preview and waits until it is stopped.
func (p *SlackRelayPreviewForTest) Stop(ctx context.Context) { p.session.stopPreview(ctx) }

// Await polls done until it holds; past ctx the test fails.
func (p *SlackRelayPreviewForTest) Await(ctx context.Context, what string, done func() bool) {
	p.session.await(ctx, what, done)
}

// seedPlatformSlackConnection creates a READY platform Slack connection of
// Slack v1.0.0 installed in account, with placeholder tokens, as an
// authorization with the platform's Slack application would.
func seedPlatformSlackConnection(ctx context.Context, t *testing.T, dao *connectiondao.ConnectionDAO, objects *objectstore.Store, teamSpaceID, actor string,
	account connectiondao.ProviderAccount) connectiondao.Connection {
	t.Helper()
	now := time.Now().UTC()
	name := "slack-" + flowid.RandomID(8)
	c, inserted, err := dao.Create(ctx, connectiondao.Connection{
		TeamSpaceID: teamSpaceID, ConnectionID: "c" + flowid.RandomID(11), CreationFingerprint: name, Name: name, ConnectorID: connections.SlackConnectorID,
		ModulePath: slackModulePath, VersionLine: connectormanifest.VersionLine(slackVersion), ManifestVersion: slackVersion,
		ClientSource: connectiondao.ClientPlatform, Settings: json.RawMessage(`{}`), Revision: 1, CredentialStatus: connectiondao.CredentialAuthorizing,
		ConnectionStatus: connectiondao.ConnectionActive, CreatorAccountID: actor, UpdatedByAccountID: actor, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || !inserted {
		t.Fatalf("create the platform Slack connection: inserted=%t (%v)", inserted, err)
	}
	values := map[string]json.RawMessage{
		"bot_token": comm.Must(json.Marshal("xoxb-relay-test-" + flowid.RandomID(12))), "user_token": comm.Must(json.Marshal("xoxp-relay-test-" + flowid.RandomID(12))),
	}
	authorized, err := connections.AuthorizeForTest(ctx, dao, objects, c, values, account, actor)
	if err != nil {
		t.Fatalf("authorize the platform Slack connection (%v)", err)
	}
	return authorized
}

// RequireRegistrationRefusals posts registrations to the lane server's
// credential read listener: an unknown token is 401, a request naming a URL
// is 400, the scope's own token registering a connection the scope does not
// use answers CONNECTION_NOT_SELECTED, and its own connection REGISTERED.
func (p *SlackRelayPreviewForTest) RequireRegistrationRefusals(ctx context.Context, t *testing.T) {
	requireRegistrationRefusals(ctx, t, p.session, p.DAO, p.Connection)
}

func requireRegistrationRefusals(ctx context.Context, t *testing.T, session *mockSession, dao *connectiondao.ConnectionDAO, slack connectiondao.Connection) {
	t.Helper()
	project := session.identity.ProjectID
	ref, err := dao.ScopeToken(ctx, project, infra.ScopePreview.String())
	if err != nil {
		t.Fatalf("the Preview has no scope token (%T)", err)
	}
	configuration := &projectsconfig.Configuration{Projects: session.projects, Storage: laneStorage(t)}
	token, err := configuration.ReadScopeToken(ctx, project, infra.ScopePreview.String(), ref)
	if err != nil {
		t.Fatalf("the Preview's scope token is unreadable (%T)", err)
	}
	register := func(token string, body any) (int, string) {
		request := comm.Must(http.NewRequestWithContext(ctx, http.MethodPost, dexflowtest.CredentialReadHostOriginForTest(t)+"/internal/slack-triggers/register",
			bytes.NewReader(comm.Must(json.Marshal(body)))))
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("the registration endpoint did not answer (%T)", err)
		}
		defer func() { _ = response.Body.Close() }()
		var answer struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&answer)
		return response.StatusCode, answer.Status
	}
	if status, _ := register("unknown-"+flowid.RandomID(20), map[string]string{"sharedConnectionId": slack.ConnectionID, "connectionName": SlackRelayConnectionForTest}); status != http.StatusUnauthorized {
		t.Fatalf("an unknown token registered with %d", status)
	}
	if status, _ := register(token, map[string]string{"sharedConnectionId": slack.ConnectionID, "connectionName": SlackRelayConnectionForTest, "url": "http://169.254.169.254/"}); status != http.StatusBadRequest {
		t.Fatalf("a registration naming a URL answered %d", status)
	}
	if status, answer := register(token, map[string]string{"sharedConnectionId": "c" + flowid.RandomID(11), "connectionName": SlackRelayConnectionForTest}); status != http.StatusOK || answer != "CONNECTION_NOT_SELECTED" {
		t.Fatalf("a registration of a connection the scope does not use answered %d %s", status, answer)
	}
	if status, answer := register(token, map[string]string{"sharedConnectionId": slack.ConnectionID, "connectionName": SlackRelayConnectionForTest}); status != http.StatusOK || answer != "REGISTERED" {
		t.Fatalf("the scope's own registration answered %d %s", status, answer)
	}
}

// previewFlows dials the Preview's own Dex engine, as Studio Runs does, and
// calls read with its FlowService.
func (p *SlackRelayPreviewForTest) previewFlows(ctx context.Context, read func(dexpb.FlowServiceClient)) {
	s := p.session
	s.t.Helper()
	view := s.workspace(ctx)
	instance := sandbox.Instance{Identity: sandbox.Identity{ProjectID: p.ProjectID}, WorkspaceRequestID: view.InstanceID, Generation: sandbox.InstanceGeneration,
		Resources: view.Resources}
	connection, release, err := laneConnections(s.t).DialSandboxDex(ctx, instance.DexTarget())
	if err != nil {
		s.t.Fatalf("the Preview's Dex is unreachable (%v)", err)
	}
	defer func() { _ = release() }()
	read(dexpb.NewFlowServiceClient(connection))
}

// FlowStatus is the engine status of a Flow of the Preview's engine, and
// whether it exists.
func (p *SlackRelayPreviewForTest) FlowStatus(ctx context.Context, flowID string) (dexpb.FlowStatus, bool) {
	var status dexpb.FlowStatus
	found := false
	p.previewFlows(ctx, func(flows dexpb.FlowServiceClient) {
		summary, err := flows.GetFlowSummary(ctx, &dexpb.GetFlowSummaryRequest{FlowId: flowID})
		if err == nil {
			status, found = summary.GetFlowStatus(), true
		}
	})
	return status, found
}

// FlowIDs lists the FlowIDs of flowType in the Preview's engine.
func (p *SlackRelayPreviewForTest) FlowIDs(ctx context.Context, flowType string) []string {
	var ids []string
	p.previewFlows(ctx, func(flows dexpb.FlowServiceClient) {
		page, err := flows.SearchFlows(ctx, &dexpb.SearchFlowsRequest{Query: "FlowType = '" + flowType + "'", PageSize: 100})
		if err != nil {
			p.session.t.Fatalf("the Preview's Flow search failed (%T)", err)
		}
		for _, flow := range page.GetFlowRuns() {
			if !slices.Contains(ids, flow.GetFlowId()) {
				ids = append(ids, flow.GetFlowId())
			}
		}
	})
	slices.Sort(ids)
	return ids
}
