// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package mockmodel

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
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
	return followMessagesReplay(messages, conversation.ModelMock, false)
}

func followMessagesReplay(messages []requestMessage, model conversation.Model, compaction bool) (selection, error) {
	var err error
	messages, err = projectedHandoffReplay(messages)
	if err != nil {
		return selection{}, err
	}
	return walk(messages, func(message requestMessage, sel selection) error {
		expected, err := sel.nextReply()
		if err != nil {
			return err
		}
		if message.projectedHistory && sel.scriptStep() != nil && !sel.scriptStep().Remote {
			// A user-first retained suffix can change the grouping that originally
			// derived a mock run. Its quoted platform IDs retain that original run,
			// as in custom-endpoint replay, while step/tool identities stay exact.
			var sourceRun string
			for _, raw := range message.Content {
				var use struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				}
				if json.Unmarshal(raw, &use) != nil || use.Type != conversation.BlockTypeToolUse.String() {
					continue
				}
				source := quotedCustomRun.FindStringSubmatch(use.ID)
				if len(source) != 2 || sourceRun != "" && sourceRun != source[1] {
					return errors.New("projected platform tools lack one original scenario run")
				}
				sourceRun = source[1]
			}
			if sourceRun != "" {
				expected, err = sel.script.replyAt(sel.at, sel.arguments, sourceRun)
				if err != nil {
					return err
				}
			}
		}
		expected = messagesReply(expected, model)
		if compaction && model != conversation.ModelMock {
			blocks := []block{}
			for _, item := range expected.blocks {
				if item.kind != conversation.BlockTypeRedactedThinking {
					blocks = append(blocks, item)
				}
			}
			expected.blocks = blocks
		}
		if message.projectedHistory {
			// The prefix validator already proved the text-only wire shape.
			// Compare every public tool field after unquoting, retaining the
			// existing sameBlocks rule that JSON key order is immaterial.
			blocks := []block{}
			for _, item := range expected.blocks {
				if item.kind == conversation.BlockTypeText || item.kind == conversation.BlockTypeToolUse {
					blocks = append(blocks, item)
				}
			}
			if len(blocks) == 0 {
				blocks = append(blocks, block{kind: conversation.BlockTypeText, text: "(No visible message content.)"})
			}
			expected.blocks = blocks
		}
		if expected.failure != nil || !sameBlocks(message.Content, expected.blocks) {
			return fmt.Errorf("mock replay mismatch at %s", sel.at)
		}
		return nil
	})
}

