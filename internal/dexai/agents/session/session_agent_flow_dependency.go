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
	"encoding/json"
	"errors"

	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/sandboxtools"
	"github.com/superdurable/superversev2/internal/dexai/delivery/evolve"
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
// and the Stop button call, and the agent review's EvolveExport and
// ReportEvolveReview, which place_evolve_export and report_evolve_review call.
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
	// EvolveExport reads, for place_evolve_export, the export of review
	// reviewID of the Session's own Project from storage, and the goals file
	// at its goals commit through the repository host from the lease's
	// repository, with no Flow read; nil when the review has no stored export.
	EvolveExport(ctx context.Context, projectID, reviewID string, repository git.Repository) (*sandboxtools.EvolveExport, error)
	// ReportEvolveReview reports the Session's report on an agent review of
	// its Project, with a proposal's files, to the Project's goal measurement,
	// which checks the files against the review; a refusal is an answer.
	ReportEvolveReview(ctx context.Context, projectID string, report evolve.ReviewReport, files *evolve.ReviewFiles) (evolve.ReportAnswer, error)
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

// connectorCatalogMatcher finds the released connectors a request names in
// the server replica's warm copy of the published connector catalog, with no
// network call: the catalog_matches JSON of at most six connectors and 8 KiB,
// or "" while the copy is cold or nothing matches. The Step that opens the
// Session's first segment calls it.
type connectorCatalogMatcher interface {
	RequestMatches(request string) string
}

// attachmentRenditions resolves the attachment references of the model
// requests, which the CallModel and CompactContext Steps make, also in the
// executor segment. Resolve returns, in order, the exact Claude block each
// reference (full, or a brief) shows: the immutable block its rendition object
// holds, byte-identical on every call, which the request carries in place of
// the reference. The first resolve of an attachment copies its staged objects,
// at most four at a time, into the Project's prefix
// <projectID>/attachments/<id>/ and writes its manifest last; later resolves
// read that copy. The Project's deletion removes the prefix only after this
// Session completed. An attachment without a manifest whose staged objects are
// gone resolves to conversation.ExpiredAttachmentBlock, which stays so. An
// error is transient, and the Step retries. BoundFile reads, for
// place_attachment, the file of an attachment the Project bound: a JSON
// file's redacted copy; false when the Project bound none of that ID.
type attachmentRenditions interface {
	Resolve(ctx context.Context, projectID string, references []conversation.AttachmentReference) ([]json.RawMessage, error)
	BoundFile(ctx context.Context, projectID, attachmentID string) (conversation.AttachmentFile, bool, error)
}
