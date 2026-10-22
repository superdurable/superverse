// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow/dexflowtest"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/mockmodel"
)

// MessagesWireFlow is a disposable test Flow: only the model is substituted.
// Requests and replies cross the actual Client, Worker and Dex persistence.
type MessagesWireFlow struct {
	dex.FlowDefaults
	client  *modelprovider.MessagesClientForTest
	legacy  conversation.ModelClient
	baseURL string
}

func (*MessagesWireFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

func (flow *MessagesWireFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStartStep(InvokeMessagesWire{flow: flow})}
}

type messagesWireInput struct {
	Model          conversation.Model
	Effort         conversation.Effort
	Prompts        conversation.SegmentPrompts
	Messages       []conversation.RequestMessage
	ToolChoiceNone bool
	PrefixMismatch conversation.PrefixMismatchBehavior
	CancelOnText   bool
}
type messagesWireOutput struct {
	Reply                       conversation.ModelReply
	Rejected                    *conversation.ModelRejectedError
	Failure                     string
	RetryAfter                  time.Duration
	Canceled, ReadableReasoning bool
}

type InvokeMessagesWire struct {
	dex.StepDefaultsNoWaitFor[messagesWireInput]
	flow *MessagesWireFlow
}

func (InvokeMessagesWire) GetStepOptions() *dex.StepOptions {
	return &dex.StepOptions{ExecuteMethodTimeout: time.Minute, ExecuteDurability: dex.StepDurabilitySync}
}
func (step InvokeMessagesWire) Execute(ctx dex.Context, input messagesWireInput) (*dex.StepDecision, error) {
	callContext, cancel := context.WithCancel(ctx)
	defer cancel()
	output := messagesWireOutput{}
	request := conversation.ModelRequest{Model: input.Model, Effort: input.Effort, Prompts: input.Prompts, Messages: input.Messages, ToolChoiceNone: input.ToolChoiceNone, PrefixMismatch: input.PrefixMismatch,
		CallID: conversation.CallID(ctx.StepExecutionID()),
		WriteAssistant: func(string) error {
			if input.CancelOnText {
				cancel()
				return context.Canceled
			}
			return nil
		},
		WriteReasoning: func(string) error { output.ReadableReasoning = true; return nil },
		WriteActivity:  func(conversation.AgentEvent) error { return nil }, ToolTarget: func(conversation.ToolCall) string { return "" }}
	var err error
	if input.Model == conversation.ModelMock {
		output.Reply, err = step.flow.legacy.Complete(callContext, request)
	} else {
		output.Reply, err = modelprovider.CompleteMessagesForTest(callContext, step.flow.client, request, mockmodel.APIKeyForTest, step.flow.baseURL)
	}
	if rejected, ok := errors.AsType[*conversation.ModelRejectedError](err); ok {
		output.Rejected = rejected
	} else if errors.Is(err, context.Canceled) {
		output.Canceled = true
	} else if err != nil {
		output.Failure = err.Error()
		if retry, ok := errors.AsType[*dex.RetryAfterError](err); ok {
			output.RetryAfter = retry.After
		}
	}
	return dex.GracefulComplete(output), nil
}

type messagesWireHarness struct {
	sdk  *dex.Client
	flow *MessagesWireFlow
}

func newMessagesWireHarness(t *testing.T, client *modelprovider.MessagesClientForTest, baseURL string, legacy conversation.ModelClient) *messagesWireHarness {
	t.Helper()
	flow := &MessagesWireFlow{client: client, baseURL: baseURL, legacy: legacy}
	sdk := dexflowtest.WorkerClientForTest(t.Context(), t, dexflowtest.Setting(t, "SUPERVERSE_TEST_DEX_ADDRESS"), dexflowtest.Setting(t, "SUPERVERSE_TEST_WORKER_HOST"), flow)
	return &messagesWireHarness{sdk: sdk, flow: flow}
}
func (h *messagesWireHarness) complete(t *testing.T, ctx context.Context, request conversation.ModelRequest, cancelOnText bool) (conversation.ModelReply, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	input := messagesWireInput{Model: request.Model, Effort: request.Effort, Prompts: request.Prompts, Messages: request.Messages, ToolChoiceNone: request.ToolChoiceNone, PrefixMismatch: request.PrefixMismatch, CancelOnText: cancelOnText}
	id := "messages-wire-" + flowid.RandomID(16)
	requestID := flowid.Fingerprint(input)
	options := dex.StartFlowOptions{RequestID: &requestID, IDReusePolicy: dex.IDReuseDisallow, AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}}
	if _, err := h.sdk.StartFlow(ctx, h.flow, id, input, options); err != nil {
		t.Fatalf("start adapter Flow: %v", err)
	}
	result, err := h.sdk.WaitForFlow(ctx, id, dex.WaitForFlowOptions{NeedsResults: true})
	if err != nil {
		t.Fatalf("await adapter Flow %s: %v", id, err)
	}
	if result.Status != dex.FlowCompleted {
		t.Fatalf("adapter Flow %s ended as %v", id, result.Status)
	}
	var output messagesWireOutput
	if err := result.DecodeSingleOutput(&output); err != nil {
		t.Fatal(err)
	}
	t.Logf("FlowID=%s model=%s complete adapter reply persisted", id, request.Model)
	if output.ReadableReasoning && request.Model != conversation.ModelMock {
		t.Fatal("compatible reasoning reached the presentation writer")
	}
	if output.Rejected != nil {
		return output.Reply, output.Rejected
	}
	if output.Canceled {
		return output.Reply, context.Canceled
	}
	if output.Failure != "" {
		if output.RetryAfter > 0 {
			return output.Reply, dex.RetryAfter(output.RetryAfter, errors.New(output.Failure))
		}
		return output.Reply, errors.New(output.Failure)
	}
	return output.Reply, nil
}

func mockInput(t *testing.T, scenario string, number int) mockmodel.Input {
	t.Helper()
	for _, script := range mockmodel.Catalog() {
		if script.Name == scenario && number > 0 && number <= len(script.Inputs) {
			return script.Inputs[number-1]
		}
	}
	t.Fatal("scenario input missing")
	return mockmodel.Input{}
}
