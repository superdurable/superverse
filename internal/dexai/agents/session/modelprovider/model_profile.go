// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider

import (
	"fmt"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// ModelProfile is everything that differs between the supported models. A
// segment runs on one model for its whole life, so its profile never changes
// mid-segment.
type ModelProfile struct {
	model conversation.Model
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
