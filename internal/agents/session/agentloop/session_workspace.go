// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agentloop

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

const (
	// maximumWorkspaceLosses bounds re-initialization on behalf of one tool call.
	maximumWorkspaceLosses = 2
	// MaximumRepositoryContextBytes bounds the repository context of a segment.
	MaximumRepositoryContextBytes = 8 << 10
	// sandboxDeadlineMinimumMove is the smallest move of the Sandbox deadline
	// worth a durable write, so that a busy turn does not turn every tool call
	// into one.
	sandboxDeadlineMinimumMove = time.Minute
	// KeepRunningHold is how long the user's "Keep running" holds the READY
	// instance: Studio's "Keep running for 7 days".
	KeepRunningHold = 7 * 24 * time.Hour
)

// WorkspaceState is the Session's view of its accepted Sandbox capability. The
// empty state means the Session has not requested an instance yet.
type WorkspaceState string

const (
	WorkspaceRequested WorkspaceState = "REQUESTED"
	WorkspaceReady     WorkspaceState = "READY"
	WorkspaceRevoked   WorkspaceState = "REVOKED"
	WorkspaceFailed    WorkspaceState = "FAILED"
)

// SessionWorkspace is the Session's accepted Sandbox capability: the request
// side of its instances, which the SessionWorkspaceFlow owns. The Session
// sends that Flow a command for each request and for the reclamation at the
// Sandbox deadline, holds the accepted lease and the deadline, and decides
// whether the instance is idle; the Flow provisions, reclaims and replaces
// the instance and reports each outcome. Only the Session's own command,
// sent after it revoked or replaced the lease, ever reclaims a Pod, so no
// instance in use is reclaimed.
type SessionWorkspace struct {
	// State and RequestID are the workspace request: the request in flight,
	// or the last one and how it settled.
	State         WorkspaceState  `json:"state"`
	RequestID     string          `json:"requestId,omitempty"`
	Lease         workspace.Lease `json:"lease"`                   // last accepted lease; admits new tools only while State is READY
	Failure       string          `json:"failure,omitempty"`       // safe code
	FailureDetail string          `json:"failureDetail,omitempty"` // why the request or its command failed, bounded
	// RepositoryContext is the trusted context of the checkout, read once a
	// lease is accepted; every segment_start carries it.
	RepositoryContext string `json:"repositoryContext,omitempty"`
	// JoinedResumeAt is the conversation continuation that waits for
	// RequestID; the branch that settles the request resumes it.
	JoinedResumeAt Continuation        `json:"joinedResumeAt,omitempty"`
	LostCallID     conversation.CallID `json:"lostCallId,omitempty"`
	Losses         int                 `json:"losses,omitempty"`
	// SandboxDeadline is when the instance of a READY request is reclaimed;
	// its volume is kept. It is zero unless the request is READY and only
	// moves later. SessionAgentFlow is its only writer.
	SandboxDeadline time.Time `json:"sandboxDeadline,omitzero"`
	// PreviewWaits is the ID of the Preview command that waits for the next
	// request to settle, which the settlement names so that the Preview admits
	// it only for that command; PreviewHolds is the instance lent to the
	// Preview, which every command names so that the reclamation of that
	// instance has the Preview release it first.
	PreviewWaits string `json:"previewWaitsFor,omitempty"`
	PreviewHolds string `json:"previewHolds,omitempty"`
}

// WorkspaceUse marks tools dispatched on one instance; only the decision to
// reclaim it reads it.
type WorkspaceUse struct {
	Active    bool   `json:"active"`
	RequestID string `json:"requestId,omitempty"`
}

// WorkspaceGrant is the outcome of a request that the Session accepts: the
// READY lease, or the failure code with its bounded detail.
type WorkspaceGrant struct {
	RequestID     string          `json:"requestId"`
	Lease         workspace.Lease `json:"lease"`             // valid when Failure == ""
	Failure       string          `json:"failure,omitempty"` // safe code
	FailureDetail string          `json:"failureDetail,omitempty"`
}

