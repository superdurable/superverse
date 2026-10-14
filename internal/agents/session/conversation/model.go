// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Model is a model a Session can run on: a provider-qualified Claude model or
// the mock model.
type Model string

// The supported models: three Claude models and the mock model, which the
// in-process mock Claude API serves for tests (mockmodel) where the server
// configuration offers it. Any other value is rejected at startup and at the
// API boundary; there is no compatibility for other providers.
const (
	ModelOpus55   Model = "anthropic/claude-opus-5-5"
	ModelSonnet55 Model = "anthropic/claude-sonnet-5-5"
	ModelHaiku45  Model = "anthropic/claude-haiku-4-5"
	ModelMock     Model = "mock"
)

// Validate rejects a model the Agent does not support.
func (model Model) Validate() error {
	switch model {
	case ModelOpus55, ModelSonnet55, ModelHaiku45, ModelMock:
		return nil
	default:
		return fmt.Errorf("unsupported model %q", string(model))
	}
}

// UnmarshalJSON decodes and validates a model.
func (model *Model) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, model, Model.Validate)
}

// ProviderModel returns the API model name without its provider prefix; the
// mock model has none.
func (model Model) ProviderModel() string {
	if _, name, found := strings.Cut(string(model), "/"); found {
		return name
	}
	return string(model)
}

// Effort is how much effort the model puts into a reply.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortXHigh  Effort = "xhigh"
	EffortMax    Effort = "max"
)

// efforts lists the levels in ascending order.
var efforts = []Effort{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}

// Validate rejects an unknown effort.
func (effort Effort) Validate() error {
	if effort.rank() < 0 {
		return NewEnumValidationError("Effort", string(effort))
	}
	return nil
}

// UnmarshalJSON decodes and validates an effort; the empty value means none.
func (effort *Effort) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, effort, func(value Effort) error {
		if value == "" {
			return nil
		}
		return value.Validate()
	})
}

// AtMost clamps effort to ceiling.
func (effort Effort) AtMost(ceiling Effort) Effort {
	if effort.rank() > ceiling.rank() {
		return ceiling
	}
	return effort
}

func (effort Effort) rank() int {
	for index, level := range efforts {
		if level == effort {
			return index
		}
	}
	return -1
}

// ModelCallMetrics contains observed measurements of one model call, never
// reasoning text. An optional counter distinguishes an unavailable measurement
// from an observed zero.
type ModelCallMetrics struct {
	// Model is the requested model; ServedModel the one that completed the
	// call, which differs after a server-side fallback.
	Model       string `json:"model"`
	ServedModel string `json:"served_model,omitempty"`
	// Effort is the effort the call was sent with: the segment's base or its
	// latest per-message effort.
	Effort                   Effort `json:"effort,omitempty"`
	DurationMS               int64  `json:"duration_ms"`
	FirstEventMS             *int64 `json:"first_event_ms,omitempty"`
	InputTokens              *int64 `json:"input_tokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens,omitempty"`
	OutputTokens             *int64 `json:"output_tokens,omitempty"`
	ThinkingTokens           *int64 `json:"thinking_tokens,omitempty"`
	// DroppedThinkingBlocks counts the replayed thinking blocks the API
	// removed and the blocks the fallback cleanup removed before storing.
	DroppedThinkingBlocks int `json:"dropped_thinking_blocks,omitempty"`
	// InputTransformations records the position and reason of each change the
	// API made to the request, never content.
	InputTransformations []InputTransformation `json:"input_transformations,omitempty"`
}

// InputTransformation is one change the API reported to a request's input.
type InputTransformation struct {
	Type   string `json:"type"`
	Path   string `json:"path"`
	Reason string `json:"reason,omitempty"`
}

// PromptTokens is the size of the request the call read: input, cache read
// and cache write tokens. False when the API reported none of them.
func (metrics ModelCallMetrics) PromptTokens() (int64, bool) {
	var total int64
	known := false
	for _, count := range []*int64{metrics.InputTokens, metrics.CacheReadInputTokens, metrics.CacheCreationInputTokens} {
		if count != nil {
			total, known = total+*count, true
		}
	}
	return total, known
}

// StopReason is why the model stopped.
type StopReason string

const (
	StopReasonEndTurn                    StopReason = "end_turn"
	StopReasonToolUse                    StopReason = "tool_use"
	StopReasonMaxTokens                  StopReason = "max_tokens"
	StopReasonRefusal                    StopReason = "refusal"
	StopReasonModelContextWindowExceeded StopReason = "model_context_window_exceeded"
)

