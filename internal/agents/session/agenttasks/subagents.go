// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package agenttasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/agents/toolfailure"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/flowid"
)

// A subagent is a background agent of the Session's main agent: one
// SubagentFlow execution that works on a bounded task in its own git worktree
// and reports with a task notification. Its task ID is agent-<agent ID>, and
// the agent ID derives from the Session's FlowID and the start_agent call
// (flowid.AgentTaskID), so every retry of the call names the same subagent.
// The Session keeps one record per subagent in its AttributeMap Subagents
// under the agent ID; a record holds no prompt.

// SubagentKind is what a subagent is started for, with the limits, the
// default effort and the system prompt of the kind. It is a struct that
// string literals cannot convert to: the kinds below are its only values,
// persisted by their names.
type SubagentKind struct{ definition *subagentKind }

type subagentKind struct {
	name string
	// modelCalls and duration are the kind's limits: the last call before
	// modelCalls asks for the report, and the duration ends the subagent.
	modelCalls int
	duration   time.Duration
	// effort is the base effort of the kind's model calls unless the server
	// configures another; raisesEffort offers the kind set_effort.
	effort       conversation.Effort
	raisesEffort bool
	systemPrompt string
	// workspace is the repository its root is a worktree of; editsFiles
	// offers it edit and write, and readsPlatform the platform readers.
	workspace                 subagentWorkspace
	editsFiles, readsPlatform bool
}

// subagentWorkspace is the repository a subagent's root is a worktree of: the
// application of the Session's workspace, or the upstream connectors library
// that a connector_develop subagent contributes to.
type subagentWorkspace struct{ name string }

var (
	workspaceApplication       = subagentWorkspace{"application"}
	workspaceConnectorsLibrary = subagentWorkspace{"connectors_library"}
)

func (workspace subagentWorkspace) String() string { return workspace.name }

var (
	// SubagentInvestigate answers a question about the code or its
	// dependencies.
	SubagentInvestigate = SubagentKind{&subagentKind{name: "investigate", modelCalls: 40, duration: 20 * time.Minute,
		effort: conversation.EffortLow, systemPrompt: conversation.SubagentInvestigatePrompt, workspace: workspaceApplication, editsFiles: true, readsPlatform: true}}
	// SubagentChange makes a bounded change and checks it.
	SubagentChange = SubagentKind{&subagentKind{name: "change", modelCalls: 80, duration: 45 * time.Minute,
		effort: conversation.EffortMedium, raisesEffort: true, systemPrompt: conversation.SubagentChangePrompt, workspace: workspaceApplication, editsFiles: true, readsPlatform: true}}
	// SubagentConnectorSelect chooses the connectors that meet the
	// requirements, reading the catalog and module sources without editing.
	SubagentConnectorSelect = SubagentKind{&subagentKind{name: "connector_select", modelCalls: 25, duration: 10 * time.Minute,
		effort: conversation.EffortMedium, systemPrompt: conversation.SubagentConnectorSelectPrompt, workspace: workspaceApplication}}
	// SubagentConnectorDevelop builds one missing connector or operation, or
	// fixes an existing connector, in a worktree of the connectors library.
	SubagentConnectorDevelop = SubagentKind{&subagentKind{name: "connector_develop", modelCalls: 80, duration: 45 * time.Minute,
		effort: conversation.EffortHigh, raisesEffort: true, systemPrompt: conversation.SubagentConnectorDevelopPrompt, workspace: workspaceConnectorsLibrary, editsFiles: true}}
)

// SubagentKinds are every kind, as start_agent offers them.
var SubagentKinds = []SubagentKind{SubagentInvestigate, SubagentChange, SubagentConnectorSelect, SubagentConnectorDevelop}

// String is the persisted spelling of the kind.
func (kind SubagentKind) String() string { return kind.definition.name }

// ModelCalls is how many model calls a subagent of the kind makes; the last
// one writes the report.
func (kind SubagentKind) ModelCalls() int { return kind.definition.modelCalls }

// Duration is how long a subagent of the kind runs before it is finished as
// timed out.
func (kind SubagentKind) Duration() time.Duration { return kind.definition.duration }

// DefaultEffort is the base effort of the kind when the server configures none.
func (kind SubagentKind) DefaultEffort() conversation.Effort { return kind.definition.effort }