// WorkspaceReport is an outcome the SessionWorkspaceFlow reports for the
// request WorkspaceRequestID: the grant of a ready or failed request, or the
// reclamation of its instance. A report never reaches the conversation.
type WorkspaceReport struct {
	WorkspaceRequestID string          `json:"workspaceRequestId"`
	Grant              *WorkspaceGrant `json:"grant,omitempty"`
	Reclaimed          bool            `json:"reclaimed,omitempty"`
}

// WorkspaceSummary is the lifecycle read of the Session workspace's request
// side; the SessionWorkspaceFlow reads the instance. It carries no lease or
// binding.
type WorkspaceSummary struct {
	State         WorkspaceState `json:"state"`
	RequestID     string         `json:"requestId,omitempty"`
	Failure       string         `json:"failure,omitempty"`
	FailureDetail string         `json:"failureDetail,omitempty"`
	// SandboxDeadline is when a READY instance is reclaimed, zero otherwise.
	SandboxDeadline time.Time `json:"sandboxDeadline,omitzero"`
}

// WorkspaceDemandRequest asks the Session for a workspace outside the
// conversation. PreviewCommand marks the Project's Preview with the ID of its
// active command: a READY instance is lent to it, and otherwise the command
// waits for the next request to settle.
// ReadyOnly marks the Preview's automatic deployment, which takes only a
// READY instance: it opens no request, so a reclaimed Sandbox is never woken
// for it. ForAgentID marks a running subagent whose transport proved the
// lease of request Lost unreachable: it takes the lease of a usable instance,
// and its loss revokes that instance, which the next request replaces.
type WorkspaceDemandRequest struct {
	PreviewCommand string `json:"previewCommand,omitempty"`
	ReadyOnly      bool   `json:"readyOnly,omitempty"`
	ForAgentID     string `json:"forAgentId,omitempty"`
	Lost           string `json:"lost,omitempty"`
}

// request records a new instance Request. The previous lease stays until the
// next accepted lease replaces it.
func (ws *SessionWorkspace) request(requestID string) {
	ws.State = WorkspaceRequested
	ws.RequestID = requestID
	ws.Failure, ws.FailureDetail = "", ""
	ws.SandboxDeadline = time.Time{}
}

// ExtendDeadline moves the Sandbox deadline of a READY request to at least
// now + hold. The deadline only moves later, and only by at least a minute. It
// reports whether it moved, which the caller records under the Workspace lock.
// Every event that holds the Sandbox extends it here: the acceptance of a
// READY lease, the main agent's workspace use, a task's end and the lend to a
// Preview command with the Sandbox hold, a ready delivery notification of the
// Preview with the Preview hold, and the user's "Keep running" with
// KeepRunningHold.
func (ws *SessionWorkspace) ExtendDeadline(now time.Time, hold time.Duration) bool {
	target := now.Add(hold).UTC()
	if ws.State != WorkspaceReady || target.Sub(ws.SandboxDeadline) < sandboxDeadlineMinimumMove {
		return false
	}
	ws.SandboxDeadline = target
	return true
}

// Revoke stops new dispatches on the instance of the current request, which is
// being reclaimed or proved unreachable; its deadline ends with it.
func (ws *SessionWorkspace) Revoke() {
	ws.State, ws.SandboxDeadline = WorkspaceRevoked, time.Time{}
}

// join records the conversation continuation that waits for the request in
// flight. Only the conversation chain waits with a continuation, and it is
// waiting here, so it cannot join twice.
func (ws *SessionWorkspace) join(next Continuation) {
	if next == "" {
		return
	}
	if ws.JoinedResumeAt != "" {
		panic(fmt.Sprintf("workspace request %s already resumes %q", ws.RequestID, ws.JoinedResumeAt))
	}
	ws.JoinedResumeAt = next
}

// TakeJoined returns the joined continuation for the branch that settles the
// request and clears it.
func (ws *SessionWorkspace) TakeJoined() Continuation {
	next := ws.JoinedResumeAt
	ws.JoinedResumeAt = ""
	return next
}

