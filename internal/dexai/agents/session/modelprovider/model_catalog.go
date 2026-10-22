// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider

import (
	"errors"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"net"
	"slices"
	"strconv"
)

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
	effortReasoning    = protocolMode{"reasoning_effort"}
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
	chatThinking                                                             conversation.ChatThinkingMode
	messagesThinking                                                         protocolMode
	Replay                                                                   conversation.ReasoningReplayMode
	Thinking, Effort, StrictSchema, PromptCache, Fallback, MidSystem, Stream protocolMode
	SupportsImages, SupportsParallelTools                                    bool
}

// Candidate limits are conservative request budgets from the dated sources below.
// They are not live acceptance and cannot enable a model before adapter/BYOK delivery.
func candidate(model conversation.Model, context, output, compact, hard int64, replay conversation.ReasoningReplayMode, images bool, source, reason string) ModelProfile {
	style := model.Details().Provider.APIStyle()
	stream := streamChat
	if style == conversation.AnthropicMessages {
		stream = streamMessages
	}
	if style == conversation.OpenAIResponses {
		stream = streamResponses
	}
	thinking := thinkingDisabled
	if replay == conversation.ReplayEncryptedOpaque || replay == conversation.ReplayPlaintextRequired {
		thinking = thinkingRequired
	}
	if replay == conversation.ReplayPlaintextRequired {
		reason = replay.Reason() + " " + reason
	}
	chatThinking := conversation.ChatThinkingDefault
	switch model {
	case conversation.ModelKimiK26, conversation.ModelGLM52:
		chatThinking = conversation.ChatThinkingDisabled
	case conversation.ModelQwen38Max:
		chatThinking = conversation.ChatThinkingQwenDisabled
	}
	messagesThinking := protocolMode{}
	switch model {
	case conversation.ModelDeepSeekFlash, conversation.ModelMiniMaxM3:
		messagesThinking = thinkingDisabled
	case conversation.ModelMuseWonder10:
		messagesThinking = thinkingAdaptive
	}
	return ModelProfile{model: model, ContextWindow: context, maxTokens: output, compactionCeiling: compact, HardLimit: hard,
		SourceURL: source, CheckedOn: "2026-10-21", Availability: ModelAvailability{AvailabilityUnavailable, reason},
		Capabilities: ModelCapabilities{chatThinking: chatThinking, messagesThinking: messagesThinking, Replay: replay, Thinking: thinking, Effort: effortDisabled, StrictSchema: strictDisabled, PromptCache: cacheDisabled, Fallback: fallbackDisabled, MidSystem: midSystemReminder, Stream: stream, SupportsImages: images, SupportsParallelTools: false}}
}

const messagesBYOKPending = "Session API keys and live provider protocol verification are not available yet."

const adapterBYOKPending = "The provider adapter, protocol verification and Session API keys are not available yet."

func populateCandidateProfiles() {
	// Earlier models with documented non-thinking paths remain unavailable pending protocol verification.
	profiles[conversation.ModelKimiK26] = candidate(conversation.ModelKimiK26, 262_144, 16_000, 160_000, 210_000, conversation.ReplayNonThinking, true, "https://platform.kimi.ai/docs/api/chat", adapterBYOKPending)
	profiles[conversation.ModelGLM52] = candidate(conversation.ModelGLM52, 1_000_000, 64_000, 650_000, 800_000, conversation.ReplayNonThinking, false, "https://docs.bigmodel.cn/cn/guide/models/text/glm-5.2", adapterBYOKPending)
	profiles[conversation.ModelMiniMaxM3] = candidate(conversation.ModelMiniMaxM3, 1_000_000, 4_096, 650_000, 800_000, conversation.ReplayNonThinking, true, "https://platform.minimax.io/docs/api-reference/text-anthropic-api", messagesBYOKPending)
	// Checked 2026-10-22: https://developers.openai.com/api/docs/models/gpt-6-astra; flagship, 1,050,000 context / 128,000 output.
	profiles[conversation.ModelGPT6Astra] = candidate(conversation.ModelGPT6Astra, 1_050_000, 64_000, 700_000, 850_000, conversation.ReplayEncryptedOpaque, true, "https://developers.openai.com/api/docs/models/gpt-6-astra", adapterBYOKPending)
	// Official limits: 1M context / 384K output; Flash is the current multimodal flagship.
	profiles[conversation.ModelDeepSeekFlash] = candidate(conversation.ModelDeepSeekFlash, 1_000_000, 64_000, 650_000, 800_000, conversation.ReplayNonThinking, true, "https://api-docs.deepseek.com/quick_start/pricing/", messagesBYOKPending)
	// Checked 2026-10-22: https://docs.x.ai/developers/grok-4-7 (updated 2026-10-10); flagship, 500,000 context.
	profiles[conversation.ModelGrok47] = candidate(conversation.ModelGrok47, 500_000, 32_000, 300_000, 400_000, conversation.ReplayEncryptedOpaque, true, "https://docs.x.ai/developers/grok-4-7", adapterBYOKPending)
	// Official context: 1,048,576. Messages documents max_tokens=1024 for Spark; Wonder output verification is pending; larger output remains unverified.
	profiles[conversation.ModelMuseWonder10] = candidate(conversation.ModelMuseWonder10, 1_048_576, 1_024, 650_000, 800_000, conversation.ReplayEncryptedOpaque, true, "https://dev.meta.ai/docs/models", "Wonder encrypted replay and output limit live verification, and Session API keys are pending.")
	// Checked 2026-10-22: https://ai.google.dev/gemini-api/docs/models/gemini-4-argon (updated 2026-10-14); flagship, 1,048,576 context.
	profiles[conversation.ModelGemini4Argon] = candidate(conversation.ModelGemini4Argon, 1_048_576, 32_000, 650_000, 800_000, conversation.ReplayEncryptedOpaque, true, "https://ai.google.dev/gemini-api/docs/models/gemini-4-argon", adapterBYOKPending)
	for _, model := range CompatibleModels() {
		profile := profiles[model]
		profile.CheckedOn = "2026-10-22"
		profile.Capabilities.SupportsParallelTools = true
		profile.Availability.reason = "This environment does not offer this model provider."
		profiles[model] = profile
	}
	// Official limits: 1M context / up to 1,048,576 output. K3 cannot disable preserved thinking.
	profiles[conversation.ModelKimiK3] = candidate(conversation.ModelKimiK3, 1_048_576, 64_000, 650_000, 800_000, conversation.ReplayPlaintextRequired, true, "https://platform.kimi.ai/docs/api/chat", "Kimi K3 requires preserved thinking; no approved opaque replay or non-thinking path exists.")
	// Official limits: 1M context / 128K output. GLM 5.3 cannot disable thinking.
	profiles[conversation.ModelGLM53] = candidate(conversation.ModelGLM53, 1_000_000, 64_000, 650_000, 800_000, conversation.ReplayPlaintextRequired, false, "https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3", "GLM 5.3 requires thinking; no approved opaque replay or non-thinking path exists.")
	// Official limits: 1,000,000 context / 131,072 output. Region/workspace admission is not implemented.
	profiles[conversation.ModelQwen38Max] = candidate(conversation.ModelQwen38Max, 1_000_000, 64_000, 650_000, 800_000, conversation.ReplayNonThinking, true, "https://help.aliyun.com/en/model-studio/qwen3-8-max", "Chat adapter, non-thinking verification, regional endpoint admission and Session API keys are pending.")
	// Official context: 1,000,000. The official request example uses 4096 output tokens.
	profiles[conversation.ModelMiniMaxM31Flash] = candidate(conversation.ModelMiniMaxM31Flash, 1_000_000, 4_096, 650_000, 800_000, conversation.ReplayPlaintextRequired, true, "https://platform.minimax.io/docs/api-reference/text-anthropic-api", "MiniMax M3.1 Flash requires readable thinking replay and rejects disabled thinking; no compliant replay path exists.")
}

