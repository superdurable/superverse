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
	"strings"

	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

// maximumFailureDetailRunes bounds the part of a failed tool result a stopped
// scenario quotes.
const maximumFailureDetailRunes = 200

// failedTool reads the user message that answers the tool calls of the
// assistant message before it. It returns the first call whose result is an
// error, with its tool name and the text of its result, or false when every
// result succeeded.
func failedTool(assistant, answer requestMessage) (tool, result string, failed bool) {
	names := map[string]string{}
	for _, raw := range assistant.Content {
		var block struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &block) == nil && block.Type == conversation.BlockTypeToolUse.String() {
			names[block.ID] = block.Name
		}
	}
	for _, raw := range answer.Content {
		var block struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			IsError   bool            `json:"is_error"`
			Content   json.RawMessage `json:"content"`
		}
		if json.Unmarshal(raw, &block) != nil || block.Type != conversation.BlockTypeToolResult.String() || !block.IsError {
			continue
		}
		if name, answers := names[block.ToolUseID]; answers {
			return name, resultText(block.Content), true
		}
	}
	return "", "", false
}

// allResults joins the text of every tool result in answer that answers a
// tool call of assistant, failed or not.
func allResults(assistant, answer requestMessage) string {
	calls := map[string]bool{}
	for _, raw := range assistant.Content {
		var block struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(raw, &block) == nil && block.Type == conversation.BlockTypeToolUse.String() {
			calls[block.ID] = true
		}
	}
	var results []string
	for _, raw := range answer.Content {
		var block struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
		}
		if json.Unmarshal(raw, &block) == nil && block.Type == conversation.BlockTypeToolResult.String() && calls[block.ToolUseID] {
			results = append(results, resultText(block.Content))
		}
	}
	return strings.Join(results, "\n")
}

// resultText is the text of a tool_result content, a string or text blocks.
func resultText(content json.RawMessage) string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return text
	}
	var blocks []textJSON
	var parts []string
	if json.Unmarshal(content, &blocks) == nil {
		for _, block := range blocks {
			if block.Type == conversation.BlockTypeText.String() {
				parts = append(parts, block.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// firstLine is the first line of text, cut to maximumFailureDetailRunes.
func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	if runes := []rune(line); len(runes) > maximumFailureDetailRunes {
		line = string(runes[:maximumFailureDetailRunes])
	}
	return line
}

// stopText is what a scenario answers with from the first failed tool of its
// script on, so it never claims work its tools did not finish.
func stopText(scenario string, at position, tool, detail string) string {
	return fmt.Sprintf("Mock scenario %s stopped at %s: %s failed: %s", scenario, at, tool, detail)
}
