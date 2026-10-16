// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres/postgrestest"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/preview"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/sessionworkspace"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/projects/projectdao"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The model admission of UpdateSessionModel and ListMockScenarios, decided by
// AgentService servers that this test assembles with the settings of each
// environment over the actual accounts of the lane's PostgreSQL. As in
// TestAdministratorProjectAccessDexIntegration, the decisions use the actual
// administrator account and the owner of a throwaway READY Project, an ordinary
// account, without a login. In the hosted settings (test and staging) only the
// administrator may select the mock model; in Local any account may; prod and a
// server without a Claude key offer what they have and refuse the rest. The
// owner then switches the Project's Session between the mock model and a
// Claude model through Local admission and the actual UpdateSessionModel; no
// turn runs, so no model is called and no segment opens.
func TestSessionModelAdmissionDexIntegration(t *testing.T) {
	address := dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS")
	target := dexflowtest.Setting(t, "SUPERVERSE_TEST_EXISTING_WORKER_TARGET")
	projectFlow, previewFlow, flow := &projects.ProjectFlow{}, &preview.PreviewFlow{}, &sessionagent.SessionAgentFlow{}
	workspaceFlow := &sessionworkspace.SessionWorkspaceFlow{}
	sdk := dexflowtest.ClientForTest(t, address, &dex.WorkerTarget{Address: target}, projectFlow, previewFlow, flow, workspaceFlow)
	reader := dexflowtest.ExecutionReaderForTest(t, address)
	// This client registry supplies definitions only. Real handlers and every
	// application dependency belong to the existing Worker selected above.
	db := postgrestest.OpenForTest(t)
	accountDAO := accountdao.New(db, "admin@superverse.test")
	client := sessionagent.NewSessionAgentFlowClientForTest(sdk, flow, reader, "")
	service := &agents.SessionFlowService{
		Projects: &projects.ProjectFlowClient{Dex: sdk, Flow: projectFlow, Projections: projectdao.New(db), ProjectIDLength: projecttest.IDLengthForTest(t), PlatformAdminEmail: "admin@superverse.test"},
		Previews: &preview.PreviewFlowClient{Dex: sdk, Flow: previewFlow, Execution: reader}, Agent: client,
		Workspaces: sessionworkspace.NewSessionWorkspaceFlowClientForTest(sdk, workspaceFlow, reader),
		Logger:     slog.New(slog.DiscardHandler),
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Minute)
	defer cancel()
	admin, err := accountDAO.AccountByEmail(ctx, "admin@superverse.test")
	if err != nil || admin.Status != accountdao.StatusActive {
		t.Fatalf("requires the actual active administrator account (%T)", err)
	}
	project := projecttest.CreateForTest(ctx, t, service.Projects, t.Name())
	owner := project.CreatorAccountID
	if owner == admin.AccountID {
		t.Fatal("the throwaway Project belongs to the administrator")
	}
	server := func(models SessionModels) *agentServer {
		return &agentServer{Boundary: &boundary.Boundary{Logger: slog.New(slog.DiscardHandler), Accounts: &accounts.Accounts{DAO: accountDAO}}, sessions: service, sessionAgents: client, models: models}
	}
	local := server(SessionModels{Claude: true, Mock: true, Local: true})
	hosted := server(SessionModels{Claude: true, Mock: true})
	keyless := server(SessionModels{Mock: true, Local: true})
	prod := server(SessionModels{Claude: true})

	for _, decision := range []struct {
		name    string
		server  *agentServer
		account string
		model   conversation.Model
		want    codes.Code
	}{
		{"hosted, the administrator selects mock", hosted, admin.AccountID, conversation.ModelMock, codes.OK},
		{"hosted, an ordinary account selects mock", hosted, owner, conversation.ModelMock, codes.PermissionDenied},
		{"hosted, an ordinary account selects Opus", hosted, owner, conversation.ModelOpus55, codes.OK},
		{"Local, an ordinary account selects mock", local, owner, conversation.ModelMock, codes.OK},
		{"Local, an ordinary account selects Sonnet", local, owner, conversation.ModelSonnet55, codes.OK},
		{"no Claude key, an ordinary account selects Opus", keyless, owner, conversation.ModelOpus55, codes.FailedPrecondition},
		{"no Claude key, an ordinary account selects mock", keyless, owner, conversation.ModelMock, codes.OK},
		{"prod, the administrator selects mock", prod, admin.AccountID, conversation.ModelMock, codes.FailedPrecondition},
	} {
		if got := admissionCode(t, admitModel(ctx, decision.server.Boundary, decision.server.models, decision.account, decision.model)); got != decision.want {
			t.Fatalf("%s: admission answered %s, want %s", decision.name, got, decision.want)
		}
	}
	for _, listing := range []struct {
		name    string
		server  *agentServer
		account string
		want    bool
	}{
		{"hosted, the administrator", hosted, admin.AccountID, true},
		{"hosted, an ordinary account", hosted, owner, false},
		{"Local, an ordinary account", local, owner, true},
		{"prod, the administrator", prod, admin.AccountID, false},
	} {
		selectable, readErr := mockSelectable(ctx, listing.server.Boundary, listing.server.models, listing.account)
		if readErr != nil || selectable != listing.want {
			t.Fatalf("%s: the mock model is selectable=%t (%v), want %t", listing.name, selectable, readErr, listing.want)
		}
	}

	// The owner switches the Project's Session: mock, its start model, to Opus
	// and back.
	id := agentloop.SessionFlowID(project.ProjectID)
	t.Logf("model admission probe FlowID=%s", id)
	description := func() agentloop.AgentDescription {
		t.Helper()
		for {
			snapshot, readErr := client.GetSnapshot(ctx, id)
			if readErr == nil && snapshot.Agent != nil && snapshot.Agent.Description.Status == agentloop.AgentStatusWaitingForMessage {
				return snapshot.Agent.Description
			}
			select {
			case <-ctx.Done():
				t.Fatalf("Session never waited for a message; reconcile %s (%T)", id, readErr)
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	if started := description(); started.Model != conversation.ModelMock || started.ModelRevision != 0 {
		t.Fatalf("a Session of a mock-default server starts on %q at revision %d", started.Model, started.ModelRevision)
	}
	for revision, model := range []conversation.Model{conversation.ModelOpus55, conversation.ModelMock} {
		if err = admitModel(ctx, local.Boundary, local.models, owner, model); err != nil {
			t.Fatalf("Local admission refused %s for the owner: %v", model, err)
		}
		if err = client.UpdateSessionModel(ctx, id, agentloop.UpdateSessionModelRequest{Model: model, ExpectedRevision: uint64(revision)}); err != nil {
			t.Fatalf("selecting %s at revision %d failed (%T)", model, revision, err)
		}
	}
	// No turn ran, so no segment opened on either model.
	if selected := description(); selected.Model != conversation.ModelMock || selected.ModelRevision != 2 || selected.ContextModel != "" {
		t.Fatalf("the Session selects %q at revision %d with context model %q", selected.Model, selected.ModelRevision, selected.ContextModel)
	}
	t.Logf("FlowID=%s switched mock, Opus, mock with Local admission; hosted admission admits only the administrator to mock", id)
}

// admissionCode is the public status code of an admission decision: OK, or the
// code of the refusal the API returns without dispatching the command.
func admissionCode(t *testing.T, err error) codes.Code {
	t.Helper()
	if err == nil {
		return codes.OK
	}
	refusal, isRefused := errors.AsType[*commandRefusalError](err)
	if !isRefused {
		t.Fatalf("the admission failed instead of deciding: %v", err)
	}
	return status.Code(refusal.public)
}
