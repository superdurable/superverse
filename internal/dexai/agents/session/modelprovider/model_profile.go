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
	"fmt"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// ModelProfile is everything that differs between the supported models. A
// segment runs on one model for its whole life, so its profile never changes
// mid-segment.
type ModelProfile struct {
	model conversation.Model
	// Catalog capabilities are reviewed protocol policy, not proof of live compatibility.
	Capabilities  ModelCapabilities
	Availability  ModelAvailability
	ContextWindow int64
	SourceURL     string
	CheckedOn     string
	// maxTokens bounds one reply.
	maxTokens int64
	// adaptiveThinking sends adaptive thinking with summarized display and
	// block binding controls; otherwise thinking has a fixed thinkingBudget.
	adaptiveThinking bool
	thinkingBudget   int64
	// Effort sends output_config.effort and offers set_effort; MaxEffort is
	// the ceiling every effort is clamped to.
	Effort    bool
	MaxEffort conversation.Effort
	// systemMessages renders system entries as mid-conversation system
	// messages, which per-message effort needs.
	systemMessages bool
	// fallbacks sends fallbacks: "default".
	fallbacks bool
	// compactionCeiling caps the configured compaction threshold; zero
	// leaves it as configured.
	compactionCeiling int64
	// HardLimit is the context size at which a failed compaction opens an
	// unsummarized segment instead of retrying.
	HardLimit int64
}

// Haiku 4.5 takes neither output_config.effort nor mid-conversation system
// messages (Claude API documentation, models and prompt caching guides); its
// notices render as reminders in the user message and it has no set_effort.
// The live API test sends every Claude profile's request shape.
var profiles = map[conversation.Model]ModelProfile{
	conversation.ModelOpus55: {model: conversation.ModelOpus55, maxTokens: 64_000, adaptiveThinking: true,
		Effort: true, MaxEffort: conversation.EffortMax, systemMessages: true, fallbacks: true, HardLimit: 900_000},
	conversation.ModelSonnet55: {model: conversation.ModelSonnet55, maxTokens: 64_000, adaptiveThinking: true,
		Effort: true, MaxEffort: conversation.EffortMax, systemMessages: true, fallbacks: true, HardLimit: 900_000},
	conversation.ModelHaiku45: {model: conversation.ModelHaiku45, maxTokens: 64_000, thinkingBudget: 8_000,
		MaxEffort: conversation.EffortHigh, compactionCeiling: 120_000, HardLimit: 130_000},
	// The mock model takes the request shape of Sonnet 5.5, so a mock Session
	// assembles and parses exactly what a Claude Session does.
	conversation.ModelMock: {model: conversation.ModelMock, maxTokens: 64_000, adaptiveThinking: true,
		Effort: true, MaxEffort: conversation.EffortMax, systemMessages: true, fallbacks: true, HardLimit: 900_000},
}

func init() {
	populateCandidateProfiles()
	for _, model := range conversation.Models() {
		if !model.IsCustom() {
			continue
		}
		source := ""
		switch model.Details().Provider.APIStyle() {
		case conversation.AnthropicMessages:
			source = "https://platform.claude.com/docs/en/api/messages"
		case conversation.OpenAIResponses:
			source = "https://platform.openai.com/docs/api-reference/responses"
		case conversation.OpenAIChatCompletions:
			source = "https://platform.openai.com/docs/api-reference/chat"
		}
		profile := candidate(model, 32768, 4096, 22528, 24576, conversation.ReplayNonThinking, false, source, "This environment does not offer this custom endpoint provider.")
		profile.Capabilities.chatThinking = conversation.ChatThinkingDefault
		profiles[model] = profile
	}
	for _, model := range []conversation.Model{conversation.ModelMockResponses, conversation.ModelMockChat} {
		profile := profiles[conversation.ModelGPT6Astra]
		if model == conversation.ModelMockChat {
			profile = profiles[conversation.ModelGemini4Argon]
		}
		profile.model, profile.Availability = model, ModelAvailability{state: AvailabilityAvailable}
		profile.Capabilities.SupportsParallelTools = true
		if model == conversation.ModelMockResponses {
			profile.Effort, profile.MaxEffort, profile.systemMessages = true, conversation.EffortXHigh, true
			profile.Capabilities.StrictSchema = strictEnabled
			profile.Capabilities.Effort = effortReasoning
		}
		profiles[model] = profile
	}
	for _, model := range conversation.ExecutionModels() {
		profile, found := profiles[model]
		if !found {
			panic(fmt.Sprintf("no model profile for %q", model))
		}
		details := model.Details()
		if details.Provider.String() == "" || details.Provider.DisplayName() == "" || details.Provider.BaseURL() == "" || details.Provider.KeyHint() == "" || details.Provider.APIStyle().String() == "" || details.APIModelID == "" || details.Label == "" {
			panic(fmt.Sprintf("incomplete catalog identity for %q", model))
		}
		if profile.model != model {
			panic(fmt.Sprintf("profile identity mismatch for %q", model))
		}
		// Existing published Claude/mock request policies and thresholds are unchanged.
		if profile.Availability.state.id == "" {
			switch model {
			case conversation.ModelOpus55, conversation.ModelSonnet55, conversation.ModelHaiku45, conversation.ModelMock:
			default:
				panic(fmt.Sprintf("missing explicit availability for %q", model))
			}
			profile.Availability = ModelAvailability{state: AvailabilityAvailable}
			profile.ContextWindow = 1_000_000
			if model == conversation.ModelHaiku45 {
				profile.ContextWindow = 200_000
			}
			profile.SourceURL = "https://platform.claude.com/docs/en/about-claude/models/overview"
			profile.CheckedOn = "2026-10-21"
			profile.Capabilities = ModelCapabilities{Replay: conversation.ReplayClaudeSigned, Thinking: thinkingFixed, Effort: effortDisabled, StrictSchema: strictEnabled, PromptCache: cacheAnthropic, Fallback: fallbackDisabled, MidSystem: midSystemReminder, Stream: streamMessages, SupportsImages: true, SupportsParallelTools: true}
			if profile.Effort {
				profile.Capabilities.Effort = effortOutputConfig
			}
			if profile.adaptiveThinking {
				profile.Capabilities.Thinking = thinkingAdaptive
			}
			if profile.fallbacks {
				profile.Capabilities.Fallback = fallbackAnthropic
			}
			if profile.systemMessages {
				profile.Capabilities.MidSystem = midSystemNative
			}
		}
		if profile.ContextWindow <= 0 || profile.maxTokens <= 0 || profile.HardLimit <= profile.maxTokens || profile.HardLimit > profile.ContextWindow || profile.compactionCeiling >= profile.HardLimit || profile.SourceURL == "" || profile.CheckedOn == "" || profile.Capabilities.Replay.String() == "" {
			panic(fmt.Sprintf("incomplete model profile for %q", model))
		}
		if profile.Availability.state != AvailabilityAvailable && profile.Availability.reason == "" {
			panic(fmt.Sprintf("missing availability reason for %q", model))
		}
		profiles[model] = profile
	}
	if len(profiles) != len(conversation.ExecutionModels()) {
		panic("model profile catalog contains undeclared identities")
	}
}

