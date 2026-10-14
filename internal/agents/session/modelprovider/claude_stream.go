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
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

// readStream accumulates every event into the complete message, forwards the
// live presentation (summarized thinking, text, completed tool calls and a
// fallback) and builds the reply from the accumulated message. Signature and
// tool-input deltas are only accumulated.
func readStream(stream *ssestream.Stream[anthropic.BetaRawMessageStreamEventUnion], request conversation.ModelRequest, startedAt time.Time) (conversation.ModelReply, error) {
	var message anthropic.BetaMessage
	var firstEventMS *int64
	for stream.Next() {
		if firstEventMS == nil {
			elapsed := time.Since(startedAt).Milliseconds()
			firstEventMS = &elapsed
		}
		event := stream.Current()
		if err := message.Accumulate(event); err != nil {
			return conversation.ModelReply{}, err
		}
		if err := forwardEvent(event, message, request); err != nil {
			return conversation.ModelReply{}, err
		}
	}
	if err := stream.Err(); err != nil {
		return conversation.ModelReply{}, err
	}
	if message.StopReason == "" {
		return conversation.ModelReply{}, errors.New("the Claude response stream ended without a stop reason")
	}
	reply := buildReply(message, request)
	reply.Metrics.DurationMS, reply.Metrics.FirstEventMS = time.Since(startedAt).Milliseconds(), firstEventMS
	return reply, nil
}

// forwardEvent writes the live presentation of one event.
func forwardEvent(event anthropic.BetaRawMessageStreamEventUnion, message anthropic.BetaMessage, request conversation.ModelRequest) error {
	switch event.Type {
	case "content_block_delta":
		switch event.Delta.Type {
		case "thinking_delta":
			if event.Delta.Thinking != "" {
				return request.WriteReasoning(event.Delta.Thinking)
			}
		case "text_delta":
			if event.Delta.Text != "" {
				return request.WriteAssistant(event.Delta.Text)
			}
		}
	case "content_block_start":
		if block := message.Content[event.Index]; block.Type == conversation.BlockTypeFallback.String() {
			return request.WriteActivity(conversation.AgentEvent{Kind: conversation.EventKindModelFallback,
				Message: "Continuing with " + block.To.Model + "."})
		}
	case "content_block_stop":
		if block := message.Content[event.Index]; block.Type == conversation.BlockTypeToolUse.String() {
			call := toolCall(block)
			target := request.ToolTarget(call)
			return request.WriteActivity(conversation.AgentEvent{Kind: conversation.EventKindModelToolCall,
				Message: "Model requested " + string(call.Name) + ".", CallID: &call.ID, ToolName: &call.Name, Target: target})
		}
	}
	return nil
}

// buildReply reads the stop reason, then the content: the blocks to store
// after the fallback cleanup, their tool calls and text, and the metrics.
func buildReply(message anthropic.BetaMessage, request conversation.ModelRequest) conversation.ModelReply {
	kept, dropped := afterFallback(message.Content)
	reply := conversation.ModelReply{StopReason: conversation.StopReason(message.StopReason), RefusalCategory: string(message.StopDetails.Category),
		Blocks: make([]json.RawMessage, 0, len(kept)), ToolCalls: []conversation.ToolCall{}}
	var text []string
	for _, block := range kept {
		reply.Blocks = append(reply.Blocks, json.RawMessage(block.RawJSON()))
		switch block.Type {
		case conversation.BlockTypeToolUse.String():
			reply.ToolCalls = append(reply.ToolCalls, toolCall(block))
		case conversation.BlockTypeText.String():
			text = append(text, block.Text)
		}
	}
	reply.Text = strings.Join(text, "\n\n")
	reply.Metrics = usageMetrics(message, request)
	reply.Metrics.DroppedThinkingBlocks += dropped
	return reply
}

// afterFallback removes what a refused model produced before a server-side
// fallback served the reply: every thinking, redacted thinking and tool_use
// block before the last fallback block, a server_tool_use there without its
// result, and the fallback block itself. It returns the kept blocks and how
// many thinking blocks it dropped.
func afterFallback(blocks []anthropic.BetaContentBlockUnion) ([]anthropic.BetaContentBlockUnion, int) {
	last := -1
	for index, block := range blocks {
		if block.Type == conversation.BlockTypeFallback.String() {
			last = index
		}
	}
	if last < 0 {
		return blocks, 0
	}
	answered := map[string]bool{}
	for _, block := range blocks[:last] {
		if block.ToolUseID != "" {
			answered[block.ToolUseID] = true
		}
	}
	kept, dropped := make([]anthropic.BetaContentBlockUnion, 0, len(blocks)), 0
	for _, block := range blocks[:last] {
		switch {
		case block.Type == conversation.BlockTypeThinking.String() || block.Type == conversation.BlockTypeRedactedThinking.String():
			dropped++
		case block.Type == conversation.BlockTypeToolUse.String(), block.Type == conversation.BlockTypeServerToolUse.String() && !answered[block.ID]:
		default:
			kept = append(kept, block)
		}
	}
	return append(kept, blocks[last+1:]...), dropped
}

// toolCall reads a complete tool_use block. A call cut off by the output
// limit can carry an input that is no JSON object; it is recorded as an empty
// object and answered without running.
func toolCall(block anthropic.BetaContentBlockUnion) conversation.ToolCall {
	arguments, err := conversation.ParseJSONObject(string(block.Input))
	if err != nil {
		arguments = conversation.MustJSONObject("{}")
	}
	return conversation.ToolCall{ID: conversation.CallID(block.ID), Name: conversation.ToolName(block.Name), Arguments: arguments}
}

// usageMetrics reads the usage the API reported; a counter it did not report
// stays absent.
func usageMetrics(message anthropic.BetaMessage, request conversation.ModelRequest) conversation.ModelCallMetrics {
	metrics := conversation.ModelCallMetrics{Model: request.Model.ProviderModel(), ServedModel: message.Model, Effort: request.Effort}
	usage := message.Usage
	count := func(valid bool, value int64) *int64 {
		if !valid {
			return nil
		}
		return &value
	}
	metrics.InputTokens = count(usage.JSON.InputTokens.Valid(), usage.InputTokens)
	metrics.CacheReadInputTokens = count(usage.JSON.CacheReadInputTokens.Valid(), usage.CacheReadInputTokens)
	metrics.CacheCreationInputTokens = count(usage.JSON.CacheCreationInputTokens.Valid(), usage.CacheCreationInputTokens)
	metrics.OutputTokens = count(usage.JSON.OutputTokens.Valid(), usage.OutputTokens)
	metrics.ThinkingTokens = count(usage.OutputTokensDetails.JSON.ThinkingTokens.Valid(), usage.OutputTokensDetails.ThinkingTokens)
	for _, transformation := range message.InputTransformations {
		metrics.InputTransformations = append(metrics.InputTransformations, conversation.InputTransformation{Type: transformation.Type, Path: transformation.Path, Reason: transformation.Reason})
		if transformation.Type == "thinking_dropped" {
			metrics.DroppedThinkingBlocks++
		}
	}
	return metrics
}
