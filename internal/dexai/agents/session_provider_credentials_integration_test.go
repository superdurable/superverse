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
	"testing"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/projects/projecttest"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newSelectableSession provisions a real Project and admits its provider key
// through the authenticated resource API before selecting a reviewed profile.
// The local profile explicitly routes these identities to the existing mock API.
func newSelectableSession(t *testing.T, ctx context.Context, model conversation.Model, content string) *mockSession {
	t.Helper()
	if model.IsMock() {
		return newSession(t, ctx, model, content)
	}
	session := newSession(t, ctx, "", "")
	connection, caller := projecttest.LoginForTest(ctx, t, session.account)
	api := pb.NewAgentServiceClient(connection)
	credentials := pb.NewModelCredentialServiceClient(connection)
	choices, err := api.ListAgentModels(ctx, &pb.ListAgentModelsRequest{Caller: caller})
	if err != nil {
		t.Fatalf("read configured model catalog (%s)", status.Code(err))
	}
	var selected *pb.AgentModelChoice
	for _, choice := range choices.GetModels() {
		switch choice.GetModel() {
		case pb.AgentModel_AGENT_MODEL_DEEPSEEK_FLASH, pb.AgentModel_AGENT_MODEL_MUSE_WONDER_1_0, pb.AgentModel_AGENT_MODEL_QWEN_3_8_MAX, pb.AgentModel_AGENT_MODEL_KIMI_K3, pb.AgentModel_AGENT_MODEL_GLM_5_3:
			t.Fatal("the catalog exposed an unusable provider")
		}
		if choice.GetLabel() == model.Details().Label {
			selected = choice
		}
	}
	if selected == nil {
		t.Fatal("the local profile did not offer its implemented provider")
	}
	before := session.awaitStarted(ctx)
	revision := before.ModelRevision
	_, err = api.UpdateSessionModel(ctx, &pb.UpdateSessionModelRequest{ProjectId: session.identity.ProjectID, Model: selected.Model, ExpectedRevision: &revision, Caller: caller})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatal("a provider switch without its key was admitted")
	}
	zero := uint64(0)
	saved, err := credentials.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: session.identity.ProjectID, Provider: selected.Provider, RequestId: flowid.RandomID(20), ExpectedRevision: &zero, ApiKey: mockmodel.APIKey, Caller: caller})
	if err != nil || saved.GetCredential().GetRevision() != 1 {
		t.Fatalf("admit provider Project key through mock endpoint (%s)", status.Code(err))
	}
	if _, err = api.UpdateSessionModel(ctx, &pb.UpdateSessionModelRequest{ProjectId: session.identity.ProjectID, Model: selected.Model, ExpectedRevision: &revision, Caller: caller}); err != nil {
		t.Fatalf("select keyed provider (%s)", status.Code(err))
	}
	envelope, err := session.client.ReadModelCredential(ctx, session.id, modelcredential.ForModel(model))
	if err != nil || envelope == nil || envelope.Key != mockmodel.APIKey || envelope.Revision != 1 {
		t.Fatal("provider key was not held in the exact Session namespace")
	}
	if content != "" {
		session.send(ctx, content)
	}
	return session
}
