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
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
)

// This drives a real Native/Kind Project, source gate, Git service, Dex,
// Preview and summary HTTP API. No Project connection is selected and the
// TeamSpace has no LLM connection; the mock endpoint URL alone is not a selection.
func TestOptionalConnectionPreviewDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+previewDeployTimeout)
	defer cancel()
	session := newMockSession(t, ctx, mockInput(t, "build-basic", 1).Content)
	agent := session.finishedTurn(ctx)
	requireNoFailure(t, agent.History)
	if pushed := lastToolResult(t, agent.History, conversation.ToolCommitAndPush); pushed.Outcome != conversation.ToolOutcomeSucceeded {
		t.Fatalf("the independent homepage edit was not committed: %+v", pushed)
	}
	head := session.summary(ctx).Identity.HeadCommit
	if head == session.identity.HeadCommit {
		t.Fatal("the homepage edit left HEAD unchanged")
	}
	started := session.awaitDeployment(ctx, func(s preview.Snapshot) bool { return s.Deployment.Last.Commit == head })
	if started.Preview.Status != preview.PreviewReady || started.Deployment.Last.Status != preview.DeploymentReady || len(started.Preview.ConfigurationGaps) != 0 {
		t.Fatalf("optional connection blocked Preview: %s (%s)", started.Preview.Status, failureCode(started))
	}
	session.previews.Flow.Preview = preview.NewPreviewConfiguration(nil, nil, artifactStore(ctx, t), nil, nil, nil, nil)
	live := newLiveDelivery(session)
	for _, scope := range []pb.ProjectScope{pb.ProjectScope_PROJECT_SCOPE_PREVIEW, pb.ProjectScope_PROJECT_SCOPE_LIVE} {
		target := &pb.ProjectConfigurationTarget{ProjectId: session.identity.ProjectID, Scope: scope, NextPublish: scope == pb.ProjectScope_PROJECT_SCOPE_LIVE}
		snapshot, _, err := live.configuration.Get(ctx, session.account, target)
		if err != nil || snapshot.GetStatus() != pb.ConfigurationStatus_CONFIGURATION_STATUS_READY || len(snapshot.GetRequirementGaps()) != 0 {
			t.Fatalf("optional %s readiness: %s (%v)", scope, snapshot.GetStatus(), err)
		}
		if declarations := snapshot.GetAppManifest().GetConnectors(); len(declarations) != 1 || !declarations[0].GetOptional() {
			t.Fatalf("%s lost manifest optionality", scope)
		}
	}
	form := &pb.FlowApiContext{Target: &pb.FlowApiTarget{ProjectId: session.identity.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW}}
	var list struct {
		Connections []struct {
			Optional           bool   `json:"optional"`
			SharedConnectionID string `json:"sharedConnectionId"`
		} `json:"connections"`
	}
	session.invokeConfiguration(ctx, live.configuration, form, projectconfig.ReadAccess, http.MethodGet, "/api/v2/connector-connections", nil, &list)
	if len(list.Connections) != 1 || !list.Connections[0].Optional || list.Connections[0].SharedConnectionID != "" {
		t.Fatal("optional connection has an unexpected selection")
	}
	var count int
	if err := postgrestest.OpenForTest(t).QueryRowContext(ctx, "SELECT count(*) FROM team_space_connections WHERE team_space_id = $1 AND connector_id = 'llm'", session.teamSpaceID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("missing-connection case has %d TeamSpace LLM connections (%v)", count, err)
	}
	client := optionalPreviewClient(t, started.Preview.URL)
	if code, _ := optionalPreviewRequest(t, ctx, client, http.MethodGet, started.Preview.URL, nil); code != http.StatusOK {
		t.Fatalf("homepage answered %d", code)
	}
	id := uuid.NewString()
	body, _ := json.Marshal(map[string]string{"requestId": id, "text": "A Project can preview before its optional model is connected."})
	for range 2 {
		code, _ := optionalPreviewRequest(t, ctx, client, http.MethodPost, started.Preview.URL+"/api/text-summaries", body)
		if code != http.StatusAccepted {
			t.Fatalf("summary admission answered %d", code)
		}
	}
	session.await(ctx, "the durable optional-connection notice", func() bool {
		code, body := optionalPreviewRequest(t, ctx, client, http.MethodGet, started.Preview.URL+"/api/text-summaries/"+id, nil)
		var result struct {
			Status, Summary, Notice string
			TotalTokens             int64 `json:"totalTokens"`
		}
		if code != http.StatusOK || json.Unmarshal(body, &result) != nil {
			t.Fatalf("summary snapshot answered %d", code)
		}
		if result.Status == "pending" || result.Status == "generating" {
			return false
		}
		if result.Status != "defect" || result.Summary != "" || result.TotalTokens != 0 || !strings.Contains(result.Notice, "Connect an LLM to summarize") || !strings.Contains(result.Notice, "summary-model") {
			t.Fatalf("missing optional feature returned %+v", result)
		}
		return true
	})
	session.stopPreview(ctx)
	t.Logf("FlowID=%s commit=%s: Preview READY without a connection; Preview/Live READY; repeated summary %s records the safe notice with zero tokens", session.id, head, id)
}