// RaisesEffort reports whether the kind may raise its effort with set_effort.
func (kind SubagentKind) RaisesEffort() bool { return kind.definition.raisesEffort }

// SystemPrompt is the system prompt of every subagent of the kind.
func (kind SubagentKind) SystemPrompt() string { return kind.definition.systemPrompt }

// EditsFiles reports whether the kind may edit and write files in its root.
func (kind SubagentKind) EditsFiles() bool { return kind.definition.editsFiles }

// ReadsPlatform reports whether the kind may read the Project's delivery,
// platform Flows and Live runs.
func (kind SubagentKind) ReadsPlatform() bool { return kind.definition.readsPlatform }

// ContributesConnector reports whether the kind contributes a connector to
// the library, so that its start names the connector (ConnectorTarget).
func (kind SubagentKind) ContributesConnector() bool {
	return kind.definition.workspace == workspaceConnectorsLibrary
}

// MarshalText writes the persisted spelling.
func (kind SubagentKind) MarshalText() ([]byte, error) { return []byte(kind.String()), nil }

// UnmarshalText reads a persisted spelling.
func (kind *SubagentKind) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, kind, SubagentKind.String, SubagentKinds...)
}

// ParseSubagentKind is the kind spelled name, as start_agent names it.
func ParseSubagentKind(name string) (SubagentKind, bool) {
	for _, kind := range SubagentKinds {
		if kind.String() == name {
			return kind, true
		}
	}
	return SubagentKind{}, false
}

// SubagentStatus is where a subagent's record stands: starting until the
// subagent's first Step admitted it, running, finished with the outcome it
// reported, and closed once its changes were merged or discarded. It is a
// struct like SubagentKind.
type SubagentStatus struct{ definition *subagentStatus }

type subagentStatus struct {
	name string
	// finished: the subagent ended; closed: its changes were also merged or
	// discarded, so nothing about it is open any more.
	finished, closed bool
}

var (
	SubagentStarting  = SubagentStatus{&subagentStatus{name: "starting"}}
	SubagentRunning   = SubagentStatus{&subagentStatus{name: "running"}}
	SubagentCompleted = SubagentStatus{&subagentStatus{name: conversation.TaskEndCompleted, finished: true}}
	SubagentFailed    = SubagentStatus{&subagentStatus{name: conversation.TaskEndFailed, finished: true}}
	SubagentStopped   = SubagentStatus{&subagentStatus{name: conversation.TaskEndStopped, finished: true}}
	SubagentTimedOut  = SubagentStatus{&subagentStatus{name: conversation.TaskEndTimedOut, finished: true}}
	SubagentMerged    = SubagentStatus{&subagentStatus{name: "merged", finished: true, closed: true}}
	SubagentDiscarded = SubagentStatus{&subagentStatus{name: "discarded", finished: true, closed: true}}
)

// subagentStatuses are every status of a record.
var subagentStatuses = []SubagentStatus{SubagentStarting, SubagentRunning, SubagentCompleted, SubagentFailed, SubagentStopped, SubagentTimedOut, SubagentMerged, SubagentDiscarded}

// SubagentOutcomes are the statuses a subagent reports when it ends.
var SubagentOutcomes = []SubagentStatus{SubagentCompleted, SubagentFailed, SubagentStopped, SubagentTimedOut}

// String is the persisted spelling of the status, which a task notification
// also carries.
func (status SubagentStatus) String() string { return status.definition.name }

// Finished reports whether the subagent ended.
func (status SubagentStatus) Finished() bool { return status.definition.finished }

// Closed reports whether the changes of the ended subagent were merged or
// discarded.
func (status SubagentStatus) Closed() bool { return status.definition.closed }

// MarshalText writes the persisted spelling.
func (status SubagentStatus) MarshalText() ([]byte, error) { return []byte(status.String()), nil }

// UnmarshalText reads a persisted spelling.
func (status *SubagentStatus) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, status, SubagentStatus.String, subagentStatuses...)
}

// The bounds of the Session's subagents. At most maximumRunningSubagents run
// at once and at most maximumSubagentsWithoutUser start between two messages of
// the user. A record that is still unfinished subagentLostGrace after its
// kind's duration counts as lost: it no longer holds the Sandbox or a running
// place, and a late report still updates it.
const (
	maximumRunningSubagents     = 3
	maximumSubagentsWithoutUser = 6
	retainedFinishedSubagents   = 50
	subagentLostGrace           = time.Hour
	// maximumSubagentSummaryBytes keeps a record's summary short.
	maximumSubagentSummaryBytes = 400
)

