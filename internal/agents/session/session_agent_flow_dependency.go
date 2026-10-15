// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package session

import (
	"context"
	"errors"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

// ErrWorkspaceCommandRejected marks a workspace command that asking again
// cannot change, such as one for a Project that no longer admits a workspace.
// It wraps the reason, which the Session records with the failed request.
var ErrWorkspaceCommandRejected = errors.New("session workspace command rejected")

// SessionAgentFlowDependency is what SessionAgentFlow consumes from the Session
// Service, which lives in another package because it also fronts the
// SessionWorkspaceFlow, the PreviewFlow, the SubagentFlow and the Project.
// Every method is called only from cold-path, deadline and Preview handoff
// Steps, from ReportPlatformIssue beside the turn and from the stop of a
// background command's watch at the Session's close, never from a tool, except
// the AgentTaskLauncher, which the delegation tools start_agent and stop_task
// and the Stop button call.
type SessionAgentFlowDependency interface {
	agenttasks.AgentTaskLauncher
	// ReportPlatformIssue starts the PlatformIssueFlow of the incident's
	// signature, which files it, or joins the one that runs. It never changes
	// the Session; an error is a failed start, which the Step asks again.
	ReportPlatformIssue(ctx context.Context, id agentloop.SessionIdentity, incident agentloop.PlatformIncident) error
	// AdmitSession runs before any model, tool, or workspace effect. False is an
	// authoritative admission denial, not a transport or provider error.
	AdmitSession(context.Context, agentloop.SessionIdentity) (bool, error)
	// ProvisionWorkspace sends the SessionWorkspaceFlow, which it starts once,
	// the command that provisions the instance of requestID on the Session
	// HEAD, after reclaiming an earlier one; previewHolds names the instance
	// the Preview holds. False refuses a command of a replaced request; an
	// error wrapping ErrWorkspaceCommandRejected is a business rejection.
	ProvisionWorkspace(ctx context.Context, id agentloop.SessionIdentity, requestID, previewHolds string) (bool, error)
	// ReclaimWorkspace sends the command that reclaims the instance of the
	// revoked request requestID, keeping its volume. It answers like
	// ProvisionWorkspace; the outcome returns as a reclaimed report.
	ReclaimWorkspace(ctx context.Context, id agentloop.SessionIdentity, requestID, previewHolds string) (bool, error)
	// ObserveWorkspace reads the outcome of requestID from the
	// SessionWorkspaceFlow, which owns it, once the Session's wait ended
	// without its report: nil while it is not settled, a grant with a Failure
	// for a failed request, any other grant as READY.
	ObserveWorkspace(ctx context.Context, id agentloop.SessionIdentity, requestID string) (*agentloop.WorkspaceGrant, error)
	// NotifyPreviewWorkspace announces to the Project's Preview command
	// commandID that the request it waits for settled: READY, or FAILED with
	// the safe code.
	NotifyPreviewWorkspace(ctx context.Context, id agentloop.SessionIdentity, commandID, requestID, failure string) error
	// RequestPreviewDeployment asks the Project's Preview, with one bounded
	// call, to deploy the commit of handoff on the READY instance requestID,
	// starting the Preview first if the Project never ran one. The answer
	// stays with the Preview; its outcome returns as a notification.
	RequestPreviewDeployment(ctx context.Context, id agentloop.SessionIdentity, requestID string, handoff agentloop.PreviewHandoff) error
	// StopBackgroundCommandWatch stops the BackgroundCommandFlow SubFlow flowID that watches a
	// background command, when the Session closes; one that no longer runs is stopped already.
	StopBackgroundCommandWatch(ctx context.Context, flowID string) error
	// SubmitConnectorContribution reconciles the Project and slug's existing
	// contribution before accepting the remote worktree's committed head.
	SubmitConnectorContribution(ctx context.Context, projectID string, contributed conversation.ConnectorContribution, summary string) error
}

// ProjectDiagnosticsReader runs the Agent's read-only platform tools on the
// server. identity scopes every read to the Session's own Project; the call's
// arguments never do. It takes no workspace lease and never reaches sandboxd,
// so the tools work while the Sandbox is reclaimed. A refused or impossible
// read is a known-failure result; an error is a failure that the Step retries.
type ProjectDiagnosticsReader interface {
	ReadDiagnostics(ctx context.Context, identity agentloop.SessionIdentity, call conversation.ToolCall) (agentloop.ToolExecutionResult, error)
}

// repositoryContextReader reads the trusted prompt context from the checked-out
// HEAD through the pooled sandboxd transport (no Flow RPC).
type repositoryContextReader interface {
	RepositoryContext(ctx context.Context, lease workspace.Lease) (string, error)
}
