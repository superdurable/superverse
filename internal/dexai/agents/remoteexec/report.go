// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteexec

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Report bounds apply before any admission watermark or Channel publication.
const (
	inboxCapacity         = 8
	maximumInboxBytes     = 2 << 20
	maximumEvents         = 32
	maximumEventBytes     = 64 << 10
	maximumArgumentsBytes = 256 << 10
)

// EventKind is the closed set of public facts a remote executor may report.
type EventKind string

const (
	EventAssistantMessage  EventKind = "assistant_message"
	EventUserMessage       EventKind = "user_message"
	EventImportedMessage   EventKind = "imported_message"
	EventActivity          EventKind = "activity"
	EventDeliveryUncertain EventKind = "delivery_uncertain"
	EventDeliveryFailed    EventKind = "delivery_failed"
)

func (kind EventKind) validate() error {
	switch kind {
	case EventAssistantMessage, EventUserMessage, EventImportedMessage, EventActivity, EventDeliveryUncertain, EventDeliveryFailed:
		return nil
	default:
		return conversation.NewEnumValidationError("remote event", string(kind))
	}
}

func (kind *EventKind) UnmarshalJSON(data []byte) error {
	return conversation.DecodeEnum(data, kind, EventKind.validate)
}

// RemoteEvent contains public text only; no reasoning block can be represented.
type RemoteEvent struct {
	Kind          EventKind              `json:"kind"`
	Text          string                 `json:"text,omitempty"`
	Role          conversation.EntryRole `json:"role,omitempty"`
	EntrySequence conversation.Sequence  `json:"entry_sequence,omitempty"`
}

// RemoteToolCall is an external request; its stable ID is assigned by the Session.
type RemoteToolCall struct {
	Name      conversation.ToolName   `json:"name"`
	Arguments conversation.JSONObject `json:"arguments"`
}

// RemoteReport is the durable inbox payload. ConnectionID is authenticated by
// the endpoint, never selected from the remote request body.
type RemoteReport struct {
	ConnectionID  string             `json:"connection_id"`
	Owner         conversation.Actor `json:"owner"`
	Epoch         uint64             `json:"epoch"`
	Generation    uint64             `json:"generation"`
	Sequence      uint64             `json:"sequence"`
	ProviderTurn  string             `json:"provider_turn,omitempty"`
	Events        []RemoteEvent      `json:"events,omitempty"`
	ToolCall      *RemoteToolCall    `json:"tool_call,omitempty"`
	TurnCompleted bool               `json:"turn_completed,omitempty"`
}

// Validate rejects malformed reports and bounds their individual fields. The
// HTTP endpoint bounds the original body; admission bounds enriched inbox data.
func (report RemoteReport) Validate() error {
	parts := 0
	if report.Events != nil {
		if len(report.Events) == 0 {
			return errors.New("an events report must contain at least one event")
		}
		parts++
	}
	if report.ToolCall != nil {
		parts++
	}
	if report.TurnCompleted {
		parts++
	}
	if parts != 1 {
		return errors.New("a report must contain exactly one of events, toolCall or turnCompleted")
	}
	if report.Sequence == 0 {
		return errors.New("report sequence must be positive")
	}
	if len(report.ProviderTurn) > 256 || !utf8.ValidString(report.ProviderTurn) {
		return errors.New("provider turn must be at most 256 UTF-8 bytes")
	}
	if len(report.Events) > maximumEvents {
		return errors.New("a report has at most 32 events")
	}
	for _, event := range report.Events {
		if err := event.Kind.validate(); err != nil {
			return err
		}
		if len(event.Text) > maximumEventBytes || !utf8.ValidString(event.Text) {
			return errors.New("event text must contain at most 65536 UTF-8 bytes")
		}
		switch event.Kind {
		case EventDeliveryUncertain, EventDeliveryFailed:
			if event.EntrySequence <= 0 {
				return errors.New("delivery events must reference an entry sequence")
			}
		default:
			if len(event.Text) == 0 {
				return errors.New("message and activity events require text")
			}
			if event.EntrySequence != 0 {
				return errors.New("only delivery events reference an entry sequence")
			}
		}
		if event.Kind == EventImportedMessage {
			if event.Role != conversation.EntryRoleUser && event.Role != conversation.EntryRoleAssistant {
				return errors.New("imported messages require user or assistant role")
			}
		} else if event.Role != "" {
			return errors.New("only imported messages carry a role")
		}
	}
	if call := report.ToolCall; call != nil {
		if len(call.Name) == 0 || len(call.Name) > 64 {
			return errors.New("tool name must contain 1 to 64 bytes")
		}
		if len(call.Arguments) > maximumArgumentsBytes {
			return errors.New("tool arguments exceed 256 KiB")
		}
		if _, err := conversation.ParseJSONObject(call.Arguments.String()); err != nil {
			return fmt.Errorf("tool arguments: %w", err)
		}
	}
	return nil
}

