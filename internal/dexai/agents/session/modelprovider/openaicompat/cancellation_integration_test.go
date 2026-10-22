// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package openaicompat_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
)

// Cancellation occurs after the first actual assistant Stream write, while the
// real HTTP SSE reply still has a proposed tool. No partial tool may be committed.
func TestCompatibleStreamCancellationDexIntegration(t *testing.T) {
	for _, policy := range []openaicompat.Policy{
		{Provider: conversation.ProviderOpenAI, WireMode: conversation.OpenAIResponses, MaxTokens: 64000, Replay: conversation.ReplayEncryptedOpaque},
		{Provider: conversation.ProviderGemini, WireMode: conversation.OpenAIChatCompletions, MaxTokens: 32000, Replay: conversation.ReplayEncryptedOpaque},
	} {
		t.Run(policy.Provider.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			base, stop, err := mockmodel.StartForTest(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal("start approved model:", err)
			}
			defer stop()
			adapter, err := newMockAdapter(t, ctx, base, policy, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal("create adapter:", err)
			}
			flow := &CompatibleReplyPersistenceFlow{client: adapter, policy: policy, cancelStream: true}
			sdk := dexflowtest.WorkerClientForTest(ctx, t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST"), flow)
			id := "compatible-stream-cancel-" + flowid.RandomID(16)
			defer func() {
				cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer done()
				var inactive *dex.FlowNotActiveOrNotFoundError
				if err := sdk.StopFlow(cleanup, id, dex.StopOptions{Type: dex.TerminateFlow, Reason: "Compatible cancellation probe cleanup"}); err != nil && !errors.As(err, &inactive) {
					t.Errorf("clean cancellation probe: %s", dexflowtest.ErrorCategory(err))
				}
			}()
			if _, err := sdk.StartFlow(ctx, flow, id, compatibleProbeInput(t), dex.StartFlowOptions{RequestID: &id, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}}); err != nil {
				t.Fatal("start cancellation probe:", dexflowtest.ErrorCategory(err))
			}
			result, err := sdk.WaitForFlow(ctx, id, dex.WaitForFlowOptions{NeedsResults: true})
			if err != nil || result.Status != dex.FlowCompleted {
				t.Fatal("cancellation probe did not complete:", dexflowtest.ErrorCategory(err))
			}
			var rejection compatibleProbeRejection
			if err := result.DecodeSingleOutput(&rejection); err != nil || rejection.Code != "stream_cancelled" || rejection.ReasoningWrites != 0 {
				t.Fatal("partial cancelled reply was accepted or reasoning was streamed")
			}
			var snapshot compatibleReplySnapshot
			if err := sdk.InvokeRPC(ctx, id, flow.ReadCompatibleReply, nil, &snapshot); err != nil || snapshot.Present {
				t.Fatal("cancellation committed a tool-bearing reply")
			}
			var assistant, reasoning dex.StreamMessagesPage[string]
			if err := sdk.ListStreamMessages(ctx, id, compatibleProbeAssistant, 1, "", &assistant); err != nil || len(assistant.Messages) == 0 {
				t.Fatal("cancellation never reached an actual assistant Stream frame")
			}
			if err := sdk.ListStreamMessages(ctx, id, compatibleProbeReasoning, 1, "", &reasoning); err != nil || len(reasoning.Messages) != 0 {
				t.Fatal("cancelled stream exposed private reasoning")
			}
		})
	}
}
