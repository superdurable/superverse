// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package subagent

import (
	"context"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm"
)

// SubagentFlowDependency is what SubagentFlow consumes from the Session
// Service, which fronts the subagent's Session: the subagent reaches its
// Session only through it, and only to be admitted, to ask for a lease after
// the transport proved its copy unreachable, and to report its end. No tool
// call reaches it.
type SubagentFlowDependency interface {
	// AdmitSubagent asks the Session whether the subagent may start: admitted
	// only while the Session is ACTIVE and its record of the agent is
	// starting with this fingerprint, or already running with it. A denial is
	// authoritative; it is marked stopped when a Stop or the Session's end
	// stopped the record before the subagent started.
	AdmitSubagent(ctx context.Context, id agenttasks.SubagentIdentity, admission agenttasks.SubagentAdmission) (agenttasks.SubagentAdmissionAnswer, error)
	// RefreshSubagentWorkspace asks the Session for the lease of its instance
	// for a running subagent, reporting the instance lost whose lease proved
	// unreachable. The Session starts a new instance when it has none.
	RefreshSubagentWorkspace(ctx context.Context, id agenttasks.SubagentIdentity, lost string) (WorkspaceRefresh, error)
	// ReportSubagentOutcome delivers the subagent's task notification to the
	// Session; the notification's ID makes a repeat harmless.
	ReportSubagentOutcome(ctx context.Context, id agenttasks.SubagentIdentity, notification conversation.SessionNotification) (ReportAnswer, error)
}

// WorkspaceRefresh is the Session's answer to a lease refresh: the lease of
// its usable instance, none while it provisions one, or Closed when the
// subagent may no longer use the workspace (the Session is not ACTIVE, or its
// record of the agent no longer runs).
type WorkspaceRefresh struct {
	Lease  *workspace.Lease
	Closed bool
}

// ReportAnswer is the Session's answer to a report: delivered (admitted, or
// already admitted under its ID), busy (the Session's queue is full, so the
// report is asked again), or closed (the Session ended, so it never arrives).
type ReportAnswer struct{ name string }

var (
	ReportAdmitted = ReportAnswer{"admitted"}
	ReportBusy     = ReportAnswer{"busy"}
	ReportClosed   = ReportAnswer{"closed"}
)

func (answer ReportAnswer) String() string               { return answer.name }
func (answer ReportAnswer) MarshalText() ([]byte, error) { return []byte(answer.name), nil }
func (answer *ReportAnswer) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, answer, ReportAnswer.String, ReportAdmitted, ReportBusy, ReportClosed)
}

// ProjectDiagnosticsReader runs a subagent's read-only platform tools on the
// server for its Session's own Project; it never reaches sandboxd and refuses
// every subagent Flow.
type ProjectDiagnosticsReader interface {
	ReadDiagnostics(ctx context.Context, identity agentloop.SessionIdentity, call conversation.ToolCall) (agentloop.ToolExecutionResult, error)
}

// repositoryContextReader reads the trusted prompt context of the checked-out
// HEAD through the pooled sandboxd transport (no Flow RPC).
type repositoryContextReader interface {
	RepositoryContext(ctx context.Context, lease workspace.Lease) (string, error)
}