// ReportAnswer is the authoritative admission watermark; reads never advance it.
type ReportAnswer struct {
	Epoch      uint64 `json:"epoch"`
	Generation uint64 `json:"generation"`
	Reported   uint64 `json:"reported"`
	Duplicate  bool   `json:"duplicate"`
}

// ReportRequest binds the report to its authenticated Project before RPC admission.
type ReportRequest struct {
	ProjectID string       `json:"project_id"`
	Report    RemoteReport `json:"report"`
}

// executorChangedError marks a local admission refusal; the RPC adds its current fence.
type executorChangedError struct{}

func (err *executorChangedError) Error() string { return "executor_changed" }

// SequenceGapError lets the Bridge retry the exact next report.
type SequenceGapError struct{ Expected uint64 }

func (err *SequenceGapError) Error() string {
	return fmt.Sprintf("expected report sequence %d", err.Expected)
}

var ErrInboxFull = errors.New("remote inbox is full")

// MatchesReport is the consumption fence, checked again after admission.
func (executor Executor) MatchesReport(report RemoteReport) bool {
	return executor.Mode == ModeRemote && executor.Remote != nil && report.Generation != 0 &&
		executor.Epoch == report.Epoch && executor.Generation == report.Generation && executor.Remote.ConnectionID == report.ConnectionID
}

// AdmitReport computes admission under executorLock. The caller commits the
// returned Executor and Channel publication together, and publishes only if new.
// A sequence at or below Reported of the same epoch and connection is a
// duplicate whatever its generation; only a never-admitted one is refused.
func (executor Executor) AdmitReport(report RemoteReport, pending []RemoteReport) (Executor, ReportAnswer, error) {
	answer := ReportAnswer{Epoch: executor.Epoch, Generation: executor.Generation}
	if executor.Remote != nil {
		answer.Reported = executor.Remote.Reported
	}
	// An admitted sequence stays admitted across Stop: a retry after a lost
	// answer is a duplicate, so stale_generation proves non-admission.
	sameEpoch := executor.Mode == ModeRemote && executor.Remote != nil && executor.Epoch == report.Epoch && executor.Remote.ConnectionID == report.ConnectionID
	if sameEpoch && report.Sequence <= answer.Reported {
		answer.Duplicate = true
		return executor, answer, nil
	}
	if !executor.MatchesReport(report) {
		return executor, answer, &executorChangedError{}
	}
	if answer.Reported == ^uint64(0) || report.Sequence != answer.Reported+1 {
		return executor, answer, &SequenceGapError{Expected: answer.Reported + 1}
	}
	if len(pending) >= inboxCapacity {
		return executor, answer, ErrInboxFull
	}
	encoded, err := json.Marshal(InboxItem{Report: &report})
	if err != nil {
		return executor, answer, err
	}
	bytes := len(encoded) + 2 + len(pending)
	for _, queued := range pending {
		encoded, err := json.Marshal(InboxItem{Report: &queued})
		if err != nil {
			return executor, answer, err
		}
		bytes += len(encoded)
	}
	if bytes > maximumInboxBytes {
		return executor, answer, ErrInboxFull
	}
	remote := *executor.Remote
	remote.Reported, answer.Reported = report.Sequence, report.Sequence
	executor.Remote = &remote
	return executor, answer, nil
}

// RefusalCode is a domain admission refusal, mapped by the HTTP boundary.
type RefusalCode string

const (
	RefusalMalformed       RefusalCode = "malformed_request"
	RefusalExecutorChanged RefusalCode = "executor_changed"
	RefusalStaleGeneration RefusalCode = "stale_generation"
	RefusalSequenceGap     RefusalCode = "sequence_gap"
	RefusalInboxFull       RefusalCode = "inbox_full"
	RefusalSessionClosed   RefusalCode = "session_closed"
)

// ReportRefusal keeps exact fence and gap facts across a typed Flow RPC.
type ReportRefusal struct {
	Code             RefusalCode `json:"code"`
	Message          string      `json:"message"`
	Epoch            uint64      `json:"epoch"`
	Generation       uint64      `json:"generation"`
	ExpectedSequence uint64      `json:"expected_sequence,omitempty"`
}

func (refusal *ReportRefusal) Error() string { return refusal.Message }

// ReportResult is admission's answer or its explicit domain refusal.
type ReportResult struct {
	Answer  ReportAnswer   `json:"answer"`
	Refusal *ReportRefusal `json:"refusal,omitempty"`
}

// InboxItem is the server-owned union: ReportRemote creates only Report;
// SetExecutor creates only Control. The Bridge has no control submission field.
type InboxItem struct {
	Report  *RemoteReport    `json:"report,omitempty"`
	Control *ExecutorChanged `json:"control,omitempty"`
}

// ExecutorChanged has diagnostic fences only. The consumer reads Executor.
type ExecutorChanged struct {
	Epoch      uint64 `json:"epoch"`
	Generation uint64 `json:"generation"`
}
