// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package relay

import (
	"context"
	"errors"

	"github.com/superdurable/superversev2/cmd/dexai-connect/state"
)

// SubmissionMode states what a provider can prove after an ambiguous write.
type SubmissionMode struct{ name string }

var (
	// SubmissionJournal allows resubmission with the same operation ID; Orca
	// answers from its idempotent journal without inserting a second message.
	SubmissionJournal = SubmissionMode{name: "journal"}
	// SubmissionOneWrite allows exactly one write, followed only by a bounded
	// positive history lookup for the persisted marker.
	SubmissionOneWrite = SubmissionMode{name: "one_write"}
)

// Provider is the session and public-history boundary consumed by the relay.
// The binding's session and thread IDs are provider-owned opaque strings.
// A one-write implementation also satisfies OneWriteProvider. Its Send makes
// at most one provider write for a call and never retries inside the adapter;
// only a definite pre-admission refusal may return SubmissionRejected. Its
// Subscribe resumes from provider-owned positions and omits hidden reasoning.
type Provider interface {
	ProviderID() string
	ProviderName() string
	AgentLabel() string
	SessionChangedError() error
	Runtime(ctx context.Context) (Runtime, error)
	Send(ctx context.Context, request SendRequest) (SendResult, error)
	Cancel(ctx context.Context, sessionID, providerThreadID string) (state.EventPosition, error)
	Conversation(ctx context.Context, sessionID string) (Conversation, error)
	History(ctx context.Context, sessionID string, limit int) ([]HistoryMessage, error)
	Subscribe(ctx context.Context, sessionID, providerThreadID string, after state.EventPosition, handle func([]Event, state.EventPosition) error) error
	SubmissionMode() SubmissionMode
	// SubmittedMessageID is pure and stable for one operation. EventUserMessage
	// and EventTurnStarted use that ID to suppress the relay's own echo.
	SubmittedMessageID(operationID, marker string) string
	// EventAfter compares a provider event with the recorded generation boundary.
	EventAfter(position, boundary state.EventPosition) bool
}

// OrcaProviderID identifies records written before provider selection existed.
const OrcaProviderID = "orca"

// ErrProviderSessionChanged is the neutral detachment reason for direct
// providers. Orca retains ErrConversationChanged and its existing text.
var ErrProviderSessionChanged = errors.New("the provider session changed; reconnect from Studio")

// ErrUnsupportedProtocol stops a binding when a provider's answer cannot be
// interpreted safely. Retrying the binding would repeat the same ambiguity.
var ErrUnsupportedProtocol = errors.New("unsupported provider protocol answer")

func isSessionChanged(err error) bool {
	return errors.Is(err, ErrConversationChanged) || errors.Is(err, ErrProviderSessionChanged)
}

// OneWriteProvider proves delivery only from the exact marker in canonical
// public user history. The adapter must make this a read-only, bounded lookup
// and return only an exact marker match. Empty item ID means no proof.
// A read failure likewise never licenses another write.
type OneWriteProvider interface {
	Provider
	FindSubmittedMessage(ctx context.Context, sessionID, providerThreadID, marker string) (itemID string, err error)
}

// Runtime describes a provider instance and its local agent version. The
// relay maps these values to the retained bridgeapi wire field names.
type Runtime struct {
	ProviderVersion string
	AgentVersion    string
	InstanceID      string
	// Warning records a compatible but newer-than-verified local provider.
	Warning string
}

// Conversation is a session's current provider thread and latest turn.
type Conversation struct {
	ProviderThreadID string
	TurnID           string
	Turn             TurnState
}

// TurnState is what a provider proves of a session's latest turn.
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

// SendRequest is one submission to a provider session. ClientOperationID is
// stable across journal queries; Marker is stable across one-write history
// checks. InstanceID fences an Orca journal operation to its first runtime.
type SendRequest struct {
	SessionID         string
	ProviderThreadID  string
	Text              string
	ClientOperationID string
	InstanceID        string
	Marker            string
}

// SendResult is a provider's answer about a submission. ProviderItemID is
// the canonical public user item of a positively accepted message.
type SendResult struct {
	State          SubmissionState
	Reason         string
	ProviderItemID string
}

// SubmissionState is what a provider can prove of a submission.
type SubmissionState struct{ name string }

var (
	// SubmissionAccepted reached the provider's canonical user history.
	SubmissionAccepted = SubmissionState{name: "accepted"}
	// SubmissionPending is unsettled: query a journal or check exact history.
	SubmissionPending = SubmissionState{name: "pending"}
	// SubmissionRejected was definitely refused before admission, with a reason.
	SubmissionRejected = SubmissionState{name: "rejected"}
	// SubmissionUnknown has no positive delivery or rejection proof.
	SubmissionUnknown = SubmissionState{name: "unknown"}
)

// String is the state's spelling.
func (state SubmissionState) String() string { return state.name }

// Event is one event of a session. Position is where the event's item was
// first written; TurnID names the turn of every turn event and message;
// MessageID is a message's item, or the message that opened a turn.
type Event struct {
	Position  state.EventPosition
	Kind      EventKind
	TurnID    string
	MessageID string
	Text      string
}

// EventKind is what a session event is.
type EventKind struct{ name string }

var (
	// EventTurnStarted is a provider turn starting.
	EventTurnStarted = EventKind{name: "turn_started"}
	// EventTurnCompleted is a turn ending, completed or cancelled.
	EventTurnCompleted = EventKind{name: "turn_completed"}
	// EventUserMessage is a public user message, typed locally or delivered.
	EventUserMessage = EventKind{name: "user_message"}
	// EventAssistantMessage is a completed assistant message.
	EventAssistantMessage = EventKind{name: "assistant_message"}
	// EventActivity is a short public summary of agent activity: a subagent
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
