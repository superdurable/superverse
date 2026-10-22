// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package mockmodel

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
)

// followReplay walks a conversation and applies the mock model's cheap
// stand-in for the API's thinking binding: every assistant message must hold
// exactly the blocks this server returned for the reply it stood at, key order
// and whitespace aside. A
// client that edits, drops or reorders a stored block is rejected, as the real
// API rejects a thinking block that no longer matches its conversation. It
// returns the selection in force after the last message.
func followReplay(messages []requestMessage) (selection, error) {
	return walk(messages, func(message requestMessage, sel selection) error {
		expected, err := sel.nextReply()
		if err != nil {
			return err
		}
		if expected.failure != nil || !sameBlocks(message.Content, expected.blocks) {
			return fmt.Errorf("mock replay mismatch at %s", sel.at)
		}
		return nil
	})
}

// sameBlocks compares replayed blocks with the blocks a step produces.
func sameBlocks(replayed []json.RawMessage, expected []block) bool {
	if len(replayed) != len(expected) {
		return false
	}
	for index, raw := range replayed {
		var got, want any
		if json.Unmarshal(raw, &got) != nil || json.Unmarshal(expected[index].json(), &want) != nil || !reflect.DeepEqual(got, want) {
			return false
		}
	}
	return true
}

// followWireReplay uses the same scenario engine with protocol-specific replay.
// Synthetic ciphertext is derived from the original scenario signature, never its thinking text.
func followWireReplay(messages []requestMessage, mode conversation.APIStyle, provider conversation.Provider) (selection, error) {
	compaction := isCompaction(messages)
	if compaction {
		var err error
		messages, err = compactionHistory(messages)
		if err != nil {
			return selection{}, err
		}
	}
	return walk(messages, func(message requestMessage, sel selection) error {
		expected, err := sel.nextReply()
		if err != nil {
			return err
		}
		if expected.stopReason == conversation.StopReasonMaxTokens {
			var projected []block
			for _, b := range expected.blocks {
				switch b.kind {
				case conversation.BlockTypeThinking:
					continue
				case conversation.BlockTypeToolUse:
					projected = append(projected, block{kind: conversation.BlockTypeText, text: "Tool " + b.name + ": not executed (model output limit)."})
				default:
					projected = append(projected, b)
				}
			}
			if !sameBlocks(message.Content, projected) {
				return fmt.Errorf("mock output-limit projection mismatch at %s", sel.at)
			}
			return nil
		}
		var want []json.RawMessage
		firstCall := true
		for index, b := range compatibleReplyBlocks(expected, mode) {
			if b.kind == conversation.BlockTypeThinking {
				if mode == conversation.OpenAIResponses && !compaction {
					want = append(want, mockReasoning(b, index))
				}
			} else {
				if !compaction && mode == conversation.OpenAIChatCompletions && provider == conversation.ProviderGemini && b.kind == conversation.BlockTypeToolUse && firstCall {
					firstCall = false
					want = append(want, mockCallSignature(b.id))
				}
				want = append(want, b.json())
			}
		}
		if expected.failure != nil || len(want) != len(message.Content) {
			return fmt.Errorf("mock wire replay mismatch at %s", sel.at)
		}
		for index, raw := range message.Content {
			var got, expected any
			if json.Unmarshal(raw, &got) != nil || json.Unmarshal(want[index], &expected) != nil || !reflect.DeepEqual(got, expected) {
				return fmt.Errorf("mock wire replay mismatch at %s", sel.at)
			}
		}
		return nil
	})
}

// Responses reasoning profiles always provide replay for tool proposals. Scenarios
// without authored thinking get protocol-only synthetic ciphertext, never new text.
func compatibleReplyBlocks(answer reply, mode conversation.APIStyle) []block {
	if mode != conversation.OpenAIResponses || answer.stopReason == conversation.StopReasonMaxTokens {
		return answer.blocks
	}
	var firstCall string
	for _, b := range answer.blocks {
		if b.kind == conversation.BlockTypeThinking {
			return answer.blocks
		}
		if firstCall == "" && b.kind == conversation.BlockTypeToolUse {
			firstCall = b.id
		}
	}
	if firstCall == "" {
		return answer.blocks
	}
	reasoning := block{kind: conversation.BlockTypeThinking, signature: signature("wire/"+firstCall, position{}, 0)}
	return append([]block{reasoning}, answer.blocks...)
}

// compactionHistory verifies the real request omitted private replay, then reads
// its explicitly quoted tools only to drive the same scenario's position machine.
func compactionHistory(messages []requestMessage) ([]requestMessage, error) {
	result := append([]requestMessage(nil), messages...)
	for index, message := range messages {
		result[index].Content = append([]json.RawMessage(nil), message.Content...)
		for position, raw := range message.Content {
			var header struct{ Type, Text string }
			if json.Unmarshal(raw, &header) != nil {
				return nil, fmt.Errorf("invalid compaction history")
			}
			if header.Type == "mock_reasoning" || header.Type == "mock_signature" || conversation.IsOpaqueReplay(raw) || strings.Contains(string(raw), "mock-encrypted-") {
				return nil, fmt.Errorf("compaction history contains private replay")
			}
			if text, found := strings.CutPrefix(header.Text, conversation.RecordedToolHistoryPrefix); found {
				var tool struct{ Type string }
				if json.Unmarshal([]byte(text), &tool) != nil || tool.Type != conversation.BlockTypeToolUse.String() && tool.Type != conversation.BlockTypeToolResult.String() {
					return nil, fmt.Errorf("invalid quoted compaction tool history")
				}
				result[index].Content[position] = json.RawMessage(text)
			}
		}
	}
	return result, nil
}

func mockReasoning(b block, index int) json.RawMessage {
	return comm.Must(json.Marshal(map[string]any{"type": openaicompat.ItemReasoning.String(), "id": fmt.Sprintf("rs_%d_%s", index, b.signature), "status": conversation.ProviderReplyCompleted, "summary": []any{}, "encrypted_content": "mock-encrypted-" + b.signature}))
}

func mockCallSignature(id string) json.RawMessage {
	return comm.Must(json.Marshal(map[string]string{"type": "mock_signature", "callID": id, "signature": "mock-encrypted-" + id}))
}
