// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider

import "github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"

// ReasoningReplayMode is a closed policy for private, same-segment protocol replay.
type ReasoningReplayMode struct{ id, reason string }

var (
	ReplayClaudeSigned      = ReasoningReplayMode{id: "claude_signed"}
	ReplayEncryptedOpaque   = ReasoningReplayMode{id: "encrypted_opaque"}
	ReplayPlaintextRequired = ReasoningReplayMode{"plaintext_replay_required", "The model requires readable reasoning replay; platform policy forbids persisting it."}
	ReplayNonThinking       = ReasoningReplayMode{id: "non_thinking"}
)

func (mode ReasoningReplayMode) String() string { return mode.id }

// AvailabilityState separates a reviewed identity from a callable implementation.
type AvailabilityState struct{ id string }

var (
	AvailabilityCandidate   = AvailabilityState{"candidate"}
	AvailabilityAvailable   = AvailabilityState{"available"}
	AvailabilityUnavailable = AvailabilityState{"unavailable"}
)

func (state AvailabilityState) String() string { return state.id }

type ModelAvailability struct {
	state  AvailabilityState
	reason string
}

func (availability ModelAvailability) State() AvailabilityState { return availability.state }
func (availability ModelAvailability) Reason() string           { return availability.reason }
func (availability ModelAvailability) Selectable() bool {
	return availability.state == AvailabilityAvailable
}

// protocolMode values name concrete request policies; zero is never a capability guess.
type protocolMode struct{ id string }

var (
	thinkingDisabled   = protocolMode{"disabled"}
	thinkingFixed      = protocolMode{"fixed_budget"}
	thinkingAdaptive   = protocolMode{"adaptive"}
	thinkingRequired   = protocolMode{"required"}
	effortDisabled     = protocolMode{"effort_disabled"}
	effortOutputConfig = protocolMode{"output_config_effort"}
	strictDisabled     = protocolMode{"strict_disabled"}
	strictEnabled      = protocolMode{"strict_enabled"}
	cacheDisabled      = protocolMode{"cache_disabled"}
	cacheAnthropic     = protocolMode{"anthropic_breakpoints"}
	fallbackDisabled   = protocolMode{"fallback_disabled"}
	fallbackAnthropic  = protocolMode{"anthropic_default"}
	midSystemReminder  = protocolMode{"user_reminder"}
	midSystemNative    = protocolMode{"native_system"}
	streamMessages     = protocolMode{"messages_sse"}
	streamResponses    = protocolMode{"responses_sse"}
	streamChat         = protocolMode{"chat_sse"}
)

func (mode protocolMode) String() string { return mode.id }

// ModelCapabilities is the reviewed request/replay policy of a profile.
// Unavailable profiles describe a target only; admission never executes them.
type ModelCapabilities struct {
	Replay                                                                   ReasoningReplayMode
	Thinking, Effort, StrictSchema, PromptCache, Fallback, MidSystem, Stream protocolMode
	SupportsImages, SupportsParallelTools                                    bool
}

// Candidate limits are conservative request budgets from the dated sources below.
// They are not live acceptance and cannot enable a model before adapter/BYOK delivery.
func candidate(model conversation.Model, context, output, compact, hard int64, replay ReasoningReplayMode, images bool, source, reason string) ModelProfile {
	style := model.Details().Provider.APIStyle()
	stream := streamChat
	if style == conversation.AnthropicMessages {
		stream = streamMessages
	}
	if style == conversation.OpenAIResponses {
		stream = streamResponses
	}
	thinking := thinkingDisabled
	if replay == ReplayEncryptedOpaque || replay == ReplayPlaintextRequired {
		thinking = thinkingRequired
	}
	if replay == ReplayPlaintextRequired {
		reason = replay.reason + " " + reason
	}
	return ModelProfile{model: model, ContextWindow: context, maxTokens: output, compactionCeiling: compact, HardLimit: hard,
		SourceURL: source, CheckedOn: "2026-10-21", Availability: ModelAvailability{AvailabilityUnavailable, reason},
		Capabilities: ModelCapabilities{Replay: replay, Thinking: thinking, Effort: effortDisabled, StrictSchema: strictDisabled, PromptCache: cacheDisabled, Fallback: fallbackDisabled, MidSystem: midSystemReminder, Stream: stream, SupportsImages: images, SupportsParallelTools: false}}
}

const adapterBYOKPending = "The provider adapter, protocol verification and Session API keys are not available yet."