// ProviderConfig is operator admission for implemented compatible profiles.
// Hosted profiles default to no additional providers; local profiles opt in.
type ProviderConfig struct {
	MockListen      string                  `yaml:"mockListen"`
	Enabled         []conversation.Provider `yaml:"enabled"`
	CustomEndpoints bool                    `yaml:"customEndpoints"`
	MockEndpoint    bool                    `yaml:"mockEndpoint"`
}

// Validate rejects unsupported/duplicate providers and confines mock routing locally.
func (config ProviderConfig) Validate(local, mock bool) error {
	if config.MockListen != "" {
		host, port, err := net.SplitHostPort(config.MockListen)
		number, portErr := strconv.Atoi(port)
		if !local || !mock || err != nil || host != "127.0.0.1" || portErr != nil || number < 1 || number > 65535 {
			return errors.New("agent.providers.mockListen requires a local mock model and a 127.0.0.1 listener")
		}
	}
	seen := make(map[conversation.Provider]bool, len(config.Enabled))
	for _, provider := range config.Enabled {
		if _, supported := compatibleFlagships[provider]; !supported && provider != conversation.ProviderAnthropic && provider != conversation.ProviderQwen {
			return errors.New("agent.providers.enabled contains an unsupported provider")
		}
		if seen[provider] {
			return errors.New("agent.providers.enabled contains a duplicate provider")
		}
		seen[provider] = true
	}
	if config.CustomEndpoints && (!local || !mock || !config.MockEndpoint) {
		return errors.New("agent.providers.customEndpoints requires a local mock endpoint")
	}
	if config.MockEndpoint && (!local || !mock) {
		return errors.New("agent.providers.mockEndpoint requires a local lane with agent.mockModel")
	}
	return nil
}

// Availability combines reviewed implementation capability with this server's policy.
// The global catalog is immutable and never changed by a replica configuration.
func (config ProviderConfig) Availability(model conversation.Model) ModelAvailability {
	profile := ProfileOf(model)
	if profile.Availability.Selectable() {
		return profile.Availability
	}
	if compatibleFlagships[model.Details().Provider] == model || model.IsCustom() {
		if slices.Contains(config.Enabled, model.Details().Provider) {
			return ModelAvailability{state: AvailabilityAvailable}
		}
		return ModelAvailability{state: AvailabilityUnavailable, reason: "This environment does not offer this model provider."}
	}
	return profile.Availability
}

// compatibleFlagships owns the enabled-provider to reviewed flagship mapping.
var compatibleFlagships = map[conversation.Provider]conversation.Model{
	conversation.ProviderOpenAI: conversation.ModelGPT6Astra,
	conversation.ProviderXAI:    conversation.ModelGrok47,
	conversation.ProviderGemini: conversation.ModelGemini4Argon,
}

// CompatibleModels returns implemented provider flagships in stable catalog order.
func CompatibleModels() []conversation.Model {
	var result []conversation.Model
	for _, model := range conversation.Models() {
		if compatibleFlagships[model.Details().Provider] == model {
			result = append(result, model)
		}
	}
	return result
}

// defaultModel is the reviewed current flagship for one implemented provider.
func defaultModel(provider conversation.Provider) (conversation.Model, error) {
	if model, ok := compatibleFlagships[provider]; ok {
		return model, nil
	}
	return "", errors.New("model provider is not implemented")
}
