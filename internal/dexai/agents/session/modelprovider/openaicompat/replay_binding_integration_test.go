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
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
)

type replayCorruption string

const (
	replayWrongProvider replayCorruption = "provider"
	replayWrongMode     replayCorruption = "wire-mode"
	replayWrongPosition replayCorruption = "position"
	replayUnknownField  replayCorruption = "unknown-field"
	replayDuplicate     replayCorruption = "duplicate"
	replayWrongRole     replayCorruption = "role"
	replayTotalBudget   replayCorruption = "aggregate-budget"
)

type RejectCorruptReplay struct {
	dex.StepDefaultsNoWaitFor[string]
	flow *CompatibleReplyPersistenceFlow
}

func (step RejectCorruptReplay) Execute(ctx dex.Context, input string) (*dex.StepDecision, error) {
	reply, err := compatibleProbeReply.Get(ctx)
	if err != nil {
		return nil, err
	}
	for position, raw := range reply.Blocks {
		if !conversation.IsOpaqueReplay(raw) {
			continue
		}
		var block map[string]any
		if err := json.Unmarshal(raw, &block); err != nil {
			return nil, err
		}
		switch step.flow.corruption {
		case replayWrongProvider:
			block["provider"] = conversation.ProviderMeta.String()
		case replayWrongMode:
			block["wireMode"] = conversation.AnthropicMessages.String()
		case replayWrongPosition:
			block["position"] = position + 1
		case replayUnknownField:
			block["plaintext"] = "Synthetic forbidden reasoning"
		case replayDuplicate, replayTotalBudget:
			if step.flow.corruption == replayTotalBudget {
				block["ciphertext"] = strings.Repeat("x", 130<<10)
				reply.Blocks[position], err = json.Marshal(block)
				if err != nil {
					return nil, err
				}
				if step.flow.policy.WireMode == conversation.OpenAIResponses {
					block["itemID"] = "different-item"
				} else {
					block["callID"] = "different-call"
				}
			}
			block["position"] = len(reply.Blocks)
			duplicate, err := json.Marshal(block)
			if err != nil {
				return nil, err
			}
			reply.Blocks = append(reply.Blocks, duplicate)
			if step.flow.policy.WireMode == conversation.OpenAIChatCompletions {
				tool := reply.Blocks[position+1]
				if step.flow.corruption == replayTotalBudget {
					var call map[string]any
					if err := json.Unmarshal(tool, &call); err != nil {
						return nil, err
					}
					call["id"] = "different-call"
					tool, err = json.Marshal(call)
					if err != nil {
						return nil, err
					}
				}
				reply.Blocks = append(reply.Blocks, tool)
			}
		}
		if step.flow.corruption != replayDuplicate && step.flow.corruption != replayTotalBudget {
			reply.Blocks[position], err = json.Marshal(block)
			if err != nil {
				return nil, err
			}
		}
		break
	}
	entry := conversation.UserMessageEntry(input, nil)
	role := conversation.EntryRoleAssistant
	if step.flow.corruption == replayWrongRole {
		role = conversation.EntryRoleUser
	}
	request := conversation.ModelRequest{Model: conversation.ModelMock, Prompts: agentloop.SessionPromptFor(conversation.ModelMock).Prompts,
		Messages: []conversation.RequestMessage{{Role: entry.Role, Blocks: entry.Blocks, CacheAnchor: -1}, {Role: role, Blocks: reply.Blocks, CacheAnchor: -1}}}
	_, err = step.flow.client.Complete(ctx, request)
	var rejected *conversation.ModelRejectedError
	if errors.As(err, &rejected) {
		return dex.GracefulComplete(compatibleProbeRejection{Code: rejected.Code}), nil
	}
	return dex.GracefulComplete(compatibleProbeRejection{}), nil
}

// The first Step calls the actual model endpoint and commits its reply in Dex;
// the next Step verifies corrupted persisted bindings fail at the adapter boundary.
func TestCompatibleReplayBindingsDexIntegration(t *testing.T) {
	for _, policy := range []openaicompat.Policy{
		{Provider: conversation.ProviderOpenAI, WireMode: conversation.OpenAIResponses, MaxTokens: 64000, Replay: conversation.ReplayEncryptedOpaque},
		{Provider: conversation.ProviderGemini, WireMode: conversation.OpenAIChatCompletions, MaxTokens: 32000, Replay: conversation.ReplayEncryptedOpaque},
	} {
		for _, corruption := range []replayCorruption{replayWrongProvider, replayWrongMode, replayWrongPosition, replayUnknownField, replayDuplicate, replayWrongRole, replayTotalBudget} {
			t.Run(policy.Provider.String()+"/"+string(corruption), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				base, stop, err := mockmodel.Start(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
				if err != nil {
					t.Fatal("start approved model:", err)
				}
				defer stop()
				transport := modelprovider.NewProviderHTTPClient()
				defer transport.CloseIdleConnections()
				adapter, err := openaicompat.NewMock(transport, base, policy, slog.New(slog.NewTextHandler(io.Discard, nil)))
				if err != nil {
					t.Fatal("create adapter:", err)
				}
				flow := &CompatibleReplyPersistenceFlow{client: adapter, policy: policy, corruption: corruption}
				sdk := dexflowtest.WorkerClientForTest(ctx, t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST"), flow)
				id := "compatible-replay-binding-" + flowid.RandomID(16)
				defer func() {
					cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
					defer done()
					var inactive *dex.FlowNotActiveOrNotFoundError
					if err := sdk.StopFlow(cleanup, id, dex.StopOptions{Type: dex.TerminateFlow, Reason: "Compatible replay binding probe cleanup"}); err != nil && !errors.As(err, &inactive) {
						t.Errorf("clean binding probe: %s", dexflowtest.ErrorCategory(err))
					}
				}()
				if _, err := sdk.StartFlow(ctx, flow, id, compatibleProbeInput(t), dex.StartFlowOptions{RequestID: &id, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}}); err != nil {
					t.Fatal("start binding probe:", dexflowtest.ErrorCategory(err))
				}
				result, err := sdk.WaitForFlow(ctx, id, dex.WaitForFlowOptions{NeedsResults: true})
				if err != nil || result.Status != dex.FlowCompleted {
					t.Fatal("binding probe did not complete:", dexflowtest.ErrorCategory(err))
				}
				var rejection compatibleProbeRejection
				if err := result.DecodeSingleOutput(&rejection); err != nil || rejection.Code != "invalid_replay" {
					t.Fatalf("corrupt persisted replay was not rejected locally: code=%s", rejection.Code)
				}
			})
		}
	}
}
