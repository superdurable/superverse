// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package subagent

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"

	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/agents/sandbox/workspace/git"
	"github.com/superdurable/superversev2/internal/agents/session/agenttasks"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
)

// FlowStatus is where a SubagentFlow execution stands: initializing until its
// Session admitted it, running its loop, finishing while it saves its
// worktree, and then completed, failed, stopped, timed out, or denied by its
// Session without any effect. It is a struct that string literals cannot
// convert to; the indexed SubagentStatus Attribute holds its name.
type FlowStatus struct{ definition *statusDefinition }

type statusDefinition struct {
	name string
	// outcome is the status a subagent's report and its Session's record
	// carry; a status without one is not an end, or a denial, which reports
	// nothing.
	outcome agenttasks.SubagentStatus
	ended   bool
}

var (
	StatusInitializing = FlowStatus{&statusDefinition{name: "INITIALIZING"}}
	StatusRunning      = FlowStatus{&statusDefinition{name: "RUNNING"}}
	StatusFinishing    = FlowStatus{&statusDefinition{name: "FINISHING"}}
	StatusCompleted    = FlowStatus{&statusDefinition{name: "COMPLETED", outcome: agenttasks.SubagentCompleted, ended: true}}
	StatusFailed       = FlowStatus{&statusDefinition{name: "FAILED", outcome: agenttasks.SubagentFailed, ended: true}}
	StatusStopped      = FlowStatus{&statusDefinition{name: "STOPPED", outcome: agenttasks.SubagentStopped, ended: true}}
	StatusTimedOut     = FlowStatus{&statusDefinition{name: "TIMED_OUT", outcome: agenttasks.SubagentTimedOut, ended: true}}
	StatusDenied       = FlowStatus{&statusDefinition{name: "DENIED", ended: true}}
)

var flowStatuses = []FlowStatus{StatusInitializing, StatusRunning, StatusFinishing, StatusCompleted, StatusFailed, StatusStopped, StatusTimedOut, StatusDenied}

// String is the persisted spelling of the status.
func (status FlowStatus) String() string { return status.definition.name }

// ended reports whether the subagent ended: it reported, or was denied.
func (status FlowStatus) ended() bool { return status.definition.ended }

// outcome is the status the subagent's report carries; the zero status for a
// status that reports nothing.
func (status FlowStatus) outcome() agenttasks.SubagentStatus { return status.definition.outcome }

func (status FlowStatus) MarshalText() ([]byte, error) { return []byte(status.String()), nil }
func (status *FlowStatus) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, status, FlowStatus.String, flowStatuses...)
}

// statusOf is the status the SubagentStatus Attribute holds; this Flow wrote
// it, so another spelling is a programming error.
func statusOf(name string) FlowStatus {
	var status FlowStatus
	comm.MustDo(status.UnmarshalText([]byte(name)))
	return status
}

// endStatus is the Flow status of a subagent that ends with outcome.
func endStatus(outcome agenttasks.SubagentStatus) FlowStatus {
	for _, status := range flowStatuses {
		if status.definition.outcome == outcome {
			return status
		}
	}
	panic(fmt.Sprintf("the subagent outcome %s has no Flow status", outcome))
}

// Every outcome a subagent reports ends one Flow status.
func init() {
	for _, outcome := range agenttasks.SubagentOutcomes {
		endStatus(outcome)
	}
}

// startRecord is the Start Attribute: the call the Session admitted, its
// fingerprint, the snapshot the worktree starts from, and the Session's model
// and HEAD when it started the subagent. It is immutable and installed with
// the start, together with the Session's accepted lease in Workspace; a
// retried start that carries a fresher lease joins the execution, which keeps
// the first.
type startRecord struct {
	Call        agenttasks.SubagentCall `json:"call"`
	Fingerprint string                  `json:"fingerprint"`
	BaseCommit  string                  `json:"base_commit"`
	Model       conversation.Model      `json:"model"`
	HeadCommit  string                  `json:"head_commit,omitempty"`
	// Branch is the library branch of a connector_develop subagent.
	Branch string `json:"branch,omitempty"`
}