// projectedHandoffReplay verifies the actual public-only prefix, then unwraps
// quoted tool blocks solely for the scenario walk's ID/result/order checks.
// The assistant comparator requires every field of the exact projected script,
// allowing only JSON key-order/whitespace differences. A canonical handoff at its declared script boundary is
// mandatory; a wire flag, arbitrary notice or quoted block cannot enable this.
func projectedHandoffReplay(messages []requestMessage) ([]requestMessage, error) {
	boundary := -1
	for index, message := range messages {
		for _, text := range message.texts() {
			if strings.HasPrefix(text, remoteexec.HandoffNoticePrefix) {
				if message.Role != string(conversation.EntryRoleUser) {
					return nil, errors.New("canonical handoff must be a user boundary")
				}
				if _, err := remoteexec.ParseHandoffNotice(text); err != nil {
					return nil, err
				}
				boundary = index
			}
		}
	}
	if boundary < 0 {
		return messages, nil
	}
	projected := slices.Clone(messages)
	pending, seen := map[string]bool{}, map[string]bool{}
	for index := 0; index <= boundary; index++ {
		message := &projected[index]
		if message.Role == string(conversation.EntryRoleAssistant) && len(pending) != 0 {
			return nil, errors.New("projected handoff contains an incomplete preceding tool unit")
		}
		message.projectedHistory = message.Role == string(conversation.EntryRoleAssistant)
		message.Content = slices.Clone(message.Content)
		for position, raw := range message.Content {
			_, kind, err := interactiveFields(raw, message.Role == string(conversation.EntryRoleUser))
			if err != nil {
				return nil, err
			}
			if kind != conversation.BlockTypeText.String() && kind != conversation.BlockTypeImage.String() {
				return nil, errors.New("handoff prefix contains native protocol history")
			}
			var text textJSON
			if kind != conversation.BlockTypeText.String() || json.Unmarshal(raw, &text) != nil || !strings.HasPrefix(text.Text, conversation.RecordedToolHistoryPrefix) {
				continue
			}
			quoted := json.RawMessage(strings.TrimPrefix(text.Text, conversation.RecordedToolHistoryPrefix))
			fields, quotedKind, err := interactiveFields(quoted, message.Role == string(conversation.EntryRoleUser))
			if err != nil {
				return nil, err
			}
			if quotedKind == conversation.BlockTypeToolUse.String() && message.Role == string(conversation.EntryRoleAssistant) ||
				quotedKind == conversation.BlockTypeToolResult.String() && message.Role == string(conversation.EntryRoleUser) {
				var id string
				key := "id"
				if quotedKind == conversation.BlockTypeToolResult.String() {
					key = "tool_use_id"
				}
				if json.Unmarshal(fields[key], &id) != nil || id == "" {
					return nil, errors.New("projected tool history lacks its stable identity")
				}
				if quotedKind == conversation.BlockTypeToolUse.String() {
					if seen[id] {
						return nil, errors.New("projected handoff repeats a tool identity")
					}
					pending[id], seen[id] = true, true
				} else {
					if !pending[id] {
						return nil, errors.New("projected result does not answer one pending tool")
					}
					delete(pending, id)
				}
				message.Content[position] = quoted
			} else {
				return nil, errors.New("quoted handoff history has an invalid tool role or kind")
			}
		}
	}
	if len(pending) != 0 {
		return nil, errors.New("projected handoff ends with an incomplete tool unit")
	}
	return projected, nil
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
func followWireReplay(messages []requestMessage, mode conversation.APIStyle, provider conversation.Provider, custom ...bool) (selection, error) {
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
		if len(custom) > 0 && custom[0] {
			expected = messagesReply(expected, conversation.ModelOpenAICustom)
		}
		var want []json.RawMessage
		firstCall := true
		for index, b := range compatibleReplyBlocks(expected, mode) {
			if b.kind == conversation.BlockTypeThinking {
				if mode == conversation.OpenAIResponses && !compaction {
					want = append(want, mockReasoning(b, index))
				}
			} else {
				if !compaction && mode == conversation.OpenAIChatCompletions && provider == conversation.ProviderGemini && !expected.custom && b.kind == conversation.BlockTypeToolUse && firstCall {
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
	if answer.custom || mode != conversation.OpenAIResponses || answer.stopReason == conversation.StopReasonMaxTokens {
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

// messagesReply uses the same scenario engine for compatible wire profiles.
// Its encrypted carrier is synthetic and proves no live provider compatibility.
func messagesReply(answer reply, model conversation.Model) reply {
	if model == "" || model == conversation.ModelMock {
		return answer
	}
	answer.custom = model.IsCustom()
	replay := modelprovider.ProfileOf(model).Capabilities.Replay
	kept := []block{}
	for _, item := range answer.blocks {
		if item.kind != conversation.BlockTypeThinking {
			kept = append(kept, item)
		}
	}
	if replay == conversation.ReplayEncryptedOpaque && answer.failure == nil {
		encoded := []json.RawMessage{}
		for _, item := range kept {
			encoded = append(encoded, item.json())
		}
		sum := sha256.Sum256(comm.Must(json.Marshal(encoded)))
		kept = append([]block{{kind: conversation.BlockTypeRedactedThinking, signature: base64.StdEncoding.EncodeToString(sum[:])}}, kept...)
	}
	switch answer.protocolFault {
	case faultPlaintext:
		kept = append([]block{{kind: conversation.BlockTypeThinking, text: "synthetic private reasoning must never escape", signature: "synthetic"}}, kept...)
	case faultMissingOpaque:
		without := []block{}
		for _, item := range kept {
			if item.kind != conversation.BlockTypeRedactedThinking {
				without = append(without, item)
			}
		}
		kept = without
	case faultDuplicateOpaque:
		if len(kept) > 0 && kept[0].kind == conversation.BlockTypeRedactedThinking {
			kept = append([]block{kept[0]}, kept...)
		}
	case faultOversizeOpaque:
		kept = append([]block{{kind: conversation.BlockTypeRedactedThinking, signature: strings.Repeat("x", 256<<10)}}, kept...)
	}
	answer.blocks = kept
	return answer
}

func (request messagesRequest) catalogModel() conversation.Model {
	if request.Custom || request.Model == customDiscoveredModelID || request.Model == customManualModelID {
		if request.WireMode == conversation.OpenAIResponses {
			return conversation.ModelOpenAICustom
		}
		if request.WireMode == conversation.OpenAIChatCompletions {
			return conversation.ModelQwenCustom
		}
		return conversation.ModelAnthropicCustom
	}
	for _, model := range []conversation.Model{conversation.ModelMock, conversation.ModelDeepSeekFlash, conversation.ModelMiniMaxM3, conversation.ModelMuseWonder10} {
		if request.Model == model.ProviderModel() {
			return model
		}
	}
	return ""
}

// validateMessagesProfile checks the actual adapter wire request rather than
// inferring compatibility from a model label or a decoded ModelReply.
func (request messagesRequest) validateMessagesProfile(r *http.Request, body []byte) error {
	if r.Header.Get("anthropic-beta") != "" {
		return errors.New("compatible requests must not carry Claude betas")
	}
	model := request.catalogModel()
	if model == conversation.ModelMuseWonder10 {
		if r.Header.Get("Authorization") != "Bearer "+apiKey || r.Header.Get("X-Api-Key") != "" {
			return errors.New("Meta requires only bearer authentication")
		}
	} else if r.Header.Get("X-Api-Key") != apiKey || r.Header.Get("Authorization") != "" {
		return errors.New("compatible SDK request requires only API-key authentication")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return errors.New("compatible request is not an object")
	}
	allowed := map[string]bool{"model": true, "max_tokens": true, "system": true, "messages": true, "tools": true, "tool_choice": true, "stream": true}
	control, want := "thinking", `{"type":"disabled"}`
	if model == conversation.ModelMuseWonder10 {
		want = `{"type":"adaptive","display":"omitted"}`
	}
	var maxTokens int64
	if json.Unmarshal(fields["max_tokens"], &maxTokens) != nil || maxTokens != modelprovider.ProfileOf(model).MaximumOutputTokens() {
		return errors.New("compatible request has the wrong output budget")
	}
	allowed[control] = true
	for key := range fields {
		if !allowed[key] {
			return errors.New("compatible request has an undocumented top-level field")
		}
	}
	if model.IsCustom() {
		if _, exists := fields["thinking"]; exists {
			return errors.New("custom request must not enable thinking")
		}
		return nil
	}
	var gotValue, wantValue any
	if json.Unmarshal(fields[control], &gotValue) != nil || json.Unmarshal([]byte(want), &wantValue) != nil || !reflect.DeepEqual(gotValue, wantValue) {
		return errors.New("compatible request has the wrong reasoning control")
	}
	var system []map[string]json.RawMessage
	if json.Unmarshal(fields["system"], &system) != nil || len(system) != 1 || len(system[0]) != 2 {
		return errors.New("compatible system prompt must not carry cache policy")
	}
	var tools []map[string]json.RawMessage
	if json.Unmarshal(fields["tools"], &tools) != nil {
		return errors.New("compatible tools must be an array")
	}
	for _, tool := range tools {
		for field := range tool {
			if field != "name" && field != "description" && field != "input_schema" {
				return errors.New("compatible tool carries a Claude-only option")
			}
		}
	}
	for _, message := range request.Messages {
		if message.Role != string(conversation.EntryRoleUser) && message.Role != string(conversation.EntryRoleAssistant) {
			return errors.New("compatible requests cannot send mid-conversation system messages")
		}
		for _, block := range message.Content {
			var content map[string]json.RawMessage
			if json.Unmarshal(block, &content) != nil {
				return errors.New("invalid compatible block")
			}
			if _, exists := content["cache_control"]; exists {
				return errors.New("compatible content must not carry cache policy")
			}
		}
	}
	return nil
}

// protocolFault belongs only to the mock model's scenario engine.
type protocolFault string

const (
	faultNone                protocolFault = ""
	faultCompatibleReasoning protocolFault = "compatible_readable_reasoning"
	faultPlaintext           protocolFault = "plaintext_reasoning"
	faultMissingOpaque       protocolFault = "missing_opaque"
	faultDuplicateOpaque     protocolFault = "duplicate_opaque"
	faultOversizeOpaque      protocolFault = "oversize_opaque"
	faultTruncated           protocolFault = "truncated_stream"
	faultWrongModel          protocolFault = "wrong_model"
)

var protocolFaults = []protocolFault{faultNone, faultCompatibleReasoning, faultPlaintext, faultMissingOpaque, faultDuplicateOpaque, faultOversizeOpaque, faultTruncated, faultWrongModel}

var quotedCustomRun = regexp.MustCompile(`^toolu_mock_([a-f0-9]{8}_[0-9]+)_[0-9]+_[0-9]+_[0-9]+$`)

// followCustomReplay verifies native or exactly quoted historical tool proposals.
func followCustomReplay(messages []requestMessage) (selection, error) {
	return walk(messages, func(message requestMessage, sel selection) error {
		expected, err := sel.nextReply()
		if err != nil {
			return err
		}
		expected = messagesReply(expected, conversation.ModelAnthropicCustom)
		projected := requestMessage{Content: make([]json.RawMessage, len(message.Content))}
		for i, raw := range message.Content {
			var text struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(raw, &text) == nil && text.Type == conversation.BlockTypeText.String() && strings.HasPrefix(text.Text, conversation.RecordedToolHistoryPrefix) {
				quoted := strings.TrimPrefix(text.Text, conversation.RecordedToolHistoryPrefix)
				var tool struct {
					Type string `json:"type"`
				}
				if json.Unmarshal([]byte(quoted), &tool) != nil || tool.Type != conversation.BlockTypeToolUse.String() {
					return errors.New("invalid quoted tool proposal")
				}
				var identity struct {
					ID string `json:"id"`
				}
				if json.Unmarshal([]byte(quoted), &identity) != nil {
					return errors.New("invalid historical tool identity")
				}
				source := quotedCustomRun.FindStringSubmatch(identity.ID)
				if len(source) != 2 {
					return errors.New("invalid historical scenario run")
				}
				// Text projection changes message grouping; preserve the original run encoded in the quoted ID.
				expected, err = sel.script.replyAt(sel.at, sel.arguments, source[1])
				if err != nil {
					return err
				}
				expected = messagesReply(expected, conversation.ModelAnthropicCustom)
				projected.Content[i] = json.RawMessage(quoted)
			} else {
				projected.Content[i] = raw
			}
		}
		if expected.failure != nil || !sameBlocks(projected.Content, expected.blocks) {
			detail := ""
			for i, raw := range projected.Content {
				if i >= len(expected.blocks) {
					break
				}
				var got, want map[string]any
				if json.Unmarshal(raw, &got) == nil && json.Unmarshal(expected.blocks[i].json(), &want) == nil && !reflect.DeepEqual(got, want) {
					detail = fmt.Sprintf("block %d: type=%v/%v id=%v/%v", i, got["type"], want["type"], got["id"], want["id"])
					break
				}
			}
			return fmt.Errorf("custom replay mismatch at %s (observed %d blocks, expected %d; %s)", sel.at, len(projected.Content), len(expected.blocks), detail)
		}
		return nil
	})
}