// Accept applies the outcome of the request the Session is waiting for: it
// becomes READY with the lease, or FAILED with the code. A Preview that held
// an earlier instance holds none of this one.
func (ws *SessionWorkspace) Accept(identity SessionIdentity, grant WorkspaceGrant) error {
	if grant.Failure != "" {
		ws.Fail(grant.Failure, grant.FailureDetail)
		return nil
	}
	if err := validateWorkspaceLease(identity, ws.RequestID, grant.Lease); err != nil {
		return err
	}
	ws.State, ws.Lease, ws.Failure, ws.FailureDetail = WorkspaceReady, grant.Lease, "", ""
	if ws.PreviewHolds != grant.Lease.RequestID {
		ws.PreviewHolds = ""
	}
	return nil
}

// validateWorkspaceLease fences a lease to the Session and request that accept it.
func validateWorkspaceLease(identity SessionIdentity, requestID string, lease workspace.Lease) error {
	if err := lease.Validate(); err != nil {
		return fmt.Errorf("workspace lease: %w", err)
	}
	binding := lease.Binding
	repository := lease.Repository
	if lease.RequestID != requestID || binding.ProjectID != identity.ProjectID ||
		binding.FlowID != flowid.SessionFlowID(identity.ProjectID) || repository.ProjectID != identity.ProjectID {
		return errors.New("workspace lease does not match the Session request")
	}
	return nil
}

// Fail settles the current request as FAILED with a safe code and the
// bounded detail of why, if known.
func (ws *SessionWorkspace) Fail(code, detail string) {
	ws.State = WorkspaceFailed
	ws.Failure, ws.FailureDetail = code, detail
	ws.SandboxDeadline = time.Time{}
}

// Reclaimed records the reclamation of instance requestID: a Preview no
// longer holds it.
func (ws *SessionWorkspace) Reclaimed(requestID string) {
	if ws.PreviewHolds == requestID {
		ws.PreviewHolds = ""
	}
}

// countLoss records that the transport proved the instance of request lost
// unreachable before callID sent any request. One call may ask for a bounded
// number of replacements: past the bound it reports exhaustion, otherwise the
// instance counts as revoked and the next request reclaims it first.
func (ws *SessionWorkspace) countLoss(lost string, callID conversation.CallID) (isExhausted bool) {
	if lost == "" || ws.RequestID != lost || (ws.State != WorkspaceReady && ws.State != WorkspaceRevoked) {
		return false
	}
	if ws.LostCallID == callID {
		ws.Losses++
	} else {
		ws.LostCallID, ws.Losses = callID, 1
	}
	if ws.Losses > maximumWorkspaceLosses {
		return true
	}
	ws.Revoke()
	return false
}

// WorkspaceDemandPlan is how the Session admits a workspace demand from
// outside the conversation.
type WorkspaceDemandPlan struct {
	// Lent reports that the Preview may use the READY instance.
	Lent bool
	// Lease is the lease a subagent takes: the usable instance's.
	Lease *workspace.Lease
	// Recorded reports a change to the workspace, which the caller records.
	Recorded bool
	// Opens reports that no request is in flight, so the cold path opens one.
	Opens bool
}

// PlanDemand admits a workspace demand from outside the conversation. A
// subagent's lost lease revokes its instance if it is still the current one.
// A usable instance is lent to a Preview, which releases it before the
// instance is reclaimed, and its lease is handed to a subagent. Otherwise a
// ready-only demand gets nothing, a Preview waits for the next request to
// settle, and the cold path opens a request unless one is in flight.
func (ws *SessionWorkspace) PlanDemand(demand WorkspaceDemandRequest) WorkspaceDemandPlan {
	isPreview := demand.PreviewCommand != ""
	isRevoked := demand.Lost != "" && ws.RequestID == demand.Lost && ws.State == WorkspaceReady
	if isRevoked {
		ws.Revoke()
	}
	switch {
	case ws.IsUsable() && demand.ForAgentID != "":
		lease := ws.Lease
		return WorkspaceDemandPlan{Lease: &lease}
	case isRevoked:
		return WorkspaceDemandPlan{Recorded: true, Opens: true}
	case ws.IsUsable() && (!isPreview || ws.PreviewHolds == ws.Lease.RequestID):
		return WorkspaceDemandPlan{Lent: isPreview}
	case ws.IsUsable():
		ws.PreviewHolds = ws.Lease.RequestID
		return WorkspaceDemandPlan{Lent: true, Recorded: true}
	case demand.ReadyOnly:
		return WorkspaceDemandPlan{}
	}
	plan := WorkspaceDemandPlan{Opens: ws.State != WorkspaceRequested}
	// The newest command waits: an earlier one that ended must not be announced.
	if isPreview && ws.PreviewWaits != demand.PreviewCommand {
		ws.PreviewWaits, plan.Recorded = demand.PreviewCommand, true
	}
	return plan
}