// Config is frozen when the Session admits the subagent: its model, kind and
// base effort and the deadline of its duration limit,
// and the Session's HEAD it reports platform reads for. The system prompt and
// the tool set are frozen in the first segment's prompts.
type Config struct {
	Model      conversation.Model      `json:"model"`
	Kind       agenttasks.SubagentKind `json:"kind"`
	Effort     conversation.Effort     `json:"effort"`
	StartedAt  time.Time               `json:"started_at"`
	Deadline   time.Time               `json:"deadline"`
	HeadCommit string                  `json:"head_commit,omitempty"`
}

// Workspace is the subagent's copy of the lease the Session accepted, its
// root, the snapshot it started from, the repository context every segment
// carries, and the losses of the transport per call, after which it asks the
// Session for a lease again.
type Workspace struct {
	Lease             workspace.Lease     `json:"lease"`
	Root              string              `json:"root"`
	BaseCommit        string              `json:"base_commit"`
	RepositoryContext string              `json:"repository_context,omitempty"`
	LostCallID        conversation.CallID `json:"lost_call_id,omitempty"`
	Losses            int                 `json:"losses,omitempty"`
}

// maximumWorkspaceLosses bounds the lease refreshes on behalf of one call, and
// those of the finish.
const maximumWorkspaceLosses = 2

// countLoss records that the transport proved the lease unreachable for call
// and reports whether the call may ask for another lease.
func (ws *Workspace) countLoss(call conversation.CallID) bool {
	if ws.LostCallID == call {
		ws.Losses++
	} else {
		ws.LostCallID, ws.Losses = call, 1
	}
	return ws.Losses <= maximumWorkspaceLosses
}

// reportDelivery is what became of the report: delivered to the Session, or
// undelivered because the Session ended or the report's wait limit passed.
type reportDelivery struct{ name string }

var (
	reportDelivered   = reportDelivery{"delivered"}
	reportUndelivered = reportDelivery{"undelivered"}
)

func (delivery reportDelivery) String() string { return delivery.name }
func (delivery reportDelivery) MarshalText() ([]byte, error) {
	return []byte(delivery.name), nil
}
func (delivery *reportDelivery) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, delivery, reportDelivery.String, reportDelivered, reportUndelivered)
}

// maximumReportBytes bounds a report the Outcome keeps; the notification fits
// it into its own bound.
const maximumReportBytes = conversation.MaximumSubagentReportBytes

// Outcome is how the subagent ended: its status, its report, the changes it
// saved, why it failed, why its worktree could not be saved, which leaves its
// changes unsaved in the worktree on the volume, and whether its report
// reached the Session.
type Outcome struct {
	Status     FlowStatus                `json:"status"`
	Report     string                    `json:"report,omitempty"`
	Changes    *conversation.TaskChanges `json:"changes,omitempty"`
	Failure    string                    `json:"failure,omitempty"`
	Unsaved    string                    `json:"unsaved,omitempty"`
	FinishedAt time.Time                 `json:"finished_at"`
	Delivery   reportDelivery            `json:"delivery,omitzero"`
	// Contribution is what a completed connector_develop subagent contributed,
	// and Refused why it contributed nothing.
	Contribution *conversation.ConnectorContribution `json:"contribution,omitempty"`
	Refused      string                              `json:"refused,omitempty"`
}

// subagentFinish is the input of the finishing Steps: the outcome to report,
// the report or why the subagent failed, and once FinishSubagent tried to save
// the worktree, its changes or why they stay unsaved; Refreshes counts the
// leases the finish took from the Session after its copy proved unreachable.
type subagentFinish struct {
	Status    agenttasks.SubagentStatus `json:"status"`
	Report    string                    `json:"report,omitempty"`
	Failure   string                    `json:"failure,omitempty"`
	Changes   *conversation.TaskChanges `json:"changes,omitempty"`
	Unsaved   string                    `json:"unsaved,omitempty"`
	Refreshes int                       `json:"refreshes,omitempty"`
	// Contribution and Refused are a develop subagent's, once saved.
	Contribution *conversation.ConnectorContribution `json:"contribution,omitempty"`
	Refused      string                              `json:"refused,omitempty"`
}

