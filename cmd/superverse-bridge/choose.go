// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/superversev2/cmd/superverse-bridge/relay"
	"github.com/superdurable/superversev2/cmd/superverse-bridge/state"
)

// chooseConversation lists Orca's Codex conversations on the terminal and
// reads the Builder's choice: an Orca session ID, or empty for a new
// conversation. Only a structured conversation that no binding
// holds can be chosen; a new conversation is offered last, and only with a
// worktree.
func chooseConversation(ctx context.Context, orca relay.Orca, dir *state.Dir, worktree string) (string, error) {
	conversations, err := orca.OpenConversations(ctx)
	if err != nil {
		return "", fmt.Errorf("list Orca's Codex conversations: %w", err)
	}
	bindings, err := dir.Bindings()
	if err != nil {
		return "", err
	}
	boundTo := map[string]string{}
	for _, binding := range bindings {
		boundTo[binding.Record.OrcaSessionID] = binding.Record.ProjectName
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("connect lists the conversations on a terminal; pass --session <id> or --new --worktree <selector>: %w", err)
	}
	defer func() { _ = tty.Close() }()
	var choices []string
	fmt.Fprintln(tty, "Codex conversations in Orca:")
	for _, conversation := range conversations {
		label := conversationLabel(conversation)
		switch project, bound := boundTo[conversation.SessionID]; {
		case !conversation.Structured:
			fmt.Fprintf(tty, "   -  %s (runs in a terminal and cannot be connected: two processes would write one thread)\n", label)
		case conversation.ReadError != nil:
			fmt.Fprintf(tty, "   -  %s (unavailable: %v)\n", label, conversation.ReadError)
		case bound:
			fmt.Fprintf(tty, "   -  %s (already connected to %s)\n", label, project)
		default:
			choices = append(choices, conversation.SessionID)
			fmt.Fprintf(tty, "  %2d) %s\n", len(choices), label)
		}
	}
	if worktree != "" {
		choices = append(choices, "")
		fmt.Fprintf(tty, "  %2d) New conversation in %s\n", len(choices), worktree)
	} else {
		fmt.Fprintln(tty, "   -  New conversation: run connect again with --worktree id:<worktreeId> or path:<dir>")
	}
	if len(choices) == 0 {
		return "", errors.New("no Codex conversation in Orca can be connected; start a Codex chat in Orca " +
			"(a new Codex agent opens as a chat when Settings, Experimental, Chat UI is on, as it is once Orca holds a chat), " +
			"or a new conversation with --worktree")
	}
	return readChoice(tty, choices)
}

func readChoice(tty io.ReadWriter, choices []string) (string, error) {
	reader := bufio.NewReader(tty)
	for {
		fmt.Fprintf(tty, "Connect which conversation [1-%d]: ", len(choices))
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("read the choice: %w", err)
		}
		if index, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && index >= 1 && index <= len(choices) {
			return choices[index-1], nil
		}
		fmt.Fprintf(tty, "Enter a number from 1 to %d.\n", len(choices))
	}
}

// conversationLabel is a conversation's title, worktree directory, last
// activity and Orca session ID.
func conversationLabel(conversation relay.OpenConversation) string {
	parts := []string{conversation.Title}
	if conversation.Title == "" {
		parts[0] = "Untitled"
	}
	parts = append(parts, conversation.Path)
	if !conversation.LastActivity.IsZero() {
		parts = append(parts, "active "+sinceLabel(time.Since(conversation.LastActivity)))
	}
	if conversation.SessionID != "" {
		parts = append(parts, conversation.SessionID)
	}
	return strings.Join(parts, " · ")
}

func sinceLabel(elapsed time.Duration) string {
	switch {
	case elapsed < time.Minute:
		return "just now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%d min ago", int(elapsed/time.Minute))
	case elapsed < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(elapsed/time.Hour))
	default:
		return fmt.Sprintf("%d days ago", int(elapsed/(24*time.Hour)))
	}
}
