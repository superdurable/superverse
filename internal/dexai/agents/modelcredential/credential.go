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
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
)

// Provider is a closed model credential namespace.
type Provider struct{ catalog conversation.Provider }

var Anthropic = Provider{catalog: conversation.ProviderAnthropic}

var declaredProviders = credentialProviders()

// Providers returns a copy of the bounded, catalog-derived credential namespaces.
func Providers() []Provider { return slices.Clone(declaredProviders) }

func credentialProviders() []Provider {
	var providers []Provider
	for _, model := range conversation.Models() {
		if model.IsMock() {
			continue
		}
		provider := Provider{catalog: model.Details().Provider}
		if !slices.Contains(providers, provider) {
			providers = append(providers, provider)
		}
	}
	slices.SortFunc(providers, func(a, b Provider) int { return strings.Compare(a.String(), b.String()) })
	return providers
}

// ForModel resolves a real model to its canonical credential namespace.
func ForModel(model conversation.Model) Provider {
	if model.IsMock() {
		panic("mock models have no credential namespace")
	}
	return Provider{catalog: model.Details().Provider}
}

// Catalog exposes the canonical provider for typed boundary mapping.
func (p Provider) Catalog() conversation.Provider { return p.catalog }

func (p Provider) String() string               { return p.catalog.String() }
func (p Provider) MarshalText() ([]byte, error) { return []byte(p.String()), nil }
func (p *Provider) UnmarshalText(value []byte) error {
	if len(value) == 0 {
		return errors.New("model credential provider is required")
	}
	return comm.UnmarshalName(value, p, Provider.String, Providers()...)
}

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
	Endpoint          conversation.EndpointSelection `json:"endpoint,omitempty"`
	ChangeReason      ChangeReason                   `json:"change_reason"`
	Holders           []Holder                       `json:"holders,omitempty"`
	Previous          *Info                          `json:"previous,omitempty"`
	HighWaterRevision uint64                         `json:"high_water_revision,omitempty"`
	OperationRevision uint64                         `json:"operation_revision,omitempty"`
	WriteAttempt      uint64                         `json:"write_attempt,omitempty"`
	Revision          uint64                         `json:"revision"`
	Status            Status                         `json:"status"`
	Last4             string                         `json:"last4,omitempty"`
	SetBy             conversation.Actor             `json:"set_by"`
	SetAt             time.Time                      `json:"set_at"`
	OperationID       string                         `json:"operation_id,omitempty"`
	Delete            bool                           `json:"delete,omitempty"`
}

func (info Info) Active() bool { return info.Status == StatusActive }

// Mutation is safe in Dex RPC history: it never contains key material.
type Mutation struct {
	Endpoint         conversation.EndpointSelection `json:"endpoint,omitempty"`
	Reason           ChangeReason                   `json:"reason"`
	Cancel           bool                           `json:"cancel,omitempty"`
	RetryDelete      bool                           `json:"retry_delete,omitempty"`
	WriteAttempt     uint64                         `json:"write_attempt,omitempty"`
	Provider         Provider                       `json:"provider"`
	OperationID      string                         `json:"operation_id"`
	ExpectedRevision uint64                         `json:"expected_revision"`
	Delete           bool                           `json:"delete"`
	Actor            conversation.Actor             `json:"actor"`
	Last4            string                         `json:"last4,omitempty"`
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
