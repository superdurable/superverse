// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agentloop

import (
	"encoding/json"
	"time"

	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/toolinput"
)

// FlowID uniquely identifies one durable Agent conversation.
type FlowID string

// SessionFlowID is the FlowID of the one Session of projectID.
func SessionFlowID(projectID string) FlowID {
	return FlowID(flowid.SessionFlowID(projectID))
}

// SessionIdentity is the immutable identity a Session starts with: its
// Project, which is its only business identity, its profile and its initial
// HEAD.
type SessionIdentity struct {
	ProjectID  string `json:"projectId"`
	Profile    string `json:"profile"`
	HeadCommit string `json:"headCommit,omitempty"`
}

// SessionStartIdentity is the Start RequestID of the Session of projectID:
// the fingerprint of its complete logical input, the Project, so a retried or
// replayed start joins the accepted execution.
func SessionStartIdentity(projectID string) string {
	return flowid.Fingerprint(struct{ ProjectID string }{projectID})
}

// SessionStatus is the lifecycle of a Session resource: ACTIVE until its
// Project is deleted, then ARCHIVED and read-only. It is an alias of string
// because the indexed SessionStatus Attribute must be a plain string; the
// constants of the alias declare the set.
type SessionStatus = string

const (
	SessionStatusActive   SessionStatus = "ACTIVE"
	SessionStatusArchived SessionStatus = "ARCHIVED"
)

