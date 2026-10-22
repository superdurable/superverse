// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelprovider

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

var messagesStreamInterrupted = errors.New("model provider stream ended before message_stop")

const maximumMessagesEventBytes = 16 << 20

// readCompatibleStream validates before accumulation or presentation. Readable
// reasoning is a deterministic rejection, never a persisted partial reply.
func readCompatibleStream(stream *ssestream.Stream[anthropic.BetaRawMessageStreamEventUnion], request conversation.ModelRequest, profile ModelProfile, startedAt time.Time) (conversation.ModelReply, error) {
	var message anthropic.BetaMessage
	var firstEventMS *int64
	started, stopped, inBlock, opaque := false, false, false, false
	opaqueBytes := 0
	seenOpaque := map[string]bool{}
	seenCalls := map[string]bool{}
	eventBytes := 0
	deltaReceived := false
	for stream.Next() {
		event := stream.Current()
		eventBytes += len(event.RawJSON())
		if eventBytes > maximumMessagesEventBytes {
			return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
		}
		if firstEventMS == nil {
			elapsed := time.Since(startedAt).Milliseconds()
			firstEventMS = &elapsed
		}
		if stopped {
			return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
		}
		switch event.Type {
		case "message_start":
			if started || len(event.Message.Content) != 0 || event.Message.Model != request.APIModelID() || string(event.Message.Role) != string(conversation.EntryRoleAssistant) {
				return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
			}
			started = true
		case "content_block_start":
			if !started || deltaReceived || inBlock || event.Index != int64(len(message.Content)) {
				return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
			}
			inBlock = true
			block := event.ContentBlock
			switch block.Type {
			case conversation.BlockTypeText.String():
				if !onlyFields(block.RawJSON(), "type", "text") {
					return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
				}
			case conversation.BlockTypeToolUse.String():
				if request.ToolChoiceNone || block.ID == "" || seenCalls[block.ID] || !offeredMessagesTool(request.Prompts.Tools, block.Name) {
					return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
				}
				seenCalls[block.ID] = true
				if !onlyFields(block.RawJSON(), "type", "id", "name", "input") {
					return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
				}
			case conversation.BlockTypeRedactedThinking.String():
				if profile.Capabilities.Replay != conversation.ReplayEncryptedOpaque || block.Data == "" || seenOpaque[block.Data] {
					return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
				}
				// Only the documented encrypted carrier survives. Unknown
				// fields could carry summaries or readable reasoning.
				var fields map[string]json.RawMessage
				if json.Unmarshal([]byte(block.RawJSON()), &fields) != nil || len(fields) != 2 {
					return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
				}
				seenOpaque[block.Data] = true
				opaqueBytes += len(block.RawJSON())
				if opaqueBytes > conversation.MaximumOpaqueReplayBytes {
					return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
				}
				opaque = true
			default:
				return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
			}
		case "content_block_delta":
			if !inBlock || event.Index != int64(len(message.Content)-1) {
				return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
			}
			switch event.Delta.Type {
			case "text_delta":
				if message.Content[event.Index].Type != conversation.BlockTypeText.String() || !onlyFields(eventDeltaJSON(event.RawJSON()), "type", "text") {
					return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
				}
			case "input_json_delta":
				if message.Content[event.Index].Type != conversation.BlockTypeToolUse.String() || !onlyFields(eventDeltaJSON(event.RawJSON()), "type", "partial_json") {
					return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
				}
			default:
				return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
			}
		case "content_block_stop":
			if !inBlock || event.Index != int64(len(message.Content)-1) {
				return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
			}
			inBlock = false
		case "message_delta":
			if !started || inBlock || deltaReceived {
				return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
			}
			deltaReceived = true
		case "message_stop":
			if !started || inBlock || message.StopReason == "" {
				return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
			}
			stopped = true
		case "ping":
		default:
			return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
		}
		if err := message.Accumulate(event); err != nil {
			return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
		}
		if err := forwardEvent(event, message, request); err != nil {
			return conversation.ModelReply{}, err
		}
	}
	if err := stream.Err(); err != nil {
		return conversation.ModelReply{}, err
	}
	if !stopped {
		return conversation.ModelReply{}, messagesStreamInterrupted
	}
	if profile.Capabilities.Replay == conversation.ReplayEncryptedOpaque && !opaque {
		return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
	}
	switch conversation.StopReason(message.StopReason) {
	case conversation.StopReasonEndTurn, conversation.StopReasonToolUse, conversation.StopReasonMaxTokens, conversation.StopReasonRefusal, conversation.StopReasonModelContextWindowExceeded:
	default:
		return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
	}
	if profile.Capabilities.Replay == conversation.ReplayNonThinking && message.Usage.OutputTokensDetails.ThinkingTokens > 0 {
		return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
	}
	reply := buildReply(message, request)
	if profile.Capabilities.Replay == conversation.ReplayEncryptedOpaque {
		encodedBytes := 0
		for position, block := range message.Content {
			if block.Type != conversation.BlockTypeRedactedThinking.String() {
				continue
			}
			encoded, err := (conversation.OpaqueReplay{Provider: request.Model.Details().Provider.String(), WireMode: conversation.AnthropicMessages.String(), Position: position, Ciphertext: block.Data}).Block()
			encodedBytes += len(encoded)
			if err != nil || encodedBytes > conversation.MaximumOpaqueReplayBytes {
				return conversation.ModelReply{}, conversation.ProviderProtocolRejected.Rejected(0)
			}
			reply.Blocks[position] = encoded
		}
	}
	// Compatible providers have no documented Claude diagnostic/refusal-detail
	// fields. Persist only reviewed model identities and numeric usage.
	reply.RefusalCategory = ""
	reply.Metrics.InputTransformations = nil
	reply.Metrics.DroppedThinkingBlocks = 0
	reply.Metrics.Effort = ""
	reply.Metrics.DurationMS, reply.Metrics.FirstEventMS = time.Since(startedAt).Milliseconds(), firstEventMS
	return reply, nil
}

// onlyFields rejects undocumented carriers before their bytes reach storage.
func onlyFields(raw string, allowed ...string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil || len(fields) != len(allowed) {
		return false
	}
	for _, field := range allowed {
		if _, found := fields[field]; !found {
			return false
		}
	}
	return true
}

func eventDeltaJSON(raw string) string {
	var event struct {
		Delta json.RawMessage `json:"delta"`
	}
	if json.Unmarshal([]byte(raw), &event) != nil {
		return ""
	}
	return string(event.Delta)
}

func offeredMessagesTool(tools []json.RawMessage, name string) bool {
	for _, raw := range tools {
		var tool struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &tool) != nil {
			panic("invalid frozen tool declaration")
		}
		if tool.Name == name {
			return true
		}
	}
	return false
}