// SegmentPrompts are the frozen system prompt and tool definitions of one
// context segment; each tool is its exact API JSON.
type SegmentPrompts struct {
	System string            `json:"system"`
	Tools  []json.RawMessage `json:"tools"`
}

// ModelRequest is one stateless model call.
type ModelRequest struct {
	Model   Model
	Effort  Effort
	Prompts SegmentPrompts
	// Messages are the assembled segment; see AssembleRequest.
	Messages []RequestMessage
	// ToolChoiceNone forbids tool calls; only a compaction retry sets it.
	ToolChoiceNone bool
	// PrefixMismatch is the block-binding behavior for replayed thinking.
	PrefixMismatch PrefixMismatchBehavior
	CallID         CallID
	WriteAssistant TextWriter
	WriteReasoning TextWriter
	WriteActivity  ActivityWriter
	// ToolTarget names what a tool call works on, for its activity event.
	ToolTarget func(ToolCall) string
}

// PrefixMismatchBehavior is what the API does with a replayed thinking block
// that fails its conversation check.
type PrefixMismatchBehavior string

const (
	PrefixMismatchDropBlock PrefixMismatchBehavior = "drop_block"
	PrefixMismatchError     PrefixMismatchBehavior = "error"
)

// Validate accepts exactly drop_block or error.
func (behavior PrefixMismatchBehavior) Validate() error {
	switch behavior {
	case PrefixMismatchDropBlock, PrefixMismatchError:
		return nil
	default:
		return fmt.Errorf("thinking prefix mismatch behavior %q must be drop_block or error", string(behavior))
	}
}

// ModelReply is one complete model response. Blocks are the exact content
// blocks to store, after the fallback cleanup; ToolCalls are their complete
// tool_use blocks in order.
type ModelReply struct {
	StopReason StopReason
	Blocks     []json.RawMessage
	ToolCalls  []ToolCall
	// Text joins the text blocks; a compaction reads its summary from it.
	Text string
	// RefusalCategory is set when StopReason is refusal.
	RefusalCategory string
	Metrics         ModelCallMetrics
	// MockProgress is what the mock model reports with its reply; nil for a
	// Claude model.
	MockProgress *MockProgress
}

// MockProgress is where a Session on the mock model stands in its scenario
// after a reply: the scenario, the number of the input the reply answers and
// the number of the input the scenario takes next, 0 once it received all of
// them. The mock model reports it with each reply; it is UI state that no
// model ever reads.
type MockProgress struct {
	Scenario  string `json:"scenario"`
	Input     int    `json:"input"`
	NextInput int    `json:"next_input"`
}

// ModelClient is the Flow's model port; the Claude adapter implements it.
// A non-retryable rejection is a *ModelRejectedError; any other error is
// retryable and left to Dex.
type ModelClient interface {
	Complete(context.Context, ModelRequest) (ModelReply, error)
}

// ModelRejectedError is a request the API refused in a way a retry cannot
// change, such as an invalid request or a credential problem. Code, Status and
// Hint are what the Session records; Detail is the API's bounded explanation of
// which part of the request it rejected, for logs and tests only. Only the
// server's own rejections carry a Hint.
type ModelRejectedError struct {
	Code   string
	Status int
	Detail string
	Hint   string
}

// ServerRejection is a model call the server refuses before any model sees
// it, because it does not offer the Session's model, with the hint that tells
// the model and the user how to continue.
type ServerRejection struct{ code, hint string }

var (
	// MockModelUnavailable refuses the mock model where the server does not
	// start it.
	MockModelUnavailable = ServerRejection{code: "mock_model_unavailable",
		hint: "This environment does not offer the mock model. Select another model for the Session, then continue."}
	// NoClaudeKey refuses a Claude model where the server has no Claude key.
	NoClaudeKey = ServerRejection{code: "no_claude_key",
		hint: "This environment has no Claude key. Select the mock model, or ask the operator to configure the key, then continue."}
)

// Rejected is the rejection as the error a model call returns.
func (rejection ServerRejection) Rejected() *ModelRejectedError {
	return &ModelRejectedError{Code: rejection.code, Hint: rejection.hint}
}

func (err *ModelRejectedError) Error() string {
	if err.Detail == "" {
		return fmt.Sprintf("model request rejected: %s (HTTP %d)", err.Code, err.Status)
	}
	return fmt.Sprintf("model request rejected: %s (HTTP %d): %s", err.Code, err.Status, err.Detail)
}
