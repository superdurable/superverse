// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package bridgeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Report bounds, which the Session's admission enforces.
const (
	MaxReportEvents       = 32
	MaxEventTextBytes     = 64 << 10
	MaxToolArgumentsBytes = 256 << 10
)

// Report is one numbered report of the remote agent. It carries exactly one
// of Events, ToolCall and TurnCompleted. Sequence starts at 1 for a connection
// and increases by one; Generation is that of the Codex turn that produced it.
type Report struct {
	Epoch         uint64    `json:"epoch"`
	Generation    uint64    `json:"generation"`
	Sequence      uint64    `json:"sequence"`
	ProviderTurn  string    `json:"providerTurn,omitempty"`
	Events        []Event   `json:"events,omitempty"`
	ToolCall      *ToolCall `json:"toolCall,omitempty"`
	TurnCompleted bool      `json:"turnCompleted,omitempty"`
}

// ReportResponse is the executor state after a report. Duplicate is a report
// whose sequence the Session had already admitted; it is acknowledged too.
type ReportResponse struct {
	Epoch      uint64 `json:"epoch"`
	Generation uint64 `json:"generation"`
	Reported   uint64 `json:"reported"`
	Duplicate  bool   `json:"duplicate"`
}

// Event is one public event of the remote agent. Role is set only for an
// imported message; EntrySequence names the entry a delivery event concerns.
type Event struct {
	Kind          EventKind `json:"kind"`
	Text          string    `json:"text,omitempty"`
	Role          EntryRole `json:"role,omitempty"`
	EntrySequence uint64    `json:"entrySequence,omitempty"`
}

// EventKind is the kind of a reported event.
type EventKind string

const (
	EventKindAssistantMessage  EventKind = "assistant_message"
	EventKindUserMessage       EventKind = "user_message"
	EventKindImportedMessage   EventKind = "imported_message"
	EventKindActivity          EventKind = "activity"
	EventKindDeliveryUncertain EventKind = "delivery_uncertain"
	EventKindDeliveryFailed    EventKind = "delivery_failed"
	EventKindDeliveryDelayed   EventKind = "delivery_delayed"
)

// ToolCall is one call of a remote tool; Arguments is its JSON object.
type ToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Validate checks a report's shape and bounds before it is sent or admitted.
func (report Report) Validate() error {
	if report.Sequence == 0 {
		return errors.New("a report sequence starts at 1")
	}
	parts := 0
	if len(report.Events) > 0 {
		parts++
	}
	if report.ToolCall != nil {
		parts++
	}
	if report.TurnCompleted {
		parts++
	}
	if parts != 1 {
		return errors.New("a report carries exactly one of events, toolCall and turnCompleted")
	}
	if len(report.Events) > MaxReportEvents {
		return fmt.Errorf("a report carries at most %d events", MaxReportEvents)
	}
	for _, event := range report.Events {
		if err := event.validate(); err != nil {
			return err
		}
	}
	if call := report.ToolCall; call != nil {
		if call.Name == "" {
			return errors.New("a tool call needs a name")
		}
		if len(call.Arguments) > MaxToolArgumentsBytes {
			return fmt.Errorf("tool arguments exceed %d bytes", MaxToolArgumentsBytes)
		}
		var arguments map[string]json.RawMessage
		if err := json.Unmarshal(call.Arguments, &arguments); err != nil || arguments == nil {
			return errors.New("tool arguments must be a JSON object")
		}
	}
	return nil
}

func (event Event) validate() error {
	if len(event.Text) > MaxEventTextBytes {
		return fmt.Errorf("%s event text exceeds %d bytes", event.Kind, MaxEventTextBytes)
	}
	switch event.Kind {
	case EventKindAssistantMessage, EventKindUserMessage, EventKindActivity:
		if event.Text == "" {
			return fmt.Errorf("a %s event needs text", event.Kind)
		}
	case EventKindImportedMessage:
		if event.Text == "" || (event.Role != EntryRoleUser && event.Role != EntryRoleAssistant) {
			return errors.New("an imported message needs text and the user or assistant role")
		}
	case EventKindDeliveryUncertain, EventKindDeliveryFailed, EventKindDeliveryDelayed:
		if event.EntrySequence == 0 {
			return fmt.Errorf("a %s event names the entry", event.Kind)
		}
	default:
		return fmt.Errorf("unknown event kind %q", event.Kind)
	}
	if event.Role != "" && event.Kind != EventKindImportedMessage {
		return fmt.Errorf("a %s event has no role", event.Kind)
	}
	return nil
}

const remoteCallPrefix = "remote-"

// RemoteCallID is the call ID of the tool call in report sequence of epoch:
// remote-<epoch>-<sequence>, for example remote-7-42.
func RemoteCallID(epoch, sequence uint64) string {
	return remoteCallPrefix + strconv.FormatUint(epoch, 10) + "-" + strconv.FormatUint(sequence, 10)
}

// IsRemoteCallID reports a call ID of the RemoteCallID form.
func IsRemoteCallID(callID string) bool {
	return strings.HasPrefix(callID, remoteCallPrefix)
}
