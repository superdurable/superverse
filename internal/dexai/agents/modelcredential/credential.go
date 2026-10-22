// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelcredential

import (
	"errors"
	"time"

	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Provider is a closed model credential namespace.
type Provider struct{ catalog conversation.Provider }

var Anthropic = Provider{catalog: conversation.ProviderAnthropic}

func (p Provider) String() string               { return p.catalog.String() }
func (p Provider) MarshalText() ([]byte, error) { return []byte(p.String()), nil }
func (p *Provider) UnmarshalText(value []byte) error {
	if string(value) != Anthropic.String() {
		return errProvider
	}
	*p = Anthropic
	return nil
}

var errProvider = errors.New("unsupported model credential provider")
var ErrRevision = errors.New("model credential revision changed")
var ErrPending = errors.New("model credential operation needs reconciliation")
var ErrCopyStopped = errors.New("subagent stopped because the model credential changed")
var ErrCanceled = errors.New("the change did not take effect; save the key again")

// UnavailableError is retryable by a model Step while a subagent copy is arriving.
type UnavailableError struct{}

func (*UnavailableError) Error() string { return "model credential not available" }

var ErrUnavailable = &UnavailableError{}

// FailureCode identifies a public credential failure without accepting arbitrary values.
type FailureCode struct{ name string }

func (c FailureCode) String() string { return c.name }

var UnavailableCode = FailureCode{name: "model_credential_not_available"}

// Status is the bounded lifecycle of one provider's credential.
type Status struct{ name string }

var (
	StatusActive  = Status{name: "active"}
	StatusPending = Status{name: "pending"}
	StatusRevoked = Status{name: "revoked"}
)

func (s Status) String() string               { return s.name }
func (s Status) MarshalText() ([]byte, error) { return []byte(s.name), nil }
func (s *Status) UnmarshalText(value []byte) error {
	for _, candidate := range []Status{{}, StatusActive, StatusPending, StatusRevoked} {
		if candidate.name == string(value) {
			*s = candidate
			return nil
		}
	}
	return errors.New("invalid model credential status")
}

// Envelope is secret: only a hard-delete Attribute may persist it.
type Envelope struct {
	Revision uint64 `json:"revision"`
	Key      string `json:"key"`
}

// Holder is a bounded child-copy admission identified by its original tool call.
type Holder struct {
	AgentID  string `json:"agent_id"`
	CallID   string `json:"call_id"`
	Revision uint64 `json:"revision"`
}

// Info owns non-secret configuration and one operation's admission/result.
type Info struct {
	ChangeReason      ChangeReason       `json:"change_reason"`
	Holders           []Holder           `json:"holders,omitempty"`
	Previous          *Info              `json:"previous,omitempty"`
	HighWaterRevision uint64             `json:"high_water_revision,omitempty"`
	OperationRevision uint64             `json:"operation_revision,omitempty"`
	WriteAttempt      uint64             `json:"write_attempt,omitempty"`
	Revision          uint64             `json:"revision"`
	Status            Status             `json:"status"`
	Last4             string             `json:"last4,omitempty"`
	SetBy             conversation.Actor `json:"set_by"`
	SetAt             time.Time          `json:"set_at"`
	OperationID       string             `json:"operation_id,omitempty"`
	Delete            bool               `json:"delete,omitempty"`
}

func (info Info) Active() bool { return info.Status == StatusActive }

// Mutation is safe in Dex RPC history: it never contains key material.
type Mutation struct {
	Reason           ChangeReason       `json:"reason"`
	Cancel           bool               `json:"cancel,omitempty"`
	RetryDelete      bool               `json:"retry_delete,omitempty"`
	WriteAttempt     uint64             `json:"write_attempt,omitempty"`
	Provider         Provider           `json:"provider"`
	OperationID      string             `json:"operation_id"`
	ExpectedRevision uint64             `json:"expected_revision"`
	Delete           bool               `json:"delete"`
	Actor            conversation.Actor `json:"actor"`
	Last4            string             `json:"last4,omitempty"`
}

// Admission distinguishes a newly reserved operation from a replay that must query first.
type Admission struct {
	Info     Info `json:"info"`
	Admitted bool `json:"admitted"`
	Fresh    bool `json:"fresh"`
}

// ChangeReason is the closed reason for changing a credential or stopping its child copy.
type ChangeReason struct{ name string }

var ChangedReason = ChangeReason{name: "model credential changed"}

// ProjectDeletionReason accompanies an empty system Actor; it never attributes cleanup to a user.
var ProjectDeletionReason = ChangeReason{name: "project deletion"}

func (r ChangeReason) String() string               { return r.name }
func (r ChangeReason) MarshalText() ([]byte, error) { return []byte(r.name), nil }
func (r *ChangeReason) UnmarshalText(value []byte) error {
	for _, reason := range []ChangeReason{{}, ChangedReason, ProjectDeletionReason} {
		if reason.name == string(value) {
			*r = reason
			return nil
		}
	}
	return errors.New("invalid model credential change reason")
}