// SubagentRecord is the value of one Subagents instance, at most about 2 KiB:
// what the user sees of the subagent and what the main agent may merge. It
// holds no prompt text and no report: the subagent keeps both.
type SubagentRecord struct {
	AgentID string              `json:"agent_id"`
	CallID  conversation.CallID `json:"call_id"`
	Kind    SubagentKind        `json:"kind"`
	// Description is the start_agent description, shown to the user.
	Description string `json:"description"`
	// Fingerprint identifies the admitted call (SubagentCall.Fingerprint),
	// which the subagent's admission must name.
	Fingerprint string `json:"fingerprint"`
	// BaseCommit is the snapshot the worktree started from, recorded with the
	// admission.
	BaseCommit string         `json:"base_commit,omitempty"`
	Status     SubagentStatus `json:"status"`
	StartedAt  time.Time      `json:"started_at"`
	AdmittedAt time.Time      `json:"admitted_at,omitzero"`
	FinishedAt time.Time      `json:"finished_at,omitzero"`
	// Summary is the line of the outcome the user sees.
	Summary string                    `json:"summary,omitempty"`
	Changes *conversation.TaskChanges `json:"changes,omitempty"`
	// ChangesUnsaved marks a subagent whose end could not save its worktree,
	// whose changes merge_agent_changes saves from the worktree first.
	ChangesUnsaved bool `json:"changes_unsaved,omitempty"`
	// Merge is what merge_agent_changes did with the changes.
	Merge *subagentMerge `json:"merge,omitempty"`
	// Connector is the connector a connector_develop subagent contributes to.
	Connector *ConnectorTarget `json:"connector,omitempty"`
}

// subagentMerge is the receipt of merge_agent_changes as a record keeps it.
type subagentMerge struct {
	Outcome   git.MergeOutcome `json:"outcome"`
	Applied   int              `json:"applied"`
	Conflicts int              `json:"conflicts"`
	Unchanged int              `json:"unchanged"`
}

// NewSubagentRecord is the STARTING record of a start_agent call the Session
// admitted.
func NewSubagentRecord(agentID string, callID conversation.CallID, call SubagentCall, fingerprint string, now time.Time) SubagentRecord {
	return SubagentRecord{AgentID: agentID, CallID: callID, Kind: call.Kind, Description: call.Description, Fingerprint: fingerprint,
		Status: SubagentStarting, StartedAt: now, Connector: call.Connector}
}

// TaskID is the task ID the model and the user read.
func (record SubagentRecord) TaskID() string { return flowid.SubagentTaskID(record.AgentID) }

// Admit answers the admission of the record's subagent. Only the call the
// record admitted, named by its fingerprint, is admitted: a STARTING record
// becomes RUNNING with the snapshot while the Session is open, that is ACTIVE
// with no Stop pending, and a RUNNING one is admitted again. A STARTING record
// of a closed Session is stopped before its subagent started. It returns the
// record, the answer, which marks a denial of a record a Stop ended, and
// whether the record changed.
func (record SubagentRecord) Admit(admission SubagentAdmission, isOpen bool, now time.Time) (SubagentRecord, SubagentAdmissionAnswer, bool) {
	switch {
	case record.Fingerprint != admission.Fingerprint:
		return record, SubagentAdmissionAnswer{}, false
	case record.Status == SubagentStarting && isOpen:
		record.Status, record.BaseCommit, record.AdmittedAt = SubagentRunning, admission.BaseCommit, now
		return record, SubagentAdmissionAnswer{Admitted: true}, true
	case record.Status == SubagentStarting:
		return record.StopBeforeStart(now), SubagentAdmissionAnswer{Stopped: true}, true
	}
	return record, SubagentAdmissionAnswer{Admitted: record.Status == SubagentRunning, Stopped: record.Status == SubagentStopped && record.AdmittedAt.IsZero()}, false
}

// StopBeforeStart records a subagent that a Stop or the Session's end
// overtook before its admission: it never ran, and its admission is denied.
func (record SubagentRecord) StopBeforeStart(now time.Time) SubagentRecord {
	record.Status, record.FinishedAt = SubagentStopped, now
	record.Summary = fmt.Sprintf("Subagent %q was stopped before it started.", comm.TruncateUTF8(record.Description, maximumSubagentSummaryBytes/2))
	return record
}

