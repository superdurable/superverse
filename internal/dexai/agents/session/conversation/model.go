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
)

// Model is a catalog model's durable identity. Published spellings never change.
type Model string

const (
	ModelOpus55   Model = "anthropic/claude-opus-5-5"
	ModelSonnet55 Model = "anthropic/claude-sonnet-5-5"
	ModelHaiku45  Model = "anthropic/claude-haiku-4-5"
	ModelMock     Model = "mock"
	// Internal fixture transports: never mapped to a public AgentModel.
	ModelMockResponses   Model = "mock/openai-responses"
	ModelMockChat        Model = "mock/openai-chat"
	ModelGPT6Astra       Model = "openai/gpt-6-astra"
	ModelDeepSeekFlash   Model = "deepseek/deepseek-flash"
	ModelGrok47          Model = "xai/grok-4.7"
	ModelMuseWonder10    Model = "meta/muse-wonder-1.0"
	ModelGemini4Argon    Model = "gemini/gemini-4-argon"
	ModelKimiK3          Model = "kimi/kimi-k3"
	ModelGLM53           Model = "glm/glm-5.3"
	ModelQwen38Max       Model = "qwen/qwen3.8-max"
	ModelKimiK26         Model = "kimi/kimi-k2.6"
	ModelGLM52           Model = "glm/glm-5.2"
	ModelMiniMaxM3       Model = "minimax/MiniMax-M3"
	ModelMiniMaxM31Flash Model = "minimax/MiniMax-M3.1-Flash"
)

// APIStyle is a closed wire protocol; a compatible provider never selects it at runtime.
type APIStyle struct{ id string }

var (
	AnthropicMessages     = APIStyle{"anthropic_messages"}
	OpenAIResponses       = APIStyle{"openai_responses"}
	OpenAIChatCompletions = APIStyle{"openai_chat_completions"}
)

func (style APIStyle) String() string { return style.id }

// Provider holds reviewed endpoint metadata, never credentials or a user-supplied URL.
// Mock is a test transport of the Anthropic profile, not a BYOK provider.
type Provider struct {
	id, displayName  string
	style            APIStyle
	baseURL, keyHint string
}

