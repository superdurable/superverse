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
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
)

// The same scenario engine emits optional status variants over actual SSE;
// only both terminal proofs permit a canonical completed reply to enter Dex.
func TestCompatibleResponsesItemStatusDexIntegration(t *testing.T) {
	for _, provider := range []conversation.Provider{conversation.ProviderOpenAI, conversation.ProviderXAI} {
		for _, test := range []struct {
			name     string
			fault    mockmodel.WireFault
			accepted bool
		}{
			{"refusal", mockmodel.WireFaultRefusal, true},
			{"refusal-at-output-limit", mockmodel.WireFaultOutputLimitRefusal, true},
			{"text-at-output-limit", mockmodel.WireFaultOutputLimitText, true},
			{"omitted", mockmodel.WireFaultOmittedItemStatus, true},
			{"null", mockmodel.WireFaultNullItemStatus, true},
			{"mixed-terminal-status", mockmodel.WireFaultMixedItemStatus, true},
			{"incomplete", mockmodel.WireFaultIncompleteItemStatus, false},
			{"in-progress", mockmodel.WireFaultInProgressItemStatus, false},
			{"unknown", mockmodel.WireFaultUnknownItemStatus, false},
			{"empty", mockmodel.WireFaultEmptyItemStatus, false},
			{"missing-item-done", mockmodel.WireFaultOmittedWithoutDone, false},
			{"incomplete-response", mockmodel.WireFaultOmittedIncompleteResponse, false},
		} {
			t.Run(provider.String()+"/"+test.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				base, stop, err := mockmodel.Start(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), test.fault)
				if err != nil {
					t.Fatal("start approved model:", err)
				}
				defer stop()
				transport := modelprovider.NewProviderHTTPClient()
				defer transport.CloseIdleConnections()
				policy := openaicompat.Policy{Provider: provider, WireMode: conversation.OpenAIResponses, MaxTokens: 64000, Replay: conversation.ReplayEncryptedOpaque}
				adapter, err := openaicompat.NewMock(transport, base, policy, slog.New(slog.NewTextHandler(io.Discard, nil)))
				if err != nil {
					t.Fatal("create Responses adapter:", err)
				}
				flow := &CompatibleReplyPersistenceFlow{client: adapter, policy: policy}
				sdk := dexflowtest.WorkerClientForTest(ctx, t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST"), flow)
				id := "compatible-item-status-" + flowid.RandomID(16)
				defer func() {
					cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
					defer done()
					var inactive *dex.FlowNotActiveOrNotFoundError
					if err := sdk.StopFlow(cleanup, id, dex.StopOptions{Type: dex.TerminateFlow, Reason: "Compatible status probe cleanup"}); err != nil && !errors.As(err, &inactive) {
						t.Errorf("clean status probe: %s", dexflowtest.ErrorCategory(err))
					}
				}()
				if _, err := sdk.StartFlow(ctx, flow, id, compatibleProbeInput(t), dex.StartFlowOptions{RequestID: &id, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}}); err != nil {
					t.Fatal("start status probe:", dexflowtest.ErrorCategory(err))
				}
				if test.accepted {
					if err := sdk.WaitForStepCompletion(ctx, id, dex.StepExecutionID{StepType: dex.GetFinalStepType(CallCompatibleProbe{})}, dex.WaitForStepCompletionOptions{RequestTimeout: 30 * time.Second}); err != nil {
						t.Fatal("persist completed status:", dexflowtest.ErrorCategory(err))
					}
					var snapshot compatibleReplySnapshot
					// This RPC strictly decodes the persisted opaque block and requires its
					// canonical completed status before returning the private digest/count.
					want := 1
					if test.fault == mockmodel.WireFaultRefusal || test.fault == mockmodel.WireFaultOutputLimitRefusal || test.fault == mockmodel.WireFaultOutputLimitText {
						want = 0
					}
					if err := sdk.InvokeRPC(ctx, id, flow.ReadCompatibleReply, nil, &snapshot); err != nil || !snapshot.Present || snapshot.ReplayBlocks != want || len(snapshot.Public.ToolCalls) != want {
						t.Fatal("terminal proofs did not persist canonical completed replay")
					}
					if (test.fault == mockmodel.WireFaultRefusal || test.fault == mockmodel.WireFaultOutputLimitRefusal) && snapshot.StopReason != conversation.StopReasonRefusal {
						t.Fatal("completed refusal did not remain definitive at the output limit")
					}
					if test.fault == mockmodel.WireFaultOutputLimitText && snapshot.StopReason != conversation.StopReasonMaxTokens {
						t.Fatal("ordinary completed text at the output limit did not continue")
					}
					var none dex.None
					if err := sdk.InvokeRPC(ctx, id, flow.CompleteCompatibleProbe, nil, &none); err != nil {
						t.Fatal("finish status probe:", dexflowtest.ErrorCategory(err))
					}
				}
				result, err := sdk.WaitForFlow(ctx, id, dex.WaitForFlowOptions{NeedsResults: !test.accepted})
				if err != nil || result.Status != dex.FlowCompleted {
					t.Fatal("status probe did not complete:", dexflowtest.ErrorCategory(err))
				}
				if !test.accepted {
					var rejection compatibleProbeRejection
					if err := result.DecodeSingleOutput(&rejection); err != nil || rejection.Code != "incomplete_response" {
						t.Fatal("explicit non-completion or missing proof was accepted")
					}
					var snapshot compatibleReplySnapshot
					if err := sdk.InvokeRPC(ctx, id, flow.ReadCompatibleReply, nil, &snapshot); err != nil || snapshot.Present {
						t.Fatal("incomplete item committed a tool-bearing reply")
					}
				}
				var reasoning dex.StreamMessagesPage[string]
				if err := sdk.ListStreamMessages(ctx, id, compatibleProbeReasoning, 1, "", &reasoning); err != nil || len(reasoning.Messages) != 0 {
					t.Fatal("item status handling exposed private reasoning")
				}
			})
		}
	}
}