// FailStart records a STARTING subagent whose start_agent call ran out of
// retries: its admission is denied, so it never runs. A record that was
// admitted meanwhile keeps running.
func (record SubagentRecord) FailStart(now time.Time) (SubagentRecord, bool) {
	if record.Status != SubagentStarting {
		return record, false
	}
	record.Status, record.FinishedAt = SubagentFailed, now
	record.Summary = fmt.Sprintf("Subagent %q could not be started.", comm.TruncateUTF8(record.Description, maximumSubagentSummaryBytes/2))
	return record, true
}

// Close records what merge_agent_changes did with the changes of the ended
// subagent: discarded, or merged with its counts.
func (record SubagentRecord) Close(receipt git.MergeReceipt) SubagentRecord {
	record.Status = SubagentMerged
	if receipt.Outcome == git.MergeDiscarded {
		record.Status = SubagentDiscarded
	}
	record.Merge = &subagentMerge{Outcome: receipt.Outcome, Applied: len(receipt.Applied), Conflicts: len(receipt.Conflicts), Unchanged: len(receipt.Unchanged)}
	return record
}

// IsLost reports whether an unfinished record outlived its kind's duration by
// the lost grace: its subagent never reported.
func (record SubagentRecord) IsLost(now time.Time) bool {
	return !record.Status.Finished() && now.After(record.StartedAt.Add(record.Kind.Duration()+subagentLostGrace))
}

// isUnfinished reports whether the subagent may still run: it neither
// reported nor counts as lost.
func (record SubagentRecord) isUnfinished(now time.Time) bool {
	return !record.Status.Finished() && !record.IsLost(now)
}

// Finish records the outcome a subagent's task notification reports. A record
// that already ended keeps its outcome, so a late or repeated report changes
// nothing, and a report of an unknown status is refused.
func (record SubagentRecord) Finish(notification conversation.SessionNotification) (SubagentRecord, error) {
	var status SubagentStatus
	if err := status.UnmarshalText([]byte(notification.TaskStatus)); err != nil || !slices.Contains(SubagentOutcomes, status) {
		return record, fmt.Errorf("the task notification %s reports the status %q, which no subagent reports", notification.ID, notification.TaskStatus)
	}
	if record.Status.Finished() {
		return record, nil
	}
	record.Status, record.FinishedAt, record.Changes, record.ChangesUnsaved = status, notification.OccurredAt, notification.Changes, notification.ChangesUnsaved
	record.Summary = comm.TruncateUTF8(notification.Summary, maximumSubagentSummaryBytes)
	return record, nil
}

// HoldsNoChanges reports whether the subagent left nothing to merge: it
// reported its end, saved no changes and none stayed unsaved in its worktree.
// A lost record, which never reported, may hold changes.
func (record SubagentRecord) HoldsNoChanges() bool {
	return record.Status.Finished() && record.Changes == nil && !record.ChangesUnsaved
}

// IsEnded reports whether the subagent ended for the Session: it reported, or
// it counts as lost.
func (record SubagentRecord) IsEnded(now time.Time) bool {
	return record.Status.Finished() || record.IsLost(now)
}

// SortedSubagents lists the records in start order.
func SortedSubagents(records map[string]SubagentRecord) []SubagentRecord {
	return slices.SortedFunc(maps.Values(records), func(left, right SubagentRecord) int {
		if order := left.StartedAt.Compare(right.StartedAt); order != 0 {
			return order
		}
		return strings.Compare(left.AgentID, right.AgentID)
	})
}

// UnfinishedSubagents lists the records whose subagents may still run, in
// start order.
func UnfinishedSubagents(records map[string]SubagentRecord, now time.Time) []SubagentRecord {
	return slices.DeleteFunc(SortedSubagents(records), func(record SubagentRecord) bool { return !record.isUnfinished(now) })
}

// StartingSubagents are the agent IDs of the records whose subagents were not
// admitted yet, which a Stop stops before they start.
func StartingSubagents(records map[string]SubagentRecord) []string {
	var starting []string
	for _, record := range SortedSubagents(records) {
		if record.Status == SubagentStarting {
			starting = append(starting, record.AgentID)
		}
	}
	return starting
}

