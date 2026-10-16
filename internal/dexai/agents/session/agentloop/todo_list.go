// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agentloop

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
)

// TodoItem is one item of the Agent's progress list.
type TodoItem struct {
	Content string                  `json:"content"`
	Status  conversation.TaskStatus `json:"status"`
}

// TodoList is the Agent's own progress list. Each write_todos call replaces it
// as a whole; it has no draft, execution or approval state and is never an
// approval gate.
type TodoList struct {
	Revision int64      `json:"revision"`
	Items    []TodoItem `json:"items"`
}

const MaximumProgressJSONBytes = 256 << 10

var errBlankTodoContent = errors.New("every todo item needs non-blank content")

// TodoItems maps write_todos arguments to progress items. The schema bounds
// the statuses; blank content is the one rule it cannot express.
func TodoItems(call conversation.ToolCall) ([]TodoItem, error) {
	input, err := toolinput.WriteTodos.Decode(call.Arguments.String())
	if err != nil {
		return nil, invalidToolArguments(call, err)
	}
	items := make([]TodoItem, 0, len(input.Todos))
	for _, todo := range input.Todos {
		content := strings.TrimSpace(todo.Content)
		if content == "" {
			return nil, invalidToolArguments(call, errBlankTodoContent)
		}
		items = append(items, TodoItem{Content: content, Status: conversation.TaskStatus(todo.Status)})
	}
	encoded, err := json.Marshal(TodoList{Revision: int64(^uint64(0) >> 1), Items: items})
	if err != nil || len(encoded) > MaximumProgressJSONBytes {
		return nil, invalidToolArguments(call, errors.New("the todo list exceeds 256 KiB of encoded state"))
	}
	return items, nil
}

// NextTodoList is the list a write_todos call writes after previous. An empty
// list clears the items and keeps the revision counting.
func NextTodoList(previous *TodoList, items []TodoItem) TodoList {
	revision := int64(1)
	if previous != nil {
		revision = previous.Revision + 1
	}
	return TodoList{Revision: revision, Items: items}
}

// todoListText renders the list for a segment_start entry: one line per item
// with its status, or "" when there is none.
func todoListText(list *TodoList) string {
	if list == nil {
		return ""
	}
	var text strings.Builder
	for _, item := range list.Items {
		text.WriteString("- [" + string(item.Status) + "] " + item.Content + "\n")
	}
	return strings.TrimSuffix(text.String(), "\n")
}
