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
	"strings"
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

// Only the model endpoint is synthetic; gRPC, Dex, storage and Project lifecycle stay real.
func TestCredentialSaveProbeDexIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), mockTurnTimeout)
	defer cancel()
	base, probe, stop, err := mockmodel.StartCustomEndpointProbeForTest(ctx, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	session := newSession(t, ctx, conversation.ModelMock, "")
	session.awaitStarted(ctx)
	connection, caller := projecttest.LoginForTest(ctx, t, session.account)
	api := pb.NewModelCredentialServiceClient(connection)
	for _, provider := range []pb.ModelProvider{pb.ModelProvider_MODEL_PROVIDER_OPENAI, pb.ModelProvider_MODEL_PROVIDER_QWEN} {
		t.Run(provider.String(), func(t *testing.T) {
			revision := uint64(0)
			for _, fixture := range []string{"probe-401", "probe-403", "probe-503", "probe-timeout", "probe-429"} {
				path := fixture
				// Chat endpoints validate their model list before inference admission.
				if provider == pb.ModelProvider_MODEL_PROVIDER_QWEN && fixture != "probe-timeout" {
					path = strings.Replace(fixture, "probe-", "models-", 1)
					if fixture == "probe-503" {
						path = "models-501"
					}
				}
				request := &pb.SetModelCredentialRequest{ProjectId: session.identity.ProjectID, Provider: provider, RequestId: flowid.RandomID(20), ExpectedRevision: &revision, ApiKey: mockmodel.APIKeyForTest, BaseUrl: base + "/custom/" + path + "/v1", CustomModelId: mockmodel.CustomDiscoveredModelIDForTest, ContextWindow: 32768, MaxOutputTokens: 4096, Caller: caller}
				saved, err := api.SetModelCredential(ctx, request)
				if fixture == "probe-429" {
					if err != nil || saved.GetCredential().GetStatus() != pb.ModelCredentialStatus_MODEL_CREDENTIAL_STATUS_ACTIVE || saved.GetNotice() != pb.ModelCredentialSaveNotice_MODEL_CREDENTIAL_SAVE_NOTICE_RATE_LIMITED_UNVERIFIED || !strings.Contains(saved.GetNoticeMessage(), "saved but unverified") {
						t.Fatalf("429 admission: code=%s notice=%s", status.Code(err), saved.GetNotice())
					}
					replay, err := api.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: request.ProjectId, Provider: provider, RequestId: request.RequestId, ExpectedRevision: &revision, Caller: caller})
					if err != nil || replay.GetCredential().GetRevision() != saved.GetCredential().GetRevision() {
						t.Fatal("same operation did not reconcile without a key")
					}
					revision = saved.GetCredential().GetRevision()
					encoded, _ := json.Marshal(saved)
					if strings.Contains(string(encoded), mockmodel.APIKeyForTest) {
						t.Fatal("key escaped save response")
					}
					domain := modelcredential.ForModel(conversation.ModelOpenAICustom)
					if provider == pb.ModelProvider_MODEL_PROVIDER_QWEN {
						domain = modelcredential.ForModel(conversation.ModelQwenCustom)
					}
					stored, err := session.client.ReadModelCredential(ctx, session.id, domain)
					if err != nil || stored == nil || stored.Key != mockmodel.APIKeyForTest {
						t.Fatal("unverified key was not stored in its owning Attribute")
					}

				} else {
					if status.Code(err) != codes.FailedPrecondition {
						t.Fatalf("%s: code=%s", fixture, status.Code(err))
					}
					message := status.Convert(err).Message()
					if strings.Contains(message, mockmodel.APIKeyForTest) || strings.Contains(message, "Platform service") {
						t.Fatal("unsafe or generic probe error")
					}
					if (fixture == "probe-401" || fixture == "probe-403") && !strings.Contains(message, "provider rejected") {
						t.Fatal("key rejection missing")
					}
					if fixture == "probe-timeout" && !strings.Contains(message, "timed out") {
						t.Fatal("endpoint timeout missing")
					}
				}
			}
			for _, test := range []struct {
				suffix     string
				attempts   int
				unverified bool
			}{
				{"once", 2, false}, {"date", 2, false}, {"twice", 2, true}, {"long", 1, true},
			} {
				fixture := "probe-429-" + test.suffix
				if provider == pb.ModelProvider_MODEL_PROVIDER_QWEN {
					fixture = "models-429-" + test.suffix
				}
				saved, err := api.SetModelCredential(ctx, &pb.SetModelCredentialRequest{ProjectId: session.identity.ProjectID, Provider: provider, RequestId: flowid.RandomID(20), ExpectedRevision: &revision, ApiKey: mockmodel.APIKeyForTest, BaseUrl: base + "/custom/" + fixture + "/v1", CustomModelId: mockmodel.CustomDiscoveredModelIDForTest, Caller: caller})
				if err != nil {
					t.Fatalf("retry %s: %s", fixture, status.Code(err))
				}
				limited := saved.GetNotice() == pb.ModelCredentialSaveNotice_MODEL_CREDENTIAL_SAVE_NOTICE_RATE_LIMITED_UNVERIFIED
				if limited != test.unverified || probe.ProbeAttemptsForTest(fixture) != test.attempts {
					t.Fatalf("retry %s: unverified=%t attempts=%d", fixture, limited, probe.ProbeAttemptsForTest(fixture))
				}
				revision = saved.GetCredential().GetRevision()
			}
			for _, code := range []string{"401", "403"} {
				fixture := "probe-" + code
				if provider == pb.ModelProvider_MODEL_PROVIDER_QWEN {
					fixture = "models-" + code
				}
				if probe.ProbeAttemptsForTest(fixture) != 1 {
					t.Fatal("authentication rejection retried")
				}
			}

		})
	}
	t.Logf("FlowID=%s; 401/403 rejected, outage/timeout specific, 429 saved unverified and replay reconciled", session.id)
}