// TakePreviewWaits returns the Preview command that waits for the request
// that settles now and clears the mark: the settlement is announced once.
func (ws *SessionWorkspace) TakePreviewWaits() string {
	waits := ws.PreviewWaits
	ws.PreviewWaits = ""
	return waits
}

// WorkspaceRequestPlan is what the cold path does with one demand.
type WorkspaceRequestPlan int

const (
	// WorkspaceUsable: the lease is usable; resume the demand.
	WorkspaceUsable WorkspaceRequestPlan = iota
	// WorkspaceUsableWithoutContext: the lease is usable, but its repository
	// context was never read for the model continuation that asks.
	WorkspaceUsableWithoutContext
	// WorkspaceInFlight: an RPC demand found a request in flight, whose owner
	// performs every effect of its grant.
	WorkspaceInFlight
	// WorkspaceLossesExhausted: the call lost its instance too often; resume
	// the demand without a workspace.
	WorkspaceLossesExhausted
	// WorkspaceJoined: the conversation joined the request in flight; the Step
	// that settles it resumes the continuation.
	WorkspaceJoined
	// WorkspaceOpened: a new request was recorded.
	WorkspaceOpened
)

// ChangesWorkspace reports whether the plan changed Workspace, which the
// caller records.
func (plan WorkspaceRequestPlan) ChangesWorkspace() bool { return plan >= WorkspaceLossesExhausted }

// PlanRequest decides one demand of the cold path: resumeAt is the waiting
// continuation ("" for an RPC demand), lost the request the transport proved
// unreachable for callID, and requestID the identity a new request takes. A
// new request is durable before its command is sent.
func (ws *SessionWorkspace) PlanRequest(resumeAt Continuation, lost string, callID conversation.CallID, requestID string) WorkspaceRequestPlan {
	switch {
	case ws.countLoss(lost, callID):
		return WorkspaceLossesExhausted
	case ws.IsUsable() && ws.RepositoryContext == "" && resumeAt != "" && resumeAt != ContinueRouteTool:
		return WorkspaceUsableWithoutContext
	case ws.IsUsable():
		return WorkspaceUsable
	case ws.State == WorkspaceRequested && resumeAt == "":
		return WorkspaceInFlight
	case ws.State == WorkspaceRequested:
		ws.join(resumeAt)
		return WorkspaceJoined
	}
	ws.request(requestID)
	return WorkspaceOpened
}

// IsUsable reports whether new tools may be dispatched on the accepted lease:
// the request is READY, so no command reclaimed its instance.
func (ws SessionWorkspace) IsUsable() bool {
	return ws.State == WorkspaceReady && ws.Lease.Validate() == nil
}

// Summary is the lifecycle read of the workspace's request side.
func (ws SessionWorkspace) Summary() WorkspaceSummary {
	return WorkspaceSummary{State: ws.State, RequestID: ws.RequestID, Failure: ws.Failure, FailureDetail: ws.FailureDetail, SandboxDeadline: ws.SandboxDeadline}
}

// StepExecutionParts splits "<Step>-<n>" into the registered Step name and its
// execution number.
func StepExecutionParts(stepExecutionID string) (string, string, bool) {
	separator := strings.LastIndex(stepExecutionID, "-")
	if separator <= 0 || separator == len(stepExecutionID)-1 {
		return "", "", false
	}
	name, number := stepExecutionID[:separator], stepExecutionID[separator+1:]
	if strings.Trim(number, "0123456789") != "" {
		return "", "", false
	}
	return name, number, true
}

// AdmittedLease is the lease of the wave marked in use, which keeps it for the
// wave after a revocation: no instance a wave uses is reclaimed.
func AdmittedLease(use WorkspaceUse, ws SessionWorkspace) *workspace.Lease {
	if !use.Active || use.RequestID == "" || use.RequestID != ws.Lease.RequestID {
		return nil
	}
	lease := ws.Lease
	return &lease
}
