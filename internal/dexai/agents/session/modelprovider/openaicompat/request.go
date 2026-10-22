// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package openaicompat

import (
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// canonicalBlock is the allowlisted provider-neutral content stored by the harness.
type canonicalBlock struct {
	Type      string                `json:"type"`
	Text      string                `json:"text"`
	ID        conversation.CallID   `json:"id"`
	Name      conversation.ToolName `json:"name"`
	Input     json.RawMessage       `json:"input"`
	ToolUseID conversation.CallID   `json:"tool_use_id"`
	Content   json.RawMessage       `json:"content"`
	IsError   bool                  `json:"is_error"`
}

type function struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      *bool           `json:"strict,omitempty"`
}

type functionTool struct {
	Type        string          `json:"type"`
	Function    *function       `json:"function,omitempty"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// convertTools converts frozen Anthropic-shaped schemas without changing their bytes in state.
func convertTools(request conversation.ModelRequest, policy Policy) ([]functionTool, error) {
	tools := make([]functionTool, 0, len(request.Prompts.Tools))
	for _, raw := range request.Prompts.Tools {
		var frozen struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Schema      json.RawMessage `json:"input_schema"`
		}
		if json.Unmarshal(raw, &frozen) != nil || frozen.Name == "" || !json.Valid(frozen.Schema) {
			return nil, rejected(failureInvalidToolSchema)
		}
		f := function{Name: frozen.Name, Description: frozen.Description, Parameters: frozen.Schema}
		if policy.Strict {
			enabled := true
			f.Strict = &enabled
		}
		tool := functionTool{Type: ItemFunction.String(), Function: &f}
		if policy.WireMode == conversation.OpenAIResponses {
			tool.Function = nil
			tool.Name = f.Name
			tool.Description = f.Description
			tool.Parameters = f.Parameters
			tool.Strict = f.Strict
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func encodeRequest(request conversation.ModelRequest, policy Policy) ([]byte, error) {
	tools, err := convertTools(request, policy)
	if err != nil {
		return nil, err
	}
	params := map[string]any{"model": request.APIModelID(), "stream": true, "tools": tools, "tool_choice": "auto"}
	if request.ToolChoiceNone {
		params["tool_choice"] = "none"
	}
	if request.Model.IsCustom() {
		params["parallel_tool_calls"] = false
	}
	responses := policy.WireMode == conversation.OpenAIResponses
	if responses {
		params["store"] = false
		if policy.Replay == conversation.ReplayEncryptedOpaque {
			params["include"] = []string{"reasoning.encrypted_content"}
		}
		params["max_output_tokens"] = policy.MaxTokens
		params["instructions"] = request.Prompts.System
		if effort := requestEffort(request, policy); effort != "" {
			params["reasoning"] = map[string]string{"effort": string(effort)}
		}
	} else {
		params["max_tokens"] = policy.MaxTokens
		params["stream_options"] = map[string]bool{"include_usage": true}
		switch policy.ChatThinking {
		case conversation.ChatThinkingDisabled:
			params["thinking"] = map[string]string{"type": "disabled"}
		case conversation.ChatThinkingQwenDisabled:
			params["enable_thinking"] = false
			params["preserve_thinking"] = false
		}
	}
	var messages []any
	if !responses {
		messages = append(messages, map[string]any{"role": "system", "content": request.Prompts.System})
	}
	limitedCalls := map[conversation.CallID]conversation.ToolName{}
	for _, message := range request.Messages {
		message = outputLimitProjection(message, limitedCalls)
		converted, err := convertMessage(message, policy)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted...)
	}
	if responses {
		params["input"] = messages
	} else {
		params["messages"] = messages
	}
	return json.Marshal(params)
}

func convertMessage(message conversation.RequestMessage, policy Policy) ([]any, error) {
	responses := policy.WireMode == conversation.OpenAIResponses
	if !responses && message.Role == conversation.EntryRoleSystem {
		blocks := make([]json.RawMessage, 0, len(message.Blocks))
		for _, raw := range message.Blocks {
			var block canonicalBlock
			if json.Unmarshal(raw, &block) != nil || block.Type != conversation.BlockTypeText.String() {
				return nil, rejected(failureUnsupportedHistoryBlock)
			}
			blocks = append(blocks, textBlock("<system-reminder>\n"+block.Text+"\n</system-reminder>"))
		}
		message.Role = conversation.EntryRoleUser
		message.Blocks = blocks
	}
	var result, content []any
	var calls []any
	replayIdentities := map[string]bool{}
	replayBytes := 0
	replayed, proposed := false, false
	flush := func() {
		if len(content) == 0 && len(calls) == 0 {
			return
		}
		item := map[string]any{"role": message.Role, "content": content}
		if len(calls) > 0 {
			item["tool_calls"] = calls
		}
		result = append(result, item)
		content = nil
		calls = nil
	}
	for position, raw := range message.Blocks {
		var block canonicalBlock
		if json.Unmarshal(raw, &block) != nil {
			return nil, rejected(failureInvalidHistory)
		}
		switch block.Type {
		case conversation.BlockTypeText.String():
			kind := "text"
			if responses {
				kind = ItemInputText.String()
				if message.Role == conversation.EntryRoleAssistant {
					kind = ItemOutputText.String()
				}
			}
			content = append(content, map[string]string{"type": kind, "text": block.Text})
		case conversation.BlockTypeToolUse.String():
			proposed = true
			if responses {
				flush()
				result = append(result, map[string]any{"type": ItemFunctionCall.String(), "call_id": block.ID, "name": block.Name, "arguments": string(block.Input)})
			} else {
				call := map[string]any{"id": block.ID, "type": ItemFunction.String(), ItemFunction.String(): map[string]string{"name": string(block.Name), "arguments": string(block.Input)}}
				// The immediately preceding replay block binds this tool call and position.
				if position > 0 && conversation.IsOpaqueReplay(message.Blocks[position-1]) {
					replay, err := conversation.ReadOpaqueReplay(message.Blocks[position-1], policy.Provider, policy.WireMode, position-1)
					if err != nil || replay.CallID != block.ID {
						return nil, rejected(failureInvalidReplay)
					}
					call["extra_content"] = map[string]any{"google": map[string]string{"thought_signature": replay.Ciphertext}}
				} else if policy.Replay == conversation.ReplayEncryptedOpaque && len(calls) == 0 {
					return nil, rejected(failureMissingReplay)
				}
				calls = append(calls, call)
			}
		case conversation.BlockTypeToolResult.String():
			flush()
			var text string
			if json.Unmarshal(block.Content, &text) != nil {
				return nil, rejected(failureInvalidToolResult)
			}
			text = string(comm.Must(json.Marshal(ToolOutput{Content: text, IsError: block.IsError})))
			if responses {
				result = append(result, map[string]any{"type": ItemFunctionOutput.String(), "call_id": block.ToolUseID, "output": text})
			} else {
				result = append(result, map[string]any{"role": "tool", "tool_call_id": block.ToolUseID, "content": text})
			}
		case conversation.BlockTypeImage.String():
			image, err := imageContent(raw, policy)
			if err != nil {
				return nil, err
			}
			content = append(content, image)
		case conversation.BlockTypeOpaqueReplay.String():
			replayed = true
			replay, err := conversation.ReadOpaqueReplay(raw, policy.Provider, policy.WireMode, position)
			if err != nil || policy.Replay != conversation.ReplayEncryptedOpaque || message.Role != conversation.EntryRoleAssistant {
				return nil, rejected(failureInvalidReplay)
			}
			identity := replay.ItemID
			if !responses {
				identity = string(replay.CallID)
			}
			replayBytes += len(raw)
			if replayIdentities[identity] || replayBytes > conversation.MaximumOpaqueReplayBytes {
				return nil, rejected(failureInvalidReplay)
			}
			replayIdentities[identity] = true
			if !responses {
				if position+1 >= len(message.Blocks) {
					return nil, rejected(failureInvalidReplay)
				}
				var next canonicalBlock
				if json.Unmarshal(message.Blocks[position+1], &next) != nil || next.Type != conversation.BlockTypeToolUse.String() || next.ID != replay.CallID {
					return nil, rejected(failureInvalidReplay)
				}
			}
			if responses {
				flush()
				result = append(result, map[string]any{"type": ItemReasoning.String(), "id": replay.ItemID, "status": replay.Status, "summary": []any{}, "encrypted_content": replay.Ciphertext})
			}
		default:
			return nil, rejected(failureUnsupportedHistoryBlock)
		}
	}
	if responses && policy.Replay == conversation.ReplayEncryptedOpaque && proposed && !replayed {
		return nil, rejected(failureMissingReplay)
	}
	flush()
	return result, nil
}

func imageContent(raw json.RawMessage, policy Policy) (any, error) {
	var block struct {
		Source struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		} `json:"source"`
	}
	if !policy.Images || json.Unmarshal(raw, &block) != nil || block.Source.Type != "base64" {
		return nil, rejected(failureUnsupportedImage)
	}
	switch block.Source.MediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return nil, rejected(failureUnsupportedImage)
	}
	decoded, err := base64.StdEncoding.DecodeString(block.Source.Data)
	if err != nil || len(decoded) == 0 || len(decoded) > 20<<20 {
		return nil, rejected(failureUnsupportedImage)
	}
	data := "data:" + block.Source.MediaType + ";base64," + block.Source.Data
	if policy.WireMode == conversation.OpenAIResponses {
		return map[string]string{"type": ItemInputImage.String(), "image_url": data}, nil
	}
	return map[string]any{"type": ItemImageURL.String(), "image_url": map[string]string{"url": data}}, nil
}

func textBlock(text string) json.RawMessage {
	return comm.Must(json.Marshal(map[string]string{"type": conversation.BlockTypeText.String(), "text": text}))
}

// completeCall admits only a declared tool name with a complete JSON object.
func completeCall(id, name, arguments string, request conversation.ModelRequest) (conversation.ToolCall, json.RawMessage, error) {
	validID := id != "" && len(id) <= 256
	for _, character := range id {
		validID = validID && (character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_')
	}
	if request.ToolChoiceNone || !validID || strings.TrimSpace(name) == "" {
		return conversation.ToolCall{}, nil, rejected(failureInvalidToolCall)
	}
	offered := false
	for _, raw := range request.Prompts.Tools {
		var tool struct {
			Name string `json:"name"`
		}
		comm.MustDo(json.Unmarshal(raw, &tool))
		offered = offered || tool.Name == name
	}
	if !offered {
		return conversation.ToolCall{}, nil, rejected(failureUnknownTool)
	}
	input, err := conversation.ParseJSONObject(arguments)
	if err != nil {
		return conversation.ToolCall{}, nil, rejected(failureInvalidToolArguments)
	}
	call := conversation.ToolCall{ID: conversation.CallID(id), Name: conversation.ToolName(name), Arguments: input}
	block := comm.Must(json.Marshal(map[string]any{"type": conversation.BlockTypeToolUse.String(), "id": call.ID, "name": call.Name, "input": input}))
	return call, block, nil
}

// ToolOutput is the lossless textual result envelope sent on compatible protocols,
// whose function output strings have no native Anthropic is_error field.
type ToolOutput struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// requestEffort collapses accepted mid-conversation effort notices into the
// current Responses call's global control. Non-thinking profiles send none.
func requestEffort(request conversation.ModelRequest, policy Policy) conversation.Effort {
	if !policy.Effort {
		return ""
	}
	effort := request.Effort
	for _, message := range request.Messages {
		if message.Effort != "" {
			effort = message.Effort
		}
	}
	return effort
}

// outputLimitProjection quotes only proposals that never executed and their
// matching results. Canonical history and all ordinary replay remain untouched.
func outputLimitProjection(message conversation.RequestMessage, calls map[conversation.CallID]conversation.ToolName) conversation.RequestMessage {
	limited := message.Role == conversation.EntryRoleAssistant && message.ModelStopReason == conversation.StopReasonMaxTokens
	blocks := make([]json.RawMessage, 0, len(message.Blocks))
	for _, raw := range message.Blocks {
		var block canonicalBlock
		if json.Unmarshal(raw, &block) != nil {
			blocks = append(blocks, raw)
			continue
		}
		if limited {
			switch block.Type {
			case conversation.BlockTypeOpaqueReplay.String():
				continue
			case conversation.BlockTypeToolUse.String():
				calls[block.ID] = block.Name
				blocks = append(blocks, textBlock("Tool "+string(block.Name)+": not executed (model output limit)."))
				continue
			}
		}
		if name, exists := calls[block.ToolUseID]; exists && message.Role == conversation.EntryRoleUser && block.Type == conversation.BlockTypeToolResult.String() && block.IsError {
			blocks = append(blocks, textBlock("Tool "+string(name)+": not executed (model output limit)."))
			delete(calls, block.ToolUseID)
			continue
		}
		blocks = append(blocks, raw)
	}
	message.Blocks = blocks
	return message
}