// SessionSummary is the lifecycle-only read used for listing and admission.
type SessionSummary struct {
	Identity    SessionIdentity `json:"identity"`
	Status      SessionStatus   `json:"status"`
	CloseReason string          `json:"closeReason,omitempty"`
	Revision    uint64          `json:"revision"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
	// Workspace reports the accepted Sandbox capability without its lease.
	Workspace WorkspaceSummary `json:"workspace"`
	// WorkspaceUse is an internal read of the authoritative admitted wave.
	// WorkspaceUseKnown is false until initialization has committed
	// WorkspaceUse; only a read value proves a drained wave.
	WorkspaceUse      WorkspaceUse `json:"workspaceUse"`
	WorkspaceUseKnown bool         `json:"workspaceUseKnown"`
}

// ExecutionFailure is why the engine execution of a Session whose retained
// status reads ACTIVE is not running, as a fixed code the API
// reports, never engine text or a RunID. It is a transient read of engine
// liveness, never a durable Session status, cleanup receipt, conversation
// record or recovery identity.
type ExecutionFailure struct{ code string }

var (
	// ExecutionRunning: the execution runs; there is no failure.
	ExecutionRunning = ExecutionFailure{}
	// ExecutionFailed: the execution failed, or timed out on the server.
	ExecutionFailed = ExecutionFailure{code: "SESSION_EXECUTION_FAILED"}
	// ExecutionCanceled: the execution was canceled.
	ExecutionCanceled = ExecutionFailure{code: "SESSION_EXECUTION_CANCELED"}
	// ExecutionTerminated: an operator terminated the execution.
	ExecutionTerminated = ExecutionFailure{code: "SESSION_EXECUTION_TERMINATED"}
	// ExecutionClosed: the execution completed, so it takes no command.
	ExecutionClosed = ExecutionFailure{code: "SESSION_EXECUTION_CLOSED"}
)

// String is the code the API reports, empty for a running execution.
func (failure ExecutionFailure) String() string { return failure.code }

// SessionSnapshot reads lifecycle and chat from one committed Flow invocation.
// Agent is absent until initialization has committed AgentState.
type SessionSnapshot struct {
	ExecutionFailure ExecutionFailure `json:"-"`
	Session          SessionSummary   `json:"session"`
	Agent            *AgentSnapshot   `json:"agent,omitempty"`
}

// SessionScopeInput binds an internal cleanup RPC to its owner.
type SessionScopeInput struct {
	ProjectID string `json:"projectId"`
}

// SessionCleanupReceipt is the terminal ownership handoff for project deletion.
type SessionCleanupReceipt struct {
	ProjectID, Reason string
	// Only a denied first admission carries a terminal snapshot. Ordinary entities
	// remain active and are read through their typed RPC until project deletion.
	Snapshot *SessionSummary `json:"snapshot,omitempty"`
}

// CloseReason is why a Session closes: the reason of a command that closes it,
// or one the Session records itself. The Session's CloseReason, a stop request
// and a cleanup receipt hold its spelling as a string.
type CloseReason struct{ name string }

var (
	// CloseReasonAdmissionDenied archives a Session its Project refused to admit.
	CloseReasonAdmissionDenied = CloseReason{name: "admission_denied"}
	// CloseReasonProjectCleanupComplete archives and completes a Session of a
	// deleting Project, which releases the Session's resources itself.
	CloseReasonProjectCleanupComplete = CloseReason{name: "project_cleanup_complete"}
)

// String is the persisted spelling of the reason.
func (reason CloseReason) String() string { return reason.name }

// ProfileSessionV1 is the one profile a Session Agent starts with.
const ProfileSessionV1 = "session-v1"

// maximumAutomaticHandoffContinuations bounds how often one turn is continued
// after a response stops with changed, unhanded-off source.
const maximumAutomaticHandoffContinuations = 3

// PreviewHandoff is the newest commit the Session hands to its Preview: the
// verified HEAD of a succeeded, pushed and unblocked commit_and_push, at the
// conversation Sequence of its tool result. PublishedSequence is the newest
// handoff that a successful tool result published, so a handoff is
// published once.
type PreviewHandoff struct {
	// Deployment input captures the accepted workspace request when published. Old platform
	// branches without it retain their pre-existing read-on-execute behavior.
	WorkspaceRequestID string                `json:"workspaceRequestId,omitempty"`
	Session            *SessionIdentity      `json:"session,omitempty"`
	Commit             string                `json:"commit,omitempty"`
	Sequence           conversation.Sequence `json:"sequence,omitempty"`
	PublishedSequence  conversation.Sequence `json:"publishedSequence,omitempty"`
}

// Unpublished reports whether a successful tool result has yet to publish the handoff.
func (handoff PreviewHandoff) Unpublished() bool { return handoff.Sequence > handoff.PublishedSequence }

// PreviewAutoDeployRequest turns the Preview's deployment of each pushed
// commit on or off.
type PreviewAutoDeployRequest struct {
	Actor     *conversation.Actor `json:"actor,omitempty"`
	IsEnabled bool                `json:"is_enabled"`
}

// HandOff records the commit that the result of call at sequence hands off
// and reports whether it is a new handoff: only a succeeded, pushed and
// unblocked commit_and_push hands off its verified HEAD, and a push of the
// commit already handed off adds none.
func (handoff *PreviewHandoff) HandOff(call conversation.ToolCall, result ToolExecutionResult, sequence conversation.Sequence) bool {
	if _, isHandedOff := sourceChange(call, result); !isHandedOff || !flowid.IsCommitSHA(result.HeadCommit) || result.HeadCommit == handoff.Commit {
		return false
	}
	handoff.Commit, handoff.Sequence = result.HeadCommit, sequence
	return true
}

// BuildsDesign reports whether a recorded tool result completes the build of
// the confirmed Flow design: a succeeded, pushed and unblocked commit_and_push,
// which the agent calls only when the application, its tests and its trace are
// complete.
func BuildsDesign(call conversation.ToolCall, result ToolExecutionResult) bool {
	_, isHandedOff := sourceChange(call, result)
	return isHandedOff && flowid.IsCommitSHA(result.HeadCommit)
}

// WrittenSource is the file a recorded result of a succeeded write or edit
// changed and the text the call wrote there: the whole content, or the
// new_string that replaced old_string. The call's arguments, which the
// succeeded call decoded, hold both.
func WrittenSource(call conversation.ToolCall, result ToolExecutionResult) (string, string, bool) {
	if isChanged, _ := sourceChange(call, result); !isChanged {
		return "", "", false
	}
	switch call.Name {
	case conversation.ToolWrite:
		input, err := toolinput.Write.Decode(call.Arguments.String())
		return input.Path, input.Content, err == nil
	case conversation.ToolEdit:
		input, err := toolinput.Edit.Decode(call.Arguments.String())
		return input.Path, input.NewString, err == nil
	default:
		return "", "", false
	}
}

// sourceChange reports how a recorded tool result moves the turn's source
// handoff: a successful edit or write changes source, and so does a merge of a
// subagent's changes that applied a file; a successful commit_and_push with
// blocked=false hands it off. A blocked commit is a partial checkpoint and
// hands off nothing.
func sourceChange(call conversation.ToolCall, result ToolExecutionResult) (isChanged, isHandedOff bool) {
	if result.Outcome != conversation.ToolOutcomeSucceeded {
		return false, false
	}
	switch call.Name {
	case conversation.ToolEdit, conversation.ToolWrite:
		return true, false
	case conversation.ToolMergeAgentChanges:
		var report struct {
			Result git.MergeOutcome `json:"result"`
		}
		isMerged := json.Unmarshal([]byte(result.Content), &report) == nil && (report.Result == git.MergeMerged || report.Result == git.MergeMergedWithConflicts)
		return isMerged, false
	case conversation.ToolCommitAndPush:
		return false, !isBlockedCommit(call)
	default:
		return false, false
	}
}

// isBlockedCommit reports whether a commit_and_push call asked for a blocked
// checkpoint. Its result does not echo the arguments, so the call's own
// arguments, which a succeeded call decoded, decide.
func isBlockedCommit(call conversation.ToolCall) bool {
	var arguments struct {
		Blocked bool `json:"blocked"`
	}
	return json.Unmarshal([]byte(call.Arguments.String()), &arguments) == nil && arguments.Blocked
}