func populateCandidateProfiles() {
	// Earlier models with documented non-thinking paths remain unavailable pending protocol verification.
	profiles[conversation.ModelKimiK26] = candidate(conversation.ModelKimiK26, 262_144, 16_000, 160_000, 210_000, ReplayNonThinking, true, "https://platform.kimi.ai/docs/api/chat", adapterBYOKPending)
	profiles[conversation.ModelGLM52] = candidate(conversation.ModelGLM52, 1_000_000, 64_000, 650_000, 800_000, ReplayNonThinking, false, "https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2", adapterBYOKPending)
	profiles[conversation.ModelMiniMaxM3] = candidate(conversation.ModelMiniMaxM3, 1_000_000, 4_096, 650_000, 800_000, ReplayNonThinking, true, "https://platform.minimax.io/docs/api-reference/text-anthropic-api", adapterBYOKPending)
	// Official limits: 1,050,000 context / 128,000 output. Request budget stays below output maximum.
	profiles[conversation.ModelGPT6Astra] = candidate(conversation.ModelGPT6Astra, 1_050_000, 64_000, 700_000, 850_000, ReplayEncryptedOpaque, true, "https://developers.openai.com/api/docs/models/gpt-6-astra", adapterBYOKPending)
	// Official limits: 1M context / 384K output; Flash is the current multimodal flagship.
	profiles[conversation.ModelDeepSeekFlash] = candidate(conversation.ModelDeepSeekFlash, 1_000_000, 64_000, 650_000, 800_000, ReplayNonThinking, true, "https://api-docs.deepseek.com/quick_start/pricing/", adapterBYOKPending)
	// Official limits: 500,000 context; no separate text output limit.
	profiles[conversation.ModelGrok47] = candidate(conversation.ModelGrok47, 500_000, 32_000, 300_000, 400_000, ReplayEncryptedOpaque, true, "https://docs.x.ai/developers/grok-4-7", adapterBYOKPending)
	// Official context: 1,048,576. Messages documents max_tokens=1024 for Spark; Wonder output verification is pending; larger output remains unverified.
	profiles[conversation.ModelMuseWonder10] = candidate(conversation.ModelMuseWonder10, 1_048_576, 1_024, 650_000, 800_000, ReplayEncryptedOpaque, true, "https://dev.meta.ai/docs/models", "Messages adapter, Wonder encrypted replay and output limit verification, and Session API keys are pending.")
	// Official unified context: 1,048,576; synchronous output: 65,536 (long decode is not used).
	profiles[conversation.ModelGemini4Argon] = candidate(conversation.ModelGemini4Argon, 1_048_576, 32_000, 650_000, 800_000, ReplayEncryptedOpaque, true, "https://ai.google.dev/gemini-api/docs/models/gemini-4-argon", adapterBYOKPending)
	// Official limits: 1M context / up to 1,048,576 output. K3 cannot disable preserved thinking.
	profiles[conversation.ModelKimiK3] = candidate(conversation.ModelKimiK3, 1_048_576, 64_000, 650_000, 800_000, ReplayPlaintextRequired, true, "https://platform.kimi.ai/docs/api/chat", "Kimi K3 requires preserved thinking; no approved opaque replay or non-thinking path exists.")
	// Official limits: 1M context / 128K output. GLM 5.3 cannot disable thinking.
	profiles[conversation.ModelGLM53] = candidate(conversation.ModelGLM53, 1_000_000, 64_000, 650_000, 800_000, ReplayPlaintextRequired, false, "https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3", "GLM 5.3 requires thinking; no approved opaque replay or non-thinking path exists.")
	// Official limits: 1,000,000 context / 131,072 output. Region/workspace admission is not implemented.
	profiles[conversation.ModelQwen38Max] = candidate(conversation.ModelQwen38Max, 1_000_000, 64_000, 650_000, 800_000, ReplayNonThinking, true, "https://help.aliyun.com/en/model-studio/qwen3-8-max", "Chat adapter, non-thinking verification, regional endpoint admission and Session API keys are pending.")
	// Official context: 1,000,000. The official request example uses 4096 output tokens.
	profiles[conversation.ModelMiniMaxM31Flash] = candidate(conversation.ModelMiniMaxM31Flash, 1_000_000, 4_096, 650_000, 800_000, ReplayPlaintextRequired, true, "https://platform.minimax.io/docs/api-reference/text-anthropic-api", "MiniMax M3.1 Flash requires readable thinking replay and rejects disabled thinking; no compliant replay path exists.")
}
