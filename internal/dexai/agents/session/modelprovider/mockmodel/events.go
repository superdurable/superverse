// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package mockmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// deltaRunes is the size of one text, thinking or tool input delta.
const deltaRunes = 16

// requestID is the request-id of every answer.
const requestID = "req_mock"

// typeField is the JSON field that names the type of a message, of a content
// block and of a server-sent event.
const typeField = "type"

// blockDelta is one delta of a content block: its type and the one field it
// fills, never empty.
type blockDelta struct {
	Type        string `json:"type"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

// writeStream streams a reply as the server-sent events of the Messages API:
// message_start, then per block a content_block_start, its deltas and a
// content_block_stop, then message_delta with the stop reason and the usage,
// and message_stop. The events follow the shapes the SDK's stream decoder and
// BetaMessage.Accumulate read, with a ping after the first block as the API
// sends one. It returns when the reply ends or the client is gone.
func writeStream(ctx context.Context, w http.ResponseWriter, id, model string, answer reply) error {
	if answer.protocolFault == faultWrongModel {
		model += "-unexpected"
	}
	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("request-id", requestID)
	w.WriteHeader(http.StatusOK)
	send := func(event string, data map[string]any) error {
		data[typeField] = event
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, comm.Must(json.Marshal(data))); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	usage := map[string]any{"input_tokens": answer.inputTokens, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
	started := map[string]any{"id": id, typeField: "message", "role": conversation.EntryRoleAssistant, "model": model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": withOutput(usage, 1)}
	if err := send("message_start", map[string]any{"message": started}); err != nil {
		return err
	}
	for index, block := range answer.blocks {
		if err := writeBlock(ctx, send, index, block, answer.chunkDelay); err != nil {
			return err
		}
		if index == 0 {
			if err := send("ping", map[string]any{}); err != nil {
				return err
			}
		}
	}
	delta := map[string]any{"stop_reason": answer.stopReason, "stop_sequence": nil, "stop_details": nil}
	if err := send("message_delta", map[string]any{"delta": delta, "usage": withOutput(usage, answer.outputTokens)}); err != nil {
		return err
	}
	if answer.protocolFault == faultTruncated {
		return nil
	}
	return send("message_stop", map[string]any{})
}

// withOutput copies usage with the output token count.
func withOutput(usage map[string]any, output int64) map[string]any {
	copied := map[string]any{"output_tokens": output}
	for key, value := range usage {
		copied[key] = value
	}
	return copied
}

// writeBlock streams one content block: its start with the empty shape the API
// sends, the deltas that fill it and its stop.
func writeBlock(ctx context.Context, send func(string, map[string]any) error, index int, b block, delay time.Duration) error {
	empty := map[string]any{typeField: b.kind.String()}
	var deltas []blockDelta
	switch b.kind {
	case conversation.BlockTypeRedactedThinking:
		empty["data"] = b.signature
	case conversation.BlockTypeThinking:
		empty["thinking"], empty["signature"] = "", ""
		for _, piece := range chunks(b.text, deltaRunes) {
			deltas = append(deltas, blockDelta{Type: "thinking_delta", Thinking: piece})
		}
		deltas = append(deltas, blockDelta{Type: "signature_delta", Signature: b.signature})
	case conversation.BlockTypeText:
		empty["text"] = ""
		for _, piece := range chunks(b.text, deltaRunes) {
			deltas = append(deltas, blockDelta{Type: "text_delta", Text: piece})
		}
	default:
		empty["id"], empty["name"], empty["input"] = b.id, b.name, map[string]any{}
		for _, piece := range chunks(string(b.input), deltaRunes) {
			deltas = append(deltas, blockDelta{Type: "input_json_delta", PartialJSON: piece})
		}
	}
	sendOfBlock := func(event string, data map[string]any) error {
		data["index"] = index
		return send(event, data)
	}
	if err := sendOfBlock("content_block_start", map[string]any{"content_block": empty}); err != nil {
		return err
	}
	for _, delta := range deltas {
		if err := pause(ctx, delay); err != nil {
			return err
		}
		if err := sendOfBlock("content_block_delta", map[string]any{"delta": delta}); err != nil {
			return err
		}
	}
	return sendOfBlock("content_block_stop", map[string]any{})
}

// pause waits between two deltas unless the client is gone.
func pause(ctx context.Context, delay time.Duration) error {
	if delay == 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// writeOpenAIStream encodes the original scenario reply in the selected wire protocol.
// It never emits the scenario's readable thinking; Responses carries synthetic ciphertext.
func writeOpenAIStream(ctx context.Context, w http.ResponseWriter, id, model string, answer reply, mode conversation.APIStyle, provider conversation.Provider, fault WireFaultForTest, echoTools []json.RawMessage) error {
	if fault == WireFaultOutputLimitRefusalForTest || fault == WireFaultOutputLimitTextForTest {
		answer.stopReason = conversation.StopReasonMaxTokens
	}
	answer.blocks = compatibleReplyBlocks(answer, mode)
	if answer.custom && mode == conversation.OpenAIResponses {
		kept := make([]block, 0, len(answer.blocks))
		for _, b := range answer.blocks {
			if b.kind != conversation.BlockTypeThinking {
				kept = append(kept, b)
			}
		}
		answer.blocks = kept
	}
	if fault == WireFaultOutputLimitTextForTest {
		answer.blocks = []block{{kind: conversation.BlockTypeText, text: "Synthetic completed text at the output limit."}}
	}
	limited := answer.stopReason == conversation.StopReasonMaxTokens
	if limited {
		answer.blocks = append([]block(nil), answer.blocks...)
		for index := range answer.blocks {
			if answer.blocks[index].kind == conversation.BlockTypeToolUse {
				answer.blocks[index].input = json.RawMessage("{")
			}
		}
	}
	if fault == WireFaultRefusalForTest || fault == WireFaultOutputLimitRefusalForTest {
		answer.blocks = []block{{kind: conversation.BlockTypeText}}
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if fault == WireFaultMultilineOversizedForTest {
		// Each data line fits the scanner; their one SSE event exceeds its budget.
		_, err := fmt.Fprintf(w, "data: %s\ndata: %s\ndata: {}\n\n", strings.Repeat(" ", 600<<10), strings.Repeat(" ", 600<<10))
		if flusher != nil {
			flusher.Flush()
		}
		return err
	}
	sequence := 0
	send := func(kind string, value map[string]any) error {
		if err := pause(ctx, answer.chunkDelay); err != nil {
			return err
		}
		if mode == conversation.OpenAIResponses {
			value["type"] = kind
			value["sequence_number"] = sequence
			sequence++
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", comm.Must(json.Marshal(value))); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}
	if fault == WireFaultQwenChunksForTest || fault == WireFaultQwenConflictingTypeForTest {
		if err := send("", chatFrame(model, map[string]any{"role": "assistant", "content": "", "reasoning_content": nil}, nil)); err != nil {
			return err
		}
		if err := send("", chatFrame(model, map[string]any{"reasoning_content": "synthetic forbidden reasoning"}, nil)); err != nil {
			return err
		}
		if err := send("", chatFrame(model, map[string]any{"role": nil, "content": "\n\n", "reasoning_content": nil, "tool_calls": nil}, nil)); err != nil {
			return err
		}
		if err := send("", chatFrame(model, map[string]any{"role": nil, "content": nil, "reasoning_content": nil, "tool_calls": []any{map[string]any{"id": "call_qwen_weather", "index": 0, "type": openaicompat.ItemFunction.String(), "function": map[string]any{"name": "get_weather", "arguments": ""}}}}, nil)); err != nil {
			return err
		}
		for _, piece := range []string{"{", "\"city\"", ": ", "\"Paris\"", "}"} {
			kind := openaicompat.ItemFunction.String()
			if fault == WireFaultQwenConflictingTypeForTest {
				kind = "conflicting_type"
			}
			if err := send("", chatFrame(model, map[string]any{"tool_calls": []any{map[string]any{"id": nil, "index": 0, "type": kind, "function": map[string]any{"name": nil, "arguments": piece}}}}, nil)); err != nil {
				return err
			}
		}
		if err := send("", map[string]any{"model": model, "usage": nil, "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": nil}, "finish_reason": openaicompat.FinishToolCalls.String(), "matched_stop": 248046}}}); err != nil {
			return err
		}
		_, err := fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		return err
	}

	if fault == WireFaultStreamRejectedForTest || fault == WireFaultChatRejectedForTest {
		failure := map[string]any{"code": "invalid_prompt", "type": "invalid_request_error", "message": "Synthetic secret-bearing provider message must be discarded."}
		if mode == conversation.OpenAIResponses {
			if fault == WireFaultChatRejectedForTest {
				return send(openaicompat.EventProviderError.String(), failure)
			}
			return send(openaicompat.EventResponseFailed.String(), map[string]any{"response": map[string]any{"error": failure}})
		}
		return send("", map[string]any{"error": failure})
	}
	if fault == WireFaultPlaintextForTest || answer.protocolFault == faultCompatibleReasoning {
		if mode == conversation.OpenAIResponses {
			if err := send(openaicompat.EventResponseReasoningTextDelta.String(), map[string]any{"delta": "synthetic forbidden reasoning"}); err != nil {
				return err
			}
			if err := send(openaicompat.EventResponseReasoningSummaryDelta.String(), map[string]any{"delta": "synthetic forbidden reasoning"}); err != nil {
				return err
			}
			for _, kind := range []openaicompat.EventType{openaicompat.EventResponseReasoningTextDone, openaicompat.EventResponseReasoningSummaryDone, openaicompat.EventResponseReasoningPartAdded, openaicompat.EventResponseReasoningPartDone} {
				if err := send(kind.String(), map[string]any{"text": "synthetic forbidden reasoning", "part": map[string]string{"type": "summary_text", "text": "synthetic forbidden reasoning"}}); err != nil {
					return err
				}
			}
		} else if err := send("", chatFrame(model, map[string]any{"reasoning_content": "synthetic forbidden reasoning", "thinking": "synthetic forbidden reasoning"}, nil)); err != nil {
			return err
		}
	}
	if mode == conversation.OpenAIChatCompletions {
		toolIndex := 0
		for _, b := range answer.blocks {
			switch b.kind {
			case conversation.BlockTypeThinking:
				continue
			case conversation.BlockTypeText:
				for _, piece := range chunks(b.text, deltaRunes) {
					if err := send("", chatFrame(model, map[string]any{"content": piece, "reasoning_content": compatibleReasoning(fault, answer.protocolFault)}, nil)); err != nil {
						return err
					}
				}
			case conversation.BlockTypeToolUse:
				for index, piece := range chunks(string(b.input), deltaRunes) {
					call := map[string]any{"index": toolIndex, "type": openaicompat.ItemFunction.String(), "id": nil, openaicompat.ItemFunction.String(): map[string]any{"name": nil, "arguments": piece}}
					if index == 0 {
						if provider == conversation.ProviderGemini && !answer.custom && toolIndex == 0 && fault != WireFaultMissingReplayForTest && !limited {
							call["extra_content"] = map[string]any{"google": map[string]string{"thought_signature": fixtureCiphertext("mock-encrypted-"+b.id, fault)}}
						}
						call["id"] = b.id
						call["type"] = openaicompat.ItemFunction.String()
						call[openaicompat.ItemFunction.String()] = map[string]string{"name": b.name, "arguments": piece}
					}
					if err := send("", chatFrame(model, map[string]any{"tool_calls": []any{call}}, nil)); err != nil {
						return err
					}
				}
				toolIndex++
			}
		}
		finish := openaicompat.FinishStop.String()
		if toolIndex > 0 {
			finish = openaicompat.FinishToolCalls.String()
		}
		if fault == WireFaultRefusalWithToolsForTest || fault == WireFaultRefusalForTest {
			finish = openaicompat.FinishContentFilter.String()
		}
		if limited {
			finish = openaicompat.FinishLength.String()
		}
		if err := send("", chatFrame(model, map[string]any{}, finish)); err != nil {
			return err
		}
		if err := send("", map[string]any{"model": model, "choices": []any{}, "usage": map[string]int64{"prompt_tokens": answer.inputTokens, "completion_tokens": answer.outputTokens}}); err != nil {
			return err
		}
		if fault == WireFaultTruncatedForTest {
			return nil
		}
		_, err := fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		return err
	}
	var output []any
	for index, b := range answer.blocks {
		itemID := fmt.Sprintf("%s_%d", id, index)
		var initial, final map[string]any
		switch b.kind {
		case conversation.BlockTypeThinking:
			comm.MustDo(json.Unmarshal(mockReasoning(b, index), &final))
			itemID = final["id"].(string)
			final["encrypted_content"] = fixtureCiphertext(final["encrypted_content"].(string), fault)
			initial = map[string]any{"type": openaicompat.ItemReasoning.String(), "id": itemID, "summary": []any{}}
			if fault == WireFaultPlaintextForTest {
				final["summary"] = []any{map[string]string{"type": "summary_text", "text": "synthetic forbidden reasoning"}}
				final["content"] = []any{map[string]string{"type": "reasoning_text", "text": "synthetic forbidden reasoning"}}
			}
		case conversation.BlockTypeText:
			initial = map[string]any{"type": openaicompat.ItemMessage.String(), "id": itemID, "role": "assistant", "status": conversation.ProviderReplyInProgress, "content": []any{}}
			final = map[string]any{"type": openaicompat.ItemMessage.String(), "id": itemID, "role": "assistant", "status": conversation.ProviderReplyCompleted, "content": []any{map[string]any{"type": openaicompat.ItemOutputText.String(), "text": b.text, "annotations": []any{}}}}
		case conversation.BlockTypeToolUse:
			initial = map[string]any{"type": openaicompat.ItemFunctionCall.String(), "id": itemID, "call_id": b.id, "name": b.name, "arguments": "", "status": conversation.ProviderReplyInProgress}
			final = map[string]any{"type": openaicompat.ItemFunctionCall.String(), "id": itemID, "call_id": b.id, "name": b.name, "arguments": string(b.input), "status": conversation.ProviderReplyCompleted}
		default:
			return fmt.Errorf("unsupported mock compatible output kind")
		}
		if b.kind == conversation.BlockTypeThinking || b.kind == conversation.BlockTypeToolUse {
			switch fault {
			case WireFaultOmittedItemStatusForTest, WireFaultOmittedWithoutDoneForTest, WireFaultOmittedIncompleteResponseForTest, WireFaultMixedItemStatusForTest:
				delete(initial, "status")
				delete(final, "status")
			case WireFaultNullItemStatusForTest:
				initial["status"], final["status"] = nil, nil
			case WireFaultIncompleteItemStatusForTest:
				final["status"] = "incomplete"
			case WireFaultInProgressItemStatusForTest:
				final["status"] = conversation.ProviderReplyInProgress
			case WireFaultUnknownItemStatusForTest:
				final["status"] = "unknown"
			case WireFaultEmptyItemStatusForTest:
				final["status"] = ""
			}
		}
		if b.kind == conversation.BlockTypeText && (fault == WireFaultRefusalWithToolsForTest || fault == WireFaultRefusalForTest || fault == WireFaultOutputLimitRefusalForTest) {
			final["content"] = []any{map[string]string{"type": openaicompat.ItemRefusal.String(), "refusal": "Synthetic provider refusal."}}
		}
		if err := send(openaicompat.EventResponseOutputItemAdded.String(), map[string]any{"output_index": index, "item": initial}); err != nil {
			return err
		}
		kind, text := "", ""
		if b.kind == conversation.BlockTypeText && (fault == WireFaultRefusalWithToolsForTest || fault == WireFaultRefusalForTest || fault == WireFaultOutputLimitRefusalForTest) {
			kind = openaicompat.EventResponseRefusalDelta.String()
			text = "Synthetic provider refusal."
		}
		if b.kind == conversation.BlockTypeText && fault != WireFaultRefusalWithToolsForTest && fault != WireFaultRefusalForTest && fault != WireFaultOutputLimitRefusalForTest {
			kind = openaicompat.EventResponseOutputTextDelta.String()
			text = b.text
		}
		if b.kind == conversation.BlockTypeToolUse {
			kind = openaicompat.EventResponseFunctionArgumentsDelta.String()
			text = string(b.input)
		}
		if kind != "" {
			for _, piece := range chunks(text, deltaRunes) {
				if err := send(kind, map[string]any{"output_index": index, "item_id": itemID, "delta": piece, "content_index": 0}); err != nil {
					return err
				}
			}
		}
		if kind == openaicompat.EventResponseRefusalDelta.String() {
			if err := send(openaicompat.EventResponseRefusalDone.String(), map[string]any{"output_index": index, "item_id": itemID, "refusal": text}); err != nil {
				return err
			}
		}
		if limited && b.kind == conversation.BlockTypeToolUse {
			final["status"] = conversation.ProviderReplyIncomplete
		}
		if (!limited || b.kind != conversation.BlockTypeToolUse) && (fault != WireFaultOmittedWithoutDoneForTest || b.kind == conversation.BlockTypeText) {
			if err := send(openaicompat.EventResponseOutputItemDone.String(), map[string]any{"output_index": index, "item": final}); err != nil {
				return err
			}
		}
		if fault == WireFaultMixedItemStatusForTest && b.kind != conversation.BlockTypeText {
			final["status"] = conversation.ProviderReplyCompleted
		}
		output = append(output, final)
	}
	if fault == WireFaultTruncatedForTest {
		return nil
	}
	if limited {
		return send(openaicompat.EventResponseIncomplete.String(), map[string]any{"response": map[string]any{"status": conversation.ProviderReplyIncomplete, "model": model, "output": output, "incomplete_details": map[string]string{"reason": "max_output_tokens"}}})
	}
	if fault == WireFaultOmittedIncompleteResponseForTest {
		return send(openaicompat.EventResponseIncomplete.String(), map[string]any{"response": map[string]any{"status": "incomplete"}})
	}
	return send(openaicompat.EventResponseCompleted.String(), map[string]any{"response": map[string]any{"id": id, "status": conversation.ProviderReplyCompleted, "model": model, "output": output, "tools": echoTools, "reasoning": map[string]any{"effort": nil, "summary": nil}, "usage": map[string]int64{"input_tokens": answer.inputTokens, "output_tokens": answer.outputTokens}}})
}

func compatibleReasoning(fault WireFaultForTest, scenarioFault protocolFault) string {
	if fault == WireFaultPlaintextForTest || scenarioFault == faultCompatibleReasoning {
		return "synthetic forbidden reasoning"
	}
	return ""
}

func chatFrame(model string, delta map[string]any, finish any) map[string]any {
	return map[string]any{"error": nil, "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
}

// WireFaultForTest is a closed corruption of the model protocol, applied after the shared
// scenario engine has produced its reply. It is never an application dependency substitute.
type WireFaultForTest struct{ id string }

var (
	WireFaultOutputLimitRefusalForTest        = WireFaultForTest{"output_limit_refusal"}
	WireFaultOutputLimitTextForTest           = WireFaultForTest{"output_limit_text"}
	WireFaultCreditExhaustedForTest           = WireFaultForTest{"credit_exhausted"}
	WireFaultContextRejectedForTest           = WireFaultForTest{"context_rejected"}
	WireFaultStreamRejectedForTest            = WireFaultForTest{"stream_rejected"}
	WireFaultChatRejectedForTest              = WireFaultForTest{"chat_rejected"}
	WireFaultRefusalForTest                   = WireFaultForTest{"refusal"}
	WireFaultRefusalWithToolsForTest          = WireFaultForTest{"refusal_with_tools"}
	WireFaultHTTP401ForTest                   = WireFaultForTest{"http_401"}
	WireFaultHTTP429ForTest                   = WireFaultForTest{"http_429"}
	WireFaultHTTP503ForTest                   = WireFaultForTest{"http_503"}
	WireFaultTruncatedForTest                 = WireFaultForTest{"truncated"}
	WireFaultQwenChunksForTest                = WireFaultForTest{"qwen_tool_chunks"}
	WireFaultQwenConflictingTypeForTest       = WireFaultForTest{"qwen_conflicting_type"}
	WireFaultPlaintextForTest                 = WireFaultForTest{"plaintext"}
	WireFaultMissingReplayForTest             = WireFaultForTest{"missing_replay"}
	WireFaultOversizedReplayForTest           = WireFaultForTest{"oversized_replay"}
	WireFaultMultilineOversizedForTest        = WireFaultForTest{"multiline_oversized"}
	WireFaultOmittedItemStatusForTest         = WireFaultForTest{"omitted_item_status"}
	WireFaultNullItemStatusForTest            = WireFaultForTest{"null_item_status"}
	WireFaultIncompleteItemStatusForTest      = WireFaultForTest{"incomplete_item_status"}
	WireFaultInProgressItemStatusForTest      = WireFaultForTest{"in_progress_item_status"}
	WireFaultUnknownItemStatusForTest         = WireFaultForTest{"unknown_item_status"}
	WireFaultEmptyItemStatusForTest           = WireFaultForTest{"empty_item_status"}
	WireFaultOmittedWithoutDoneForTest        = WireFaultForTest{"omitted_status_without_item_done"}
	WireFaultOmittedIncompleteResponseForTest = WireFaultForTest{"omitted_status_incomplete_response"}
	WireFaultMixedItemStatusForTest           = WireFaultForTest{"mixed_item_status"}
)

func fixtureCiphertext(value string, fault WireFaultForTest) string {
	switch fault {
	case WireFaultMissingReplayForTest:
		return ""
	case WireFaultOversizedReplayForTest:
		return strings.Repeat("x", conversation.MaximumOpaqueReplayBytes+1)
	default:
		return value
	}
}