// ExpiredSubagents are the records to delete before a new one is added: the
// oldest finished ones beyond retainedFinishedSubagents. Unfinished records
// are never deleted.
func ExpiredSubagents(records map[string]SubagentRecord) []string {
	var finished []SubagentRecord
	for _, record := range records {
		if record.Status.Finished() {
			finished = append(finished, record)
		}
	}
	slices.SortFunc(finished, func(left, right SubagentRecord) int { return left.FinishedAt.Compare(right.FinishedAt) })
	var expired []string
	for len(finished)-len(expired) > retainedFinishedSubagents-1 {
		expired = append(expired, finished[len(expired)].AgentID)
	}
	return expired
}

// AdmitSubagentStart decides whether the subagent of call may start: fewer
// than maximumSubagentsWithoutUser may have started since the user last
// wrote, fewer than maximumRunningSubagents may run, lost ones not counted,
// and a connector_develop call must pass admitConnectorTarget. The budget is
// checked first: once it is spent, waiting for a subagent does not help. A
// refusal is the call's failure, with no effect: its code and what it found.
func AdmitSubagentStart(records map[string]SubagentRecord, call SubagentCall, startedWithoutUser int, slugs []string, now time.Time) (toolfailure.Code, string, bool) {
	switch running := len(UnfinishedSubagents(records, now)); {
	case startedWithoutUser >= maximumSubagentsWithoutUser:
		return toolfailure.SubagentBudgetReached, fmt.Sprintf("%d subagents were started since the user's last message", startedWithoutUser), false
	case running >= maximumRunningSubagents:
		return toolfailure.SubagentLimitReached, fmt.Sprintf("%d subagents run, the most that may run at once", running), false
	case call.Connector != nil:
		return admitConnectorTarget(records, slugs, *call.Connector, now)
	}
	return toolfailure.Code{}, "", true
}

// SubagentIdentity names one subagent of the Project's Session.
type SubagentIdentity struct {
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
}

// FlowID is the FlowID of the subagent's SubagentFlow.
func (id SubagentIdentity) FlowID() string {
	return flowid.SubagentFlowID(id.ProjectID, id.AgentID)
}

// TaskID is the subagent's task ID.
func (id SubagentIdentity) TaskID() string { return flowid.SubagentTaskID(id.AgentID) }

// SubagentCall is what one start_agent call asks for, as the Session admitted
// it; Connector names the connector of a connector_develop call.
type SubagentCall struct {
	Kind        SubagentKind     `json:"kind"`
	Description string           `json:"description"`
	Prompt      string           `json:"prompt"`
	Connector   *ConnectorTarget `json:"connector,omitempty"`
}

// Fingerprint identifies the admitted call of the Session sessionFlowID that
// starts agentID: its kind, its description, its prompt's digest and, for a
// connector_develop call, its connector. The record holds it, and the
// subagent's admission must name it.
func (call SubagentCall) Fingerprint(sessionFlowID, agentID string) string {
	digest := sha256.Sum256([]byte(call.Prompt))
	if call.Connector == nil {
		return flowid.Fingerprint(struct{ SessionFlowID, AgentID, Kind, Description, PromptSHA256 string }{
			sessionFlowID, agentID, call.Kind.String(), call.Description, hex.EncodeToString(digest[:])})
	}
	return flowid.Fingerprint(struct{ SessionFlowID, AgentID, Kind, Description, PromptSHA256, Slug, Contribution string }{
		sessionFlowID, agentID, call.Kind.String(), call.Description, hex.EncodeToString(digest[:]), call.Connector.Slug, call.Connector.Contribution.String()})
}

// SubagentStart is the complete start of a subagent: the admitted call with
// its fingerprint, the snapshot its worktree starts from, and the Session's
// model, HEAD and accepted lease at that moment, which the subagent keeps.
type SubagentStart struct {
	Identity    SubagentIdentity   `json:"identity"`
	Call        SubagentCall       `json:"call"`
	Fingerprint string             `json:"fingerprint"`
	BaseCommit  string             `json:"base_commit"`
	Model       conversation.Model `json:"model"`
	HeadCommit  string             `json:"head_commit,omitempty"`
	Lease       workspace.Lease    `json:"lease"`
	// Branch is the library branch of a connector_develop subagent, whose
	// BaseCommit is the branch's head it started from.
	Branch string `json:"branch,omitempty"`
}

