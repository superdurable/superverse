// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package relay runs the delivery, tool call and event loops of each binding
// between the remote agent endpoint and a local agent provider.
package relay

import (
	"context"
	"errors"
	"time"
)

// Orca is what the relay needs of the Orca runtime's structured session
// protocol. An implementation reaches only these methods (a compiled
// allow-list), refuses unsupported Orca and Codex versions, and never sends
// the Orca credential anywhere.
type Orca interface {
	Provider
	// OpenConversations lists the Codex conversations open in Orca: the
	// structured ones, which can be bound once they hold a provider thread,
	// and those in a terminal, which cannot.
	OpenConversations(ctx context.Context) ([]OpenConversation, error)
	// OpenWorktrees lists the open Orca worktrees where a new structured
	// conversation can be created.
	OpenWorktrees(ctx context.Context) ([]OpenWorktree, error)
	// ResolveWorktree checks a new conversation's worktree selector
	// (id:<worktreeId> or path:<dir>) and returns the one to create it in.
	ResolveWorktree(ctx context.Context, selector string) (string, error)
	// CreateSession creates a new structured Codex conversation in a worktree
	// that ResolveWorktree returned.
	CreateSession(ctx context.Context, worktree string) (Session, error)
	// ResumeSession confirms that a session is a structured Codex conversation,
	// waits until it is idle and returns it with its provider thread.
	ResumeSession(ctx context.Context, sessionID string) (Session, error)
	// Reload stops the idle session's Codex process, so that the next send
	// starts one that resumes the same provider thread with the MCP servers
	// configured then, and shows the chat in Orca. It fails when the session
	// holds another thread, none, or a running turn.
	Reload(ctx context.Context, sessionID, providerThreadID string) error
}

// ErrConversationChanged is an Orca session that now holds another provider
// thread than the binding's: the relay serves and changes nothing until the
// Builder reconnects.
var ErrConversationChanged = errors.New("the Orca conversation changed; reconnect from Studio")

// Session is a structured Codex conversation in Orca.
type Session struct {
	ID               string
	ProviderThreadID string
}

// OpenConversation is a Codex conversation open in an Orca tab. A structured
// chat can be bound only once Orca has given it a session ID and a confirmed
// provider thread. A terminal cannot be bound because two processes would
// write one thread.
type OpenConversation struct {
	SessionID           string
	Structured          bool
	ProviderThreadReady bool
	// Worktree is Orca's worktree ID; Path is its directory.
	Worktree string
	Path     string
	Title    string
	// LastActivity is zero when Orca records none.
	LastActivity time.Time
	// ReadError is why a structured conversation's journal could not be read;
	// such a conversation cannot be bound.
	ReadError error
}

// OpenWorktree is an Orca worktree from the tabs inventory.
type OpenWorktree struct {
	Selector string
	Path     string
}

// SentMessageID is the Orca message ID of a submission: its operation ID in
// Orca's journal.
func SentMessageID(clientOperationID string) string { return "orca:" + clientOperationID }

// ParseSubmissionState reads a state Orca spelled; any other spelling is unknown.
func ParseSubmissionState(name string) SubmissionState {
	for _, state := range []SubmissionState{SubmissionAccepted, SubmissionPending, SubmissionRejected} {
		if state.name == name {
			return state
		}
	}
	return SubmissionUnknown
}