var (
	ProviderAnthropic = Provider{"anthropic", "Anthropic", AnthropicMessages, "https://api.anthropic.com", "Anthropic API key"}
	ProviderOpenAI    = Provider{"openai", "OpenAI", OpenAIResponses, "https://api.openai.com/v1", "OpenAI API key"}
	ProviderDeepSeek  = Provider{"deepseek", "DeepSeek", AnthropicMessages, "https://api.deepseek.com/anthropic", "DeepSeek API key"}
	ProviderXAI       = Provider{"xai", "xAI Grok", OpenAIResponses, "https://api.x.ai/v1", "xAI API key"}
	ProviderMeta      = Provider{"meta", "Meta Muse AI", AnthropicMessages, "https://api.meta.ai", "Meta Model API key"}
	ProviderGemini    = Provider{"gemini", "Google Gemini", OpenAIChatCompletions, "https://generativelanguage.googleapis.com/v1beta/openai", "Gemini API key"}
	ProviderKimi      = Provider{"kimi", "Kimi", OpenAIChatCompletions, "https://api.moonshot.ai/v1", "Kimi API key"}
	ProviderGLM       = Provider{"glm", "GLM", OpenAIChatCompletions, "https://open.bigmodel.cn/api/paas/v4", "GLM API key"}
	// This is a reviewed endpoint template, not a usable or user-editable URL.
	// Regional endpoint admission belongs to the later adapter/BYOK implementation.
	ProviderQwen    = Provider{"qwen", "Qwen", OpenAIChatCompletions, "https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "Model Studio API key for the endpoint region"}
	ProviderMiniMax = Provider{"minimax", "MiniMax", AnthropicMessages, "https://api.minimax.io/anthropic", "MiniMax API key"}
)

func (provider Provider) String() string      { return provider.id }
func (provider Provider) DisplayName() string { return provider.displayName }
func (provider Provider) APIStyle() APIStyle  { return provider.style }
func (provider Provider) BaseURL() string     { return provider.baseURL }
func (provider Provider) KeyHint() string     { return provider.keyHint }

// ModelDetails is a model's identity data; capability and availability belong to ModelProfile.
type ModelDetails struct {
	Provider          Provider
	APIModelID, Label string
}

var models = map[Model]ModelDetails{
	ModelOpus55:          {ProviderAnthropic, "claude-opus-5-5", "Claude Opus 5.5"},
	ModelSonnet55:        {ProviderAnthropic, "claude-sonnet-5-5", "Claude Sonnet 5.5"},
	ModelHaiku45:         {ProviderAnthropic, "claude-haiku-4-5", "Claude Haiku 4.5"},
	ModelMock:            {ProviderAnthropic, "mock", "Mock"},
	ModelMockResponses:   {ProviderOpenAI, "mock", "Mock Responses"},
	ModelMockChat:        {ProviderGemini, "mock", "Mock Chat"},
	ModelGPT6Astra:       {ProviderOpenAI, "gpt-6-astra", "GPT-6 Astra"},
	ModelDeepSeekFlash:   {ProviderDeepSeek, "deepseek-flash", "DeepSeek V4.1 Flash"},
	ModelGrok47:          {ProviderXAI, "grok-4.7", "Grok 4.7"},
	ModelMuseWonder10:    {ProviderMeta, "muse-wonder-1.0", "Muse Wonder 1.0"},
	ModelGemini4Argon:    {ProviderGemini, "gemini-4-argon", "Gemini 4 Argon"},
	ModelKimiK3:          {ProviderKimi, "kimi-k3", "Kimi K3"},
	ModelGLM53:           {ProviderGLM, "glm-5.3", "GLM 5.3"},
	ModelQwen38Max:       {ProviderQwen, "qwen3.8-max", "Qwen 3.8 Max"},
	ModelKimiK26:         {ProviderKimi, "kimi-k2.6", "Kimi K2.6"},
	ModelGLM52:           {ProviderGLM, "glm-5.2", "GLM 5.2"},
	ModelMiniMaxM3:       {ProviderMiniMax, "MiniMax-M3", "MiniMax M3"},
	ModelMiniMaxM31Flash: {ProviderMiniMax, "MiniMax-M3.1-Flash", "MiniMax M3.1 Flash"},
}

// Models returns a copy of the catalog identities, in stable picker order.
func Models() []Model {
	return []Model{ModelOpus55, ModelSonnet55, ModelHaiku45, ModelMock, ModelGPT6Astra, ModelDeepSeekFlash, ModelGrok47, ModelMuseWonder10, ModelGemini4Argon, ModelKimiK3, ModelGLM53, ModelQwen38Max, ModelMiniMaxM31Flash, ModelKimiK26, ModelGLM52, ModelMiniMaxM3}
}

// ExecutionModels includes internal fixture transports as well as public catalog identities.
// Only Models participates in public API mappings and picker construction.
func ExecutionModels() []Model { return append(Models(), ModelMockResponses, ModelMockChat) }

func (model Model) Details() ModelDetails {
	details, found := models[model]
	if !found {
		panic(fmt.Sprintf("no catalog identity for %q", model))
	}
	return details
}

// IsMock identifies the approved scenario engine independently of its wire mode.
func (model Model) IsMock() bool {
	return model == ModelMock || model == ModelMockResponses || model == ModelMockChat
}

// Validate accepts declared identities, including unavailable catalog entries.
// Admission separately checks availability before any model call.
func (model Model) Validate() error {
	if _, found := models[model]; !found {
		return fmt.Errorf("unsupported model %q", string(model))
	}
	return nil
}
func (model *Model) UnmarshalJSON(data []byte) error { return DecodeEnum(data, model, Model.Validate) }
func (model Model) ProviderModel() string            { return model.Details().APIModelID }

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
	// APIKey is transient request authentication and must never be serialized.
	APIKey  string `json:"-"`
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
	// WriteToolInput receives the input of each confirm_requirements call as
	// the model writes it, after every delta and once complete; nil skips it.
	WriteToolInput ToolInputWriter
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
// Hint and UserText are fixed platform wording, never provider response text.
// Detail is the API's bounded explanation for logs and diagnostics only.
type ModelRejectedError struct {
	Code     string
	Status   int
	Detail   string
	Hint     string
	UserText string
}

// ModelRejectionReason is a closed rejection with its model hint and user text.
type ModelRejectionReason struct{ code, hint, userText string }

var ProviderCreditExhausted = ModelRejectionReason{
	code:     "provider_credit_exhausted",
	hint:     "The model provider account has run out of credit. Wait until it is topped up; then ask the user to send the message again.",
	userText: "The model provider account has run out of credit. After it is topped up, send the message again.",
}

var ProviderSpendLimitReached = ModelRejectionReason{
	code:     "provider_spend_limit_reached",
	hint:     "The model provider account reached a spending limit. Wait until it resets or ask the account owner to raise it, then ask the user to send the message again.",
	userText: "The model provider account reached a spending limit. After it resets or the account owner raises it, send the message again.",
}

var ProviderBillingProblem = ModelRejectionReason{
	code:     "billing_error",
	hint:     "The model provider account has a billing problem. Wait for the account owner to fix it, then ask the user to send the message again.",
	userText: "The model provider account has a billing problem. After the account owner fixes it, send the message again.",
}

func (reason ModelRejectionReason) Rejected(status int) *ModelRejectedError {
	return &ModelRejectedError{Code: reason.code, Status: status, Hint: reason.hint, UserText: reason.userText}
}

// ServerRejection is a model call the server refuses before any model sees
// it, because it does not offer the Session's model, with the hint that tells
// the model and the user how to continue.
type ServerRejection struct{ code, hint string }

var (
	// ModelUnavailable refuses a catalog model whose execution boundary is unavailable.
	ModelUnavailable = ServerRejection{code: "model_unavailable",
		hint: "This model is unavailable. Select another model for the Session, then continue."}
	// ProviderRequestRejected describes deterministic provider request rejection.
	ProviderRequestRejected = ModelRejectionReason{code: "provider_rejected",
		hint: "The provider rejected the request. Correct the model configuration or request before continuing."}
	// MockModelUnavailable refuses the mock model where the server does not
	// start it.
	MockModelUnavailable = ServerRejection{code: "mock_model_unavailable",
		hint: "This environment does not offer the mock model. Select another model for the Session, then continue."}
	// NoProviderKey refuses a compatible model without this Project's key.
	NoProviderKey = ServerRejection{code: "provider_key_missing",
		hint: "Set the selected model provider's key for this Project, then continue."}
	// InvalidProviderKey refuses a key that cannot be sent as an HTTP credential.
	InvalidProviderKey = ServerRejection{code: "provider_key_invalid",
		hint: "Correct the selected model provider's key for this Project, then continue."}
	// ProviderProtocolRejected refuses unsupported compatible protocol responses or replay.
	ProviderProtocolRejected = ModelRejectionReason{code: "provider_protocol_rejected",
		hint:     "The model returned an unsupported protocol response. Select another model before continuing.",
		userText: "The model returned an unsupported protocol response. Select another model before continuing."}
	// NoClaudeKey refuses a Claude model without this Project’s active key.
	NoClaudeKey = ServerRejection{code: "no_claude_key",
		hint: "Set your Anthropic key for this Project or connect your local agent, then continue."}
)

// Rejected is the rejection as the error a model call returns.
func (rejection ServerRejection) Rejected() *ModelRejectedError {
	return &ModelRejectedError{Code: rejection.code, Hint: rejection.hint}
}

func (err *ModelRejectedError) Error() string {
	if err.Detail != "" {
		return fmt.Sprintf("model request rejected: %s (HTTP %d): %s", err.Code, err.Status, err.Detail)
	}
	return fmt.Sprintf("model request rejected: %s (HTTP %d)", err.Code, err.Status)
}

// ReasoningReplayMode is a closed policy for private, same-segment protocol replay.
type ReasoningReplayMode struct{ id, reason string }

var (
	ReplayClaudeSigned      = ReasoningReplayMode{id: "claude_signed"}
	ReplayEncryptedOpaque   = ReasoningReplayMode{id: "encrypted_opaque"}
	ReplayPlaintextRequired = ReasoningReplayMode{"plaintext_replay_required", "The model requires readable reasoning replay; platform policy forbids persisting it."}
	ReplayNonThinking       = ReasoningReplayMode{id: "non_thinking"}
)

func (mode ReasoningReplayMode) String() string { return mode.id }
func (mode ReasoningReplayMode) Reason() string { return mode.reason }

// ChatThinkingMode is the catalog-selected switch for a non-thinking Chat model.
type ChatThinkingMode struct{ id string }

var (
	ChatThinkingDefault      = ChatThinkingMode{}
	ChatThinkingDisabled     = ChatThinkingMode{"type_disabled"}
	ChatThinkingQwenDisabled = ChatThinkingMode{"qwen_disabled"}
)