// RequestID is the Start RequestID of the subagent: the complete logical
// request, the call's fingerprint and the snapshot, without the model, the
// HEAD, the lease or a time, so a retry that carries a fresher lease joins the
// same execution.
func (start SubagentStart) RequestID() string {
	return flowid.Fingerprint(struct{ Fingerprint, BaseCommit string }{start.Fingerprint, start.BaseCommit})
}

// SubagentAdmission is what a subagent's first Step asks its Session: the
// Session admits only the STARTING record of the agent with this fingerprint,
// and records the snapshot.
type SubagentAdmission struct {
	AgentID     string `json:"agent_id"`
	Fingerprint string `json:"fingerprint"`
	BaseCommit  string `json:"base_commit"`
}

// SubagentAdmissionAnswer is the Session's answer to a subagent's admission:
// whether it may run, and for a denied one whether a Stop or the Session's end
// stopped its record before it started, which the subagent reports, because
// start_agent may already have answered that it started.
type SubagentAdmissionAnswer struct {
	Admitted bool `json:"admitted"`
	Stopped  bool `json:"stopped,omitempty"`
}

// AgentTaskLauncher is how the Session starts and stops its subagents: through
// the Session Service, which fronts the SubagentFlow, because the Session's
// package never imports it.
type AgentTaskLauncher interface {
	// StartSubagent idempotently starts the SubagentFlow of an admitted
	// record; a repeated start joins its execution, also a closed one.
	StartSubagent(ctx context.Context, start SubagentStart) error
	// StopSubagent asks the subagent to stop, which saves its changes and
	// reports, and answers whether it has ended: also when it never started
	// or its execution stopped. A repeated stop is harmless.
	StopSubagent(ctx context.Context, id SubagentIdentity) (bool, error)
}

// SubagentNotification is the task notification of a subagent that ended with
// status: a summary for the user, its report for the model, bounded so that
// the notification stays within conversation.MaximumSubagentReportBytes, and
// its saved changes, or unsaved for changes its end could not save, and the
// contribution of a completed connector_develop subagent. Its ID
// repeats on every retry of the report.
func SubagentNotification(id SubagentIdentity, status SubagentStatus, description, report string, changes *conversation.TaskChanges, unsaved bool, at time.Time,
	contribution *conversation.ConnectorContribution,
) conversation.SessionNotification {
	notification := conversation.SessionNotification{
		ID: fmt.Sprintf("task/%s/%s", id.TaskID(), status), Kind: conversation.NotificationKindTask, Source: conversation.NotificationSourceSubagent,
		OccurredAt: at, TaskID: id.TaskID(), TaskStatus: status.String(), Summary: subagentSummary(status, description, changes, unsaved, contribution),
		Report: report, Changes: changes, ChangesUnsaved: unsaved, Contribution: contribution,
	}
	const marker = "\n[report truncated]"
	for overflow := len(notification.Text()) - conversation.MaximumSubagentReportBytes; overflow > 0; overflow = len(notification.Text()) - conversation.MaximumSubagentReportBytes {
		notification.Report = comm.TruncateUTF8(notification.Report, max(0, len(notification.Report)-overflow-len(marker)-16)) + marker
	}
	return notification
}

// subagentSummary is the line the user sees of a subagent's end, which never
// claims that changes the subagent could not save are none.
func subagentSummary(status SubagentStatus, description string, changes *conversation.TaskChanges, unsaved bool, contribution *conversation.ConnectorContribution) string {
	description = comm.TruncateUTF8(description, maximumSubagentSummaryBytes/2)
	var summary string
	switch status {
	case SubagentCompleted:
		summary = fmt.Sprintf("Subagent %q finished.", description)
	case SubagentStopped:
		summary = fmt.Sprintf("Subagent %q was stopped.", description)
	case SubagentTimedOut:
		summary = fmt.Sprintf("Subagent %q reached its time limit.", description)
	default:
		summary = fmt.Sprintf("Subagent %q failed.", description)
	}
	switch {
	case contribution != nil:
		return summary + fmt.Sprintf(" It contributed to the connector %s on the branch %s, %s.", contribution.Slug, contribution.Branch, contribution.Location.Description())
	case unsaved:
		return summary + " Its changes could not be saved; they stay in its worktree."
	case changes == nil || changes.Files == 0:
		return summary + " It changed no files."
	}
	return summary + fmt.Sprintf(" It changed %d file(s), which wait to be merged.", changes.Files)
}
