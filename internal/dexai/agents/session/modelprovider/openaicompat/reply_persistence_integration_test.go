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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/modelendpoint"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
)

var compatibleProbeAssistant = dex.DefineStream[string]("CompatibleProbeAssistant", 1<<20)
var compatibleProbeReasoning = dex.DefineStream[string]("CompatibleProbeReasoning", 1<<20)

var compatibleProbeReply = dex.DefineAttribute[conversation.ModelReply]("CompatibleProbeReply")

// CompatibleReplyPersistenceFlow tests the real adapter/Dex serialization boundary.
// It proposes tools without executing them; it substitutes no application dependency.
type CompatibleReplyPersistenceFlow struct {
	dex.FlowDefaults
	client           *openaicompat.Client
	policy           openaicompat.Policy
	corruption       replayCorruption
	cancelStream     bool
	returnedFailures atomic.Int32
}

func (flow *CompatibleReplyPersistenceFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(CallCompatibleProbe{flow: flow}), dex.DefineStep(RejectCorruptReplay{flow: flow}), dex.DefineStep(FinishCompatibleProbe{})}
}
func (*CompatibleReplyPersistenceFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{compatibleProbeReply}, Streams: []dex.StreamDef{compatibleProbeAssistant, compatibleProbeReasoning}}
}
func (flow *CompatibleReplyPersistenceFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.ReadCompatibleReply, &dex.RPCOptions{Timeout: 10 * time.Second}), dex.DefineRPC(flow.CompleteCompatibleProbe, &dex.RPCOptions{Timeout: 10 * time.Second, IsTransactional: true})}
}

type CallCompatibleProbe struct {
	dex.StepDefaultsNoWaitFor[string]
	flow *CompatibleReplyPersistenceFlow
}

func (CallCompatibleProbe) GetStepOptions() *dex.StepOptions {
	options := dexflow.ProviderStepOptions(nil)
	options.ExecuteMethodTimeout = 10 * time.Second
	options.ExecuteRetry = &dex.RetryPolicy{InitialInterval: time.Second, MaximumInterval: time.Second, MaximumAttempts: 2, TotalDuration: 30 * time.Second}
	return options
}
func (step CallCompatibleProbe) Execute(ctx dex.Context, input string) (*dex.StepDecision, error) {
	entry := conversation.UserMessageEntry(input, nil)
	request := conversation.ModelRequest{Model: compatibleProbeModel(step.flow.policy), APIKey: mockmodel.APIKeyForTest, Prompts: agentloop.SessionPromptFor(conversation.ModelMock).Prompts,
		Messages: []conversation.RequestMessage{{Role: entry.Role, Blocks: entry.Blocks, CacheAnchor: -1}}}
	reasoningWrites := 0
	callContext, cancel := context.WithCancel(ctx)
	defer cancel()
	request.WriteAssistant = func(text string) error {
		err := compatibleProbeAssistant.Write(ctx, text)
		if step.flow.cancelStream {
			cancel()
		}
		return err
	}
	request.WriteReasoning = func(text string) error { reasoningWrites++; return compatibleProbeReasoning.Write(ctx, text) }
	reply, err := step.flow.client.Complete(callContext, request)
	if reasoningWrites != 0 {
		return dex.GracefulComplete(compatibleProbeRejection{ReasoningWrites: reasoningWrites}), nil
	}
	if step.flow.cancelStream {
		code := ""
		if errors.Is(err, context.Canceled) && len(reply.Blocks) == 0 && len(reply.ToolCalls) == 0 {
			code = "stream_cancelled"
		}
		return dex.GracefulComplete(compatibleProbeRejection{Code: code}), nil
	}
	if err != nil {
		step.flow.returnedFailures.Add(1)
		if rejected, ok := errors.AsType[*conversation.ModelRejectedError](err); ok {
			return dex.GracefulComplete(compatibleProbeRejection{Code: rejected.Code, Status: rejected.Status}), nil
		}
		return nil, dex.ErrorWithStack(err)
	}
	if err := compatibleProbeReply.Set(ctx, reply); err != nil {
		return nil, err
	}
	if step.flow.corruption != "" {
		return dex.GoToMany(dex.MovementOf(RejectCorruptReplay{flow: step.flow}, input)), nil
	}
	return dex.DeadEnd(), nil
}