// Optionality never turns a selected revoked credential into a usable feature.
func TestOptionalSelectedConnectionStillRequiresAuthorizationDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), previewSetupTimeout+previewCommandTimeout)
	defer cancel()
	session, _ := newAppliedPreviewSession(t, ctx)
	session.start(ctx, true, session.headRevision(ctx))
	dao := connectiondao.New(postgrestest.OpenForTest(t))
	row, err := dao.Connection(ctx, session.teamSpaceID, session.summaryConnection)
	if err != nil {
		t.Fatal(err)
	}
	if err = dao.MarkReauthorizationRequired(ctx, session.teamSpaceID, row.ConnectionID, row.CredentialRevision, json.RawMessage(`{"code":"test_reauthorization"}`)); err != nil {
		t.Fatal(err)
	}
	target := &pb.ProjectConfigurationTarget{ProjectId: session.identity.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_PREVIEW}
	snapshot, _, err := session.configuration.Get(ctx, session.account, target)
	if err != nil || snapshot.GetStatus() != pb.ConfigurationStatus_CONFIGURATION_STATUS_NEEDS_REVIEW || len(snapshot.GetRequirementGaps()) != 1 || snapshot.GetRequirementGaps()[0].GetKind() != pb.ConfigurationRequirementGap_KIND_AUTHORIZATION_REQUIRED {
		t.Fatalf("selected optional credential bypassed authorization: %s (%v)", snapshot.GetStatus(), err)
	}
	live := newLiveDelivery(session.mockSession)
	next, _, liveErr := live.configuration.Get(ctx, session.account, &pb.ProjectConfigurationTarget{ProjectId: session.identity.ProjectID, Scope: pb.ProjectScope_PROJECT_SCOPE_LIVE, NextPublish: true})
	if liveErr != nil || next.GetStatus() != pb.ConfigurationStatus_CONFIGURATION_STATUS_NEEDS_REVIEW {
		t.Fatalf("revoked selected optional connection bypassed Live: %s (%v)", next.GetStatus(), liveErr)
	}
	_, refused := session.start(ctx, true, session.headRevision(ctx))
	if failureCode(refused) != preview.FailureIncompatibleForTest {
		t.Fatalf("revoked optional credential started Preview: %s (%s)", refused.Preview.Status, failureCode(refused))
	}
}

func optionalPreviewClient(t *testing.T, publicURL string) *http.Client {
	t.Helper()
	target, err := url.Parse(publicURL)
	if err != nil || target.Port() == "" {
		t.Fatal("Preview URL has no local gateway port")
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", target.Port()))
	}}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func optionalPreviewRequest(t *testing.T, ctx context.Context, client *http.Client, method, target string, body []byte) (int, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Preview HTTP request failed (%T)", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		t.Fatal("Preview response unreadable or too large")
	}
	return response.StatusCode, data
}