// describeEnd is the report of an end: the subagent's own, and for an end
// without it, why it ended with what its last reply said. Changes the end
// could not save lead the report, so that no truncation hides them, and a
// stopped or timed-out subagent claims saved changes only when they were.
func describeEnd(finish subagentFinish) string {
	var lead string
	if finish.Unsaved != "" {
		lead = "Its changes could not be saved: " + finish.Unsaved + ". merge_agent_changes saves what its worktree still holds."
	}
	saved := ""
	if finish.Unsaved == "" {
		saved = "; its changes so far are saved"
	}
	var reason string
	switch finish.Status {
	case agenttasks.SubagentStopped:
		reason = "The subagent was stopped before it finished" + saved + "."
	case agenttasks.SubagentTimedOut:
		reason = "The subagent reached its time limit before it finished" + saved + "."
	case agenttasks.SubagentFailed:
		reason = "The subagent failed: " + finish.Failure + "."
	default:
		if finish.Report == "" {
			reason = "The subagent ended without a report."
		}
	}
	report := finish.Report
	if reason != "" && report != "" {
		report = "Its last reply:\n" + report
	}
	parts := []string{lead, reason, describeContribution(finish), report}
	return comm.TruncateUTF8(strings.Join(slices.DeleteFunc(parts, func(part string) bool { return part == "" }), "\n\n"), maximumReportBytes)
}

// stopAnswer is the result of StopSubagent: whether the subagent has ended.
type stopAnswer struct {
	Ended bool `json:"ended"`
}

// subagentSnapshot is the retained read of a subagent: its identity and task, its
// status, message progress and, once admitted and ended, its
// configuration and outcome. It holds no prompt.
type subagentSnapshot struct {
	Identity     agenttasks.SubagentIdentity `json:"identity"`
	Kind         agenttasks.SubagentKind     `json:"kind"`
	Description  string                      `json:"description"`
	BaseCommit   string                      `json:"base_commit"`
	Status       FlowStatus                  `json:"status"`
	LastSequence conversation.Sequence       `json:"last_sequence"`
	Config       *Config                     `json:"config,omitempty"`
	Outcome      *Outcome                    `json:"outcome,omitempty"`
}

// receipt is the output of a completed SubagentFlow.
type receipt struct {
	Identity agenttasks.SubagentIdentity `json:"identity"`
	Status   FlowStatus                  `json:"status"`
}

// startAttributes are what a subagent starts with, installed atomically with
// StartFlow: its identity, its start, its Project, indexed for the cleanup of
// a deleting Project, its status and its copy of the lease in its worktree.
func startAttributes(start agenttasks.SubagentStart) []dex.InitialAttributeDef {
	attributes := &dexflow.InitialAttributes{}
	dexflow.Add(attributes, identityAttribute, start.Identity)
	dexflow.Add(attributes, startAttribute, startRecord{Call: start.Call, Fingerprint: start.Fingerprint, BaseCommit: start.BaseCommit, Model: start.Model, HeadCommit: start.HeadCommit,
		Branch: start.Branch})
	dexflow.Add(attributes, projectIDAttribute, start.Identity.ProjectID)
	dexflow.Add(attributes, statusAttribute, StatusInitializing.String())
	root := git.AgentWorktreeRoot(start.Identity.AgentID)
	if start.Call.Connector != nil {
		root = git.ContributionWorktreeRoot(start.Identity.AgentID)
	}
	dexflow.Add(attributes, workspaceAttribute, Workspace{Lease: start.Lease, Root: root, BaseCommit: start.BaseCommit})
	return attributes.Values()
}

// The tests name these values.
var (
	ReportDeliveredForTest = reportDelivered
)

// ReportDeliveryForTest is reportDelivery for the tests.
type ReportDeliveryForTest = reportDelivery

// SnapshotForTest is subagentSnapshot for the tests.
type SnapshotForTest = subagentSnapshot