type compatibleProbeRejection struct {
	ReasoningWrites int    `json:"reasoning_writes"`
	Code            string `json:"code"`
	Status          int    `json:"status"`
}

type compatibleReplySnapshot struct {
	StopReason   conversation.StopReason    `json:"stop_reason"`
	Present      bool                       `json:"present"`
	Public       *conversation.AgentMessage `json:"public,omitempty"`
	ReplayDigest string                     `json:"replay_digest"`
	ReplayBlocks int                        `json:"replay_blocks"`
	Bytes        int64                      `json:"bytes"`
	VisibleBytes int64                      `json:"visible_bytes"`
}

// ReadCompatibleReply deliberately returns no raw blocks, tool arguments or encrypted payload.
func (flow *CompatibleReplyPersistenceFlow) ReadCompatibleReply(ctx dex.Context, _ dex.None) (*dex.RPCResult[compatibleReplySnapshot], error) {
	reply, err := compatibleProbeReply.Get(ctx)
	if dexflow.IsAttributeNotFound(err) {
		return &dex.RPCResult[compatibleReplySnapshot]{}, nil
	}
	if err != nil {
		return nil, err
	}
	summaries := make([]conversation.ToolCallSummary, 0, len(reply.ToolCalls))
	for _, call := range reply.ToolCalls {
		summaries = append(summaries, conversation.ToolCallSummary{ID: call.ID, Name: call.Name})
	}
	entry := conversation.ModelReplyEntry(reply, summaries, time.Time{})
	public, _ := entry.PublicMessage()
	digest := sha256.New()
	count := 0
	for position, block := range reply.Blocks {
		if !conversation.IsOpaqueReplay(block) {
			continue
		}
		if _, err := conversation.ReadOpaqueReplay(block, flow.policy.Provider, flow.policy.WireMode, position); err != nil {
			return nil, err
		}
		_, _ = digest.Write(block)
		count++
	}
	return &dex.RPCResult[compatibleReplySnapshot]{Output: compatibleReplySnapshot{Present: true, StopReason: reply.StopReason, Public: &public, ReplayDigest: hex.EncodeToString(digest.Sum(nil)), ReplayBlocks: count, Bytes: entry.Bytes(), VisibleBytes: entry.WithoutOpaqueReplay().Bytes()}}, nil
}
func (*CompatibleReplyPersistenceFlow) CompleteCompatibleProbe(_ dex.Context, _ dex.None) (*dex.RPCResult[dex.None], error) {
	return &dex.RPCResult[dex.None]{NextSteps: []dex.StepMovement{dex.MovementOf(FinishCompatibleProbe{}, nil)}}, nil
}

type FinishCompatibleProbe struct {
	dex.StepDefaultsNoWaitFor[dex.None]
}

func (FinishCompatibleProbe) Execute(dex.Context, dex.None) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

