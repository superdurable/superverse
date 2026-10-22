// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bootstrap

import (
	"context"
	"testing"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	sessionagent "github.com/superdurable/superversev2/internal/dexai/agents/session"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The owning Native integration case invokes these actual Dex and gRPC APIs.
// New providers remain unavailable; private direct writes seed cleanup coverage.
func seedBYOKProviders(t *testing.T, ctx context.Context, sessions *sessionagent.SessionAgentFlowClient, api pb.ModelCredentialServiceClient, id agentloop.FlowID, project string, caller *pb.CallerContext, key string) {
	t.Helper()
	if _, err := sessions.ReserveModelCredential(ctx, id, modelcredential.Mutation{OperationID: "missing-provider"}); err == nil {
		t.Fatal("real Dex accepted an empty credential provider")
	}
	for _, provider := range modelcredential.Providers() {
		if provider == modelcredential.Anthropic {
			continue
		}
		wire := boundary.Enum[pb.ModelProvider](provider.String())
		read, err := api.GetModelCredentialInfo(ctx, &pb.GetModelCredentialInfoRequest{ProjectId: project, Provider: wire, Caller: caller})
		if err != nil || read.GetCredential().GetProvider() != wire || read.GetCredential().GetRevision() != 0 {
			t.Fatalf("read isolated %s metadata (%s)", provider.String(), status.Code(err))
		}
		zero := uint64(0)
		_, err = api.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: project, Provider: wire, ExpectedRevision: &zero, RequestId: "unavailable-provider", ApiKey: key, Caller: caller})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("unavailable %s save was admitted (%s)", provider.String(), status.Code(err))
		}
		// Reuse the operation ID across providers to prove SetAttributes identities do not collide.
		mutation := modelcredential.Mutation{Provider: provider, OperationID: "same-operation-different-provider"}
		admitted, err := sessions.ReserveModelCredential(ctx, id, mutation)
		if err != nil || !admitted.Fresh {
			t.Fatalf("reserve %s on real Dex (%T)", provider.String(), err)
		}
		if err = sessions.WriteModelCredential(ctx, id, mutation, &modelcredential.Envelope{Revision: admitted.Info.Revision, Key: key}); err != nil {
			t.Fatalf("write %s on real Dex (%T)", provider.String(), err)
		}
		if _, err = sessions.FinalizeModelCredential(ctx, id, mutation); err != nil {
			t.Fatalf("finalize %s on real Dex (%T)", provider.String(), err)
		}
		exact, err := sessions.ReadModelCredential(ctx, id, provider)
		if err != nil || exact == nil || exact.Key != key || exact.Revision != admitted.Info.Revision {
			t.Fatalf("isolated %s envelope not confirmed (%T)", provider.String(), err)
		}
		read, err = api.GetModelCredentialInfo(ctx, &pb.GetModelCredentialInfoRequest{ProjectId: project, Provider: wire, Caller: caller})
		if err != nil || read.GetCredential().GetProvider() != wire || read.GetCredential().GetStatus() != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_ACTIVE {
			t.Fatalf("isolated %s projection not confirmed (%s)", provider.String(), status.Code(err))
		}
	}
}

func assertBYOKProvidersAbsent(t *testing.T, ctx context.Context, sessions *sessionagent.SessionAgentFlowClient, id agentloop.FlowID) {
	t.Helper()
	for _, provider := range modelcredential.Providers() {
		exact, err := sessions.ReadModelCredential(ctx, id, provider)
		if err != nil || exact != nil {
			t.Fatalf("Project cleanup retained %s source credential (%T)", provider.String(), err)
		}
	}
}