// ProfileOf returns the profile of a supported model; the model was validated
// where it entered the system.
func ProfileOf(model conversation.Model) ModelProfile {
	profile, found := profiles[model]
	if !found {
		panic(fmt.Sprintf("no model profile for %q", model))
	}
	return profile
}

// BaseEffort is the effort a segment of this model is sent with: the
// configured one clamped to the ceiling, or none when the model takes no
// effort.
func (profile ModelProfile) BaseEffort(configured conversation.Effort) conversation.Effort {
	if !profile.Effort {
		return ""
	}
	return configured.AtMost(profile.MaxEffort)
}

// CompactionThreshold is the configured threshold capped by the profile.
func (profile ModelProfile) CompactionThreshold(configured int64) int64 {
	if profile.compactionCeiling > 0 {
		return min(configured, profile.compactionCeiling)
	}
	return configured
}

// RequestShape is what a request of this model may contain.
func (profile ModelProfile) RequestShape() conversation.RequestShape {
	return conversation.RequestShape{SystemMessages: profile.systemMessages}
}

// MaximumOutputTokens is the reviewed per-reply request budget.
func (profile ModelProfile) MaximumOutputTokens() int64 { return profile.maxTokens }

// NormalizeCustomEndpointBudget admits a declaration without inferring vendor capacity.
func NormalizeCustomEndpointBudget(endpoint conversation.EndpointSelection) (conversation.EndpointSelection, error) {
	if endpoint.ContextWindow == 0 {
		endpoint.ContextWindow = 32768
	}
	if endpoint.MaxOutputTokens == 0 {
		endpoint.MaxOutputTokens = 4096
	}
	if endpoint.ContextWindow < 32768 || endpoint.ContextWindow > 1_000_000 {
		return conversation.EndpointSelection{}, errors.New("custom context window must contain 32768 to 1000000 tokens")
	}
	if endpoint.MaxOutputTokens < 1 || endpoint.MaxOutputTokens > min(int64(65536), endpoint.ContextWindow/8) {
		return conversation.EndpointSelection{}, errors.New("custom maximum output tokens must be from 1 to the smaller of 65536 and one eighth of the context window")
	}
	return endpoint, nil
}

// ProfileFor derives an immutable execution profile from the frozen endpoint choice.
// Older saved keys and segments keep their conservative default budgets.
func ProfileFor(model conversation.Model, endpoint conversation.EndpointSelection) (ModelProfile, error) {
	profile := ProfileOf(model)
	if !model.IsCustom() {
		return profile, nil
	}
	endpoint, err := NormalizeCustomEndpointBudget(endpoint)
	if err != nil {
		return ModelProfile{}, err
	}
	profile.ContextWindow, profile.maxTokens = endpoint.ContextWindow, endpoint.MaxOutputTokens
	profile.compactionCeiling, profile.HardLimit = endpoint.ContextWindow*11/16, endpoint.ContextWindow*3/4
	return profile, nil
}