// TestCompatibleReplyPersistenceDexIntegration verifies real Dex persistence and cold
// Worker replacement. Native Session/Sandbox/Git acceptance remains a separate gate.
func TestCompatibleReplyPersistenceDexIntegration(t *testing.T) {
	for _, policy := range []openaicompat.Policy{
		{Provider: conversation.ProviderOpenAI, WireMode: conversation.OpenAIResponses, MaxTokens: 64000, Replay: conversation.ReplayEncryptedOpaque},
		{Provider: conversation.ProviderXAI, WireMode: conversation.OpenAIResponses, MaxTokens: 32000, Replay: conversation.ReplayEncryptedOpaque},
		{Provider: conversation.ProviderGemini, WireMode: conversation.OpenAIChatCompletions, MaxTokens: 32000, Replay: conversation.ReplayEncryptedOpaque},
		{Provider: conversation.ProviderKimi, WireMode: conversation.OpenAIChatCompletions, MaxTokens: 16000, Replay: conversation.ReplayNonThinking, ChatThinking: conversation.ChatThinkingDisabled},
		{Provider: conversation.ProviderQwen, WireMode: conversation.OpenAIChatCompletions, MaxTokens: 64000, Replay: conversation.ReplayNonThinking, ChatThinking: conversation.ChatThinkingQwenDisabled},
	} {
		t.Run(policy.Provider.String(), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
			defer cancel()
			base, stop, err := mockmodel.StartForTest(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal("start existing model scenario engine:", err)
			}
			defer stop()
			adapter, err := newMockAdapter(t, ctx, base, policy, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal("create compatible model adapter:", err)
			}
			flow := &CompatibleReplyPersistenceFlow{client: adapter, policy: policy}
			sdk, replace := dexflowtest.WorkerClientWithReplacementForTest(ctx, t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST"), flow)
			id := "compatible-reply-probe-" + flowid.RandomID(16)
			defer func() {
				cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
				defer done()
				var none dex.None
				err := sdk.InvokeRPC(cleanup, id, flow.CompleteCompatibleProbe, nil, &none)
				var inactive *dex.FlowNotActiveOrNotFoundError
				if err != nil && !errors.As(err, &inactive) {
					if stopErr := sdk.StopFlow(cleanup, id, dex.StopOptions{Type: dex.TerminateFlow, Reason: "Compatible reply probe cleanup"}); stopErr != nil && !errors.As(stopErr, &inactive) {
						t.Errorf("terminate probe: %s", dexflowtest.ErrorCategory(stopErr))
					}
					t.Errorf("finish probe: %s", dexflowtest.ErrorCategory(err))
					return
				}
				result, err := sdk.WaitForFlow(cleanup, id, dex.WaitForFlowOptions{})
				if err != nil || result.Status != dex.FlowCompleted {
					t.Errorf("probe did not complete: %s", dexflowtest.ErrorCategory(err))
				}
			}()
			input := compatibleProbeInput(t)
			runID, err := sdk.StartFlow(ctx, flow, id, input, dex.StartFlowOptions{RequestID: &id, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}})
			if err != nil {
				t.Fatal("start probe:", dexflowtest.ErrorCategory(err))
			}
			t.Logf("FlowID=%s RunID=%s FlowType=%s", id, runID, dex.GetFinalFlowType(flow))
			if err := sdk.WaitForStepCompletion(ctx, id, dex.StepExecutionID{StepType: dex.GetFinalStepType(CallCompatibleProbe{})}, dex.WaitForStepCompletionOptions{RequestTimeout: time.Minute}); err != nil {
				t.Fatal("persist model reply:", dexflowtest.ErrorCategory(err))
			}
			read := func() compatibleReplySnapshot {
				t.Helper()
				var result compatibleReplySnapshot
				if err := sdk.InvokeRPC(ctx, id, flow.ReadCompatibleReply, nil, &result); err != nil {
					t.Fatal("read stored reply:", dexflowtest.ErrorCategory(err))
				}
				return result
			}
			before := read()
			if !before.Present || len(before.Public.ToolCalls) != 1 || strings.Contains(before.Public.Content, "mock-encrypted-") || strings.Contains(before.Public.Content, "I need the application manifest before I can answer.") {
				t.Fatal("public projection lost the proposal or exposed replay")
			}
			if policy.Replay == conversation.ReplayEncryptedOpaque && (before.ReplayBlocks != 1 || before.Bytes <= before.VisibleBytes) {
				t.Fatal("private replay is missing from the byte-accounted reply")
			}
			if policy.Replay != conversation.ReplayEncryptedOpaque && (before.ReplayBlocks != 0 || before.Bytes != before.VisibleBytes) {
				t.Fatal("non-thinking Chat retained replay")
			}
			sdk = replace()
			after := read()
			if !reflect.DeepEqual(before, after) {
				t.Fatal("cold Worker replacement changed the persisted reply projection or replay digest")
			}
			var none dex.None
			if err := sdk.InvokeRPC(ctx, id, flow.CompleteCompatibleProbe, nil, &none); err != nil {
				t.Fatal("close probe:", dexflowtest.ErrorCategory(err))
			}
			terminal, err := sdk.WaitForFlow(ctx, id, dex.WaitForFlowOptions{})
			if err != nil || terminal.Status != dex.FlowCompleted {
				t.Fatal("probe completion:", dexflowtest.ErrorCategory(err))
			}
			if !reflect.DeepEqual(after, read()) {
				t.Fatal("terminal read changed the retained model reply")
			}
			var stream dex.StreamMessagesPage[string]
			if err := sdk.ListStreamMessages(ctx, id, compatibleProbeReasoning, 1, "", &stream); err != nil || len(stream.Messages) != 0 {
				t.Fatal("reasoning Stream is unreadable or contains a private frame")
			}
			t.Logf("FlowID=%s provider=%s mode=%s: completed proposal and private replay survived cold Worker replacement; public projection contains no replay", id, policy.Provider.String(), policy.WireMode.String())
		})
	}
}

