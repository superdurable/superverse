// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

// Package relay runs the delivery, tool call and event loops of each binding
// between the remote agent endpoint and an Orca session.
package relay

import (
	"context"
	"errors"
	"time"

	"github.com/superdurable/superversev2/cmd/superverse-bridge/state"
)

// Orca is what the relay needs of the Orca runtime's structured session
// protocol. An implementation reaches only these methods (a compiled
// allow-list), refuses unsupported Orca and Codex versions, and never sends
// the Orca credential anywhere.
type Orca interface {
	// Runtime returns the Orca and Codex versions and the runtime ID, which
	// changes when Orca restarts.
	Runtime(ctx context.Context) (Runtime, error)
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
	// Send submits a message with the client operation ID; a repeated ID asks
	// Orca for the state of the earlier submission instead of sending again.
	// It starts a turn or steers the running one. It sends nothing unless the
	// session holds the request's provider thread, and fails with
	// ErrConversationChanged when it holds another before or after the send.
	Send(ctx context.Context, request SendRequest) (SendResult, error)
	// Cancel cancels the session's latest turn, named by its turn ID so Orca
	// cancels no other, unless it is positively over, and returns once it is,
	// with the session's event position then. It fails with
	// ErrConversationChanged when the session holds another provider thread.
	Cancel(ctx context.Context, sessionID, providerThreadID string) (state.Position, error)
	// Conversation reads the session's current provider thread and latest
	// turn.
	Conversation(ctx context.Context, sessionID string) (Conversation, error)
	// History returns at most limit public user and assistant messages of the
	// session, newest last.
	History(ctx context.Context, sessionID string, limit int) ([]HistoryMessage, error)
	// Subscribe calls handle with the session's events after position after,
	// each once, in batches with the position to resume from, until ctx ends
	// or handle fails. A zero position starts at the current end. A page of
	// another provider thread ends it with ErrConversationChanged.
	Subscribe(ctx context.Context, sessionID, providerThreadID string, after state.Position, handle func(events []Event, next state.Position) error) error
}

// ErrConversationChanged is an Orca session that now holds another provider
// thread than the binding's: the relay serves and changes nothing until the
// Builder reconnects.
var ErrConversationChanged = errors.New("the Orca conversation changed; reconnect from Studio")

// Runtime describes the running Orca.
type Runtime struct {
	OrcaVersion  string
	CodexVersion string
	RuntimeID    string
}

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

// Conversation is a session's current provider thread and latest turn.
type Conversation struct {
	ProviderThreadID string
	TurnID           string
	Turn             TurnState
}

// TurnState is what Orca proves of a session's latest turn.
type TurnState struct{ name string }

var (
	// TurnIdle is a latest turn that completed or was interrupted, or none.
	TurnIdle = TurnState{name: "idle"}
	// TurnRunning is a latest turn that runs.
	TurnRunning = TurnState{name: "running"}
	// TurnUnknown is any other state, such as unverifiable: it may run.
	TurnUnknown = TurnState{name: "unknown"}
)

// String is the state's spelling.
func (state TurnState) String() string { return state.name }

// SendRequest is one submission to a session: a user message of one text
// block, identified by its client operation ID, a 13-digit millisecond
// timestamp, a hyphen and 32 random hex digits. RuntimeID is the runtime of
// the first submission: Send never submits to another one and answers
// SubmissionUnknown instead. ProviderThreadID is the binding's thread.
type SendRequest struct {
	SessionID         string
	ProviderThreadID  string
	Text              string
	ClientOperationID string
	RuntimeID         string
}

// SendResult is Orca's answer about a submission. ProviderItemID is the
// Codex item an accepted message became.
type SendResult struct {
	State          SubmissionState
	Reason         string
	ProviderItemID string
}

// SentMessageID is the Orca message ID of a submission: its operation ID in
// Orca's journal.
func SentMessageID(clientOperationID string) string { return "orca:" + clientOperationID }

// SubmissionState is what Orca knows of a submission.
type SubmissionState struct{ name string }

var (
	// SubmissionAccepted reached Codex.
	SubmissionAccepted = SubmissionState{name: "accepted"}
	// SubmissionPending is not settled yet; ask again with the same ID.
	SubmissionPending = SubmissionState{name: "pending"}
	// SubmissionRejected was refused by Orca, with a reason.
	SubmissionRejected = SubmissionState{name: "rejected"}
	// SubmissionUnknown is a submission Orca cannot account for.
	SubmissionUnknown = SubmissionState{name: "unknown"}
)

// String is the state's spelling in Orca's protocol.
func (state SubmissionState) String() string { return state.name }

// ParseSubmissionState reads a state Orca spelled; any other spelling is unknown.
func ParseSubmissionState(name string) SubmissionState {
	for _, state := range []SubmissionState{SubmissionAccepted, SubmissionPending, SubmissionRejected} {
		if state.name == name {
			return state
		}
	}
	return SubmissionUnknown
}

// Event is one event of a session. Position is where the event's item was
// first written; TurnID names the turn of every turn event and message;
// MessageID is a message's item, or the message that opened a turn.
type Event struct {
	Position  state.Position
	Kind      EventKind
	TurnID    string
	MessageID string
	Text      string
}

// EventKind is what a session event is.
type EventKind struct{ name string }

var (
	// EventTurnStarted is a Codex turn starting.
	EventTurnStarted = EventKind{name: "turn_started"}
	// EventTurnCompleted is a turn ending, completed or cancelled.
	EventTurnCompleted = EventKind{name: "turn_completed"}
	// EventUserMessage is a user message, typed in Orca or delivered.
	EventUserMessage = EventKind{name: "user_message"}
	// EventAssistantMessage is a completed assistant message.
	EventAssistantMessage = EventKind{name: "assistant_message"}
	// EventActivity is a short public summary of Codex activity: a subagent
	// starting or finishing, a plan update, a local command.
	EventActivity = EventKind{name: "activity"}
)

// String is the kind's spelling.
func (kind EventKind) String() string { return kind.name }

// HistoryMessage is one public message of a session's history.
type HistoryMessage struct {
	ID   string
	Role HistoryRole
	Text string
}

// HistoryRole is who wrote a history message.
type HistoryRole struct{ name string }

var (
	HistoryRoleUser      = HistoryRole{name: "user"}
	HistoryRoleAssistant = HistoryRole{name: "assistant"}
)

// String is the role's spelling.
func (role HistoryRole) String() string { return role.name }