func compatibleProbeInput(t *testing.T) string {
	t.Helper()
	for _, scenario := range mockmodel.Catalog() {
		if scenario.Name == "compaction" {
			return scenario.Inputs[0].Content
		}
	}
	t.Fatal("the existing compaction scenario is missing")
	return ""
}

// TestCompatibleProtocolFailuresDexIntegration uses only the approved model
// substitute; the Worker, retry decisions, state and terminal reads are real Dex.
func TestCompatibleProtocolFailuresDexIntegration(t *testing.T) {
	for _, policy := range []openaicompat.Policy{
		{Provider: conversation.ProviderOpenAI, WireMode: conversation.OpenAIResponses, MaxTokens: 64000, Replay: conversation.ReplayEncryptedOpaque},
		{Provider: conversation.ProviderGemini, WireMode: conversation.OpenAIChatCompletions, MaxTokens: 32000, Replay: conversation.ReplayEncryptedOpaque},
	} {
		for _, test := range []struct {
			name     string
			fault    mockmodel.WireFaultForTest
			rejected bool
		}{
			{"plaintext", mockmodel.WireFaultPlaintextForTest, true},
			{"missing-replay", mockmodel.WireFaultMissingReplayForTest, true},
			{"oversized-replay", mockmodel.WireFaultOversizedReplayForTest, true},
			{"multiline-oversized", mockmodel.WireFaultMultilineOversizedForTest, true},
			{"credit-exhausted", mockmodel.WireFaultCreditExhaustedForTest, true},
			{"context-rejected", mockmodel.WireFaultContextRejectedForTest, true},
			{"stream-rejected", mockmodel.WireFaultStreamRejectedForTest, true},
			{"error-event", mockmodel.WireFaultChatRejectedForTest, true},
			{"authentication", mockmodel.WireFaultHTTP401ForTest, true},
			{"refusal-with-tools", mockmodel.WireFaultRefusalWithToolsForTest, true},
			{"truncated", mockmodel.WireFaultTruncatedForTest, false},
			{"rate-limit", mockmodel.WireFaultHTTP429ForTest, false},
			{"overload", mockmodel.WireFaultHTTP503ForTest, false},
		} {
			t.Run(policy.Provider.String()+"/"+test.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
				defer cancel()
				base, stop, err := mockmodel.StartForTest(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), test.fault)
				if err != nil {
					t.Fatal("start approved model fixture:", err)
				}
				defer stop()
				adapter, err := newMockAdapter(t, ctx, base, policy, slog.New(slog.NewTextHandler(io.Discard, nil)))
				if err != nil {
					t.Fatal(err)
				}
				flow := &CompatibleReplyPersistenceFlow{client: adapter, policy: policy}
				sdk := dexflowtest.WorkerClientForTest(ctx, t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST"), flow)
				id := "compatible-failure-probe-" + flowid.RandomID(16)
				defer func() {
					cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
					defer done()
					var inactive *dex.FlowNotActiveOrNotFoundError
					if err := sdk.StopFlow(cleanup, id, dex.StopOptions{Type: dex.TerminateFlow, Reason: "Compatible protocol failure probe cleanup"}); err != nil && !errors.As(err, &inactive) {
						t.Errorf("clean probe: %s", dexflowtest.ErrorCategory(err))
					}
				}()
				runID, err := sdk.StartFlow(ctx, flow, id, compatibleProbeInput(t), dex.StartFlowOptions{RequestID: &id, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}})
				if err != nil {
					t.Fatal("start failure probe:", dexflowtest.ErrorCategory(err))
				}
				t.Logf("FlowID=%s RunID=%s FlowType=%s", id, runID, dex.GetFinalFlowType(flow))
				result, err := sdk.WaitForFlow(ctx, id, dex.WaitForFlowOptions{NeedsResults: true})
				if err != nil {
					t.Fatal("wait for protocol classification:", dexflowtest.ErrorCategory(err))
				}
				if test.rejected {
					if result.Status != dex.FlowCompleted {
						t.Fatal("deterministic model refusal entered engine failure instead of typed rejection")
					}
					var rejection compatibleProbeRejection
					if err := result.DecodeSingleOutput(&rejection); err != nil || rejection.Code == "" || rejection.ReasoningWrites != 0 {
						t.Fatal("missing typed rejection outcome")
					}
					if test.fault == mockmodel.WireFaultCreditExhaustedForTest && rejection.Code != conversation.ProviderCreditExhausted.Rejected(429).Code {
						t.Fatal("quota failure was not classified as exhausted credit")
					}
					if flow.returnedFailures.Load() != 1 {
						t.Fatal("deterministic rejection was retried")
					}
					if test.fault == mockmodel.WireFaultMultilineOversizedForTest && rejection.Code != "response_too_large" {
						t.Fatal("multiline event bypassed the aggregate event budget")
					}
				} else if result.Status != dex.FlowFailed {
					t.Fatal("retryable model failure did not exhaust the real Dex retry policy")
				}
				if !test.rejected && flow.returnedFailures.Load() != 2 {
					t.Fatalf("provider errors returned %d times instead of both Dex attempts", flow.returnedFailures.Load())
				}
				var snapshot compatibleReplySnapshot
				if err := sdk.InvokeRPC(ctx, id, flow.ReadCompatibleReply, nil, &snapshot); err != nil {
					t.Fatal("read terminal outcome:", dexflowtest.ErrorCategory(err))
				}
				if snapshot.Present || snapshot.Public != nil {
					t.Fatal("invalid/incomplete provider response committed a tool-bearing reply")
				}
				var reasoning dex.StreamMessagesPage[string]
				if err := sdk.ListStreamMessages(ctx, id, compatibleProbeReasoning, 1, "", &reasoning); err != nil || len(reasoning.Messages) != 0 {
					t.Fatal("invalid response exposed a private reasoning frame")
				}
			})
		}
	}
}

// Protocol fixtures use the policy's actual catalog identity and an explicit
// non-secret mock key; production provider binding and admission stay enforced.
func compatibleProbeModel(policy openaicompat.Policy) conversation.Model {
	switch policy.Provider {
	case conversation.ProviderOpenAI:
		return conversation.ModelGPT6Astra
	case conversation.ProviderXAI:
		return conversation.ModelGrok47
	case conversation.ProviderGemini:
		return conversation.ModelGemini4Argon
	case conversation.ProviderKimi:
		return conversation.ModelKimiK26
	case conversation.ProviderQwen:
		return conversation.ModelQwen38Max
	default:
		panic("unsupported protocol fixture provider")
	}
}

// newMockAdapter uses the same validated, authority-bound transport as the runtime.
func newMockAdapter(t testing.TB, ctx context.Context, base string, policy openaicompat.Policy, logger *slog.Logger) (*openaicompat.Client, error) {
	t.Helper()
	config := modelendpoint.Config{AllowLocalLoopback: true}
	if err := config.Validate(true); err != nil {
		return nil, err
	}
	pool := modelendpoint.NewPool(config)
	t.Cleanup(func() { pool.Close() })
	client, err := pool.Client(ctx, base)
	if err != nil {
		return nil, err
	}
	return openaicompat.New(client, strings.TrimRight(base, "/")+"/v1", policy, true, logger)
}
