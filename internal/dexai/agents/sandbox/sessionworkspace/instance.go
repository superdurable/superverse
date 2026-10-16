// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package sessionworkspace

import (
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
)

// InstanceState is what exists of the Session's current Sandbox instance, of
// which the SessionWorkspaceFlow is the single owner. It is a struct that
// string literals cannot convert to: the states below are its only values,
// and the zero InstanceState is InstanceNone.
type InstanceState struct{ definition *instanceStateDefinition }

type instanceStateDefinition struct {
	name            string
	workspaceStatus apiStatus
}

// apiStatus is the API WorkspaceStatus spelling of an instance state, which
// names the reclamation CLOSING and its end CLOSED.
type apiStatus = string

const (
	workspaceStatusProvisioning apiStatus = "PROVISIONING"
	workspaceStatusReady        apiStatus = "READY"
	workspaceStatusClosing      apiStatus = "CLOSING"
	workspaceStatusClosed       apiStatus = "CLOSED"
	workspaceStatusFailed       apiStatus = "FAILED"
)

var (
	// InstanceNone: the Session never had an instance.
	InstanceNone = InstanceState{}
	// InstanceProvisioning: a provisioning of InstanceID is, or may still be,
	// running; its outcome is not recorded yet.
	InstanceProvisioning = InstanceState{&instanceStateDefinition{"provisioning", workspaceStatusProvisioning}}
	// InstanceReady: the instance runs, and its lease was reported to the
	// Session.
	InstanceReady = InstanceState{&instanceStateDefinition{"ready", workspaceStatusReady}}
	// InstanceReclaiming: a reclamation deletes the instance's Pod.
	InstanceReclaiming = InstanceState{&instanceStateDefinition{"reclaiming", workspaceStatusClosing}}
	// InstanceReclaimed: the Pod is gone; the namespace and the volume remain.
	InstanceReclaimed = InstanceState{&instanceStateDefinition{"reclaimed", workspaceStatusClosed}}
	// InstanceFailed: the provisioning failed or the reclamation did not
	// finish; Resources hold what it recorded, which the next reclamation or
	// the Project's deletion removes.
	InstanceFailed = InstanceState{&instanceStateDefinition{"failed", workspaceStatusFailed}}
)

var instanceStates = []InstanceState{InstanceProvisioning, InstanceReady, InstanceReclaiming, InstanceReclaimed, InstanceFailed}

// String is the persisted spelling of the state.
func (state InstanceState) String() string {
	if state.definition == nil {
		return "none"
	}
	return state.definition.name
}

// WorkspaceStatus is the API WorkspaceStatus spelling of the state, empty for
// InstanceNone, which the API shows as no workspace.
func (state InstanceState) WorkspaceStatus() apiStatus {
	if state.definition == nil {
		return ""
	}
	return state.definition.workspaceStatus
}

// MarshalText writes the persisted spelling.
func (state InstanceState) MarshalText() ([]byte, error) { return []byte(state.String()), nil }

// UnmarshalText reads a persisted spelling.
func (state *InstanceState) UnmarshalText(text []byte) error {
	if string(text) == InstanceNone.String() {
		*state = InstanceNone
		return nil
	}
	for _, declared := range instanceStates {
		if declared.definition.name == string(text) {
			*state = declared
			return nil
		}
	}
	return fmt.Errorf("unknown Sandbox instance state %q", text)
}

// holdsPod reports whether the instance may run a Pod on the volume: it must
// be reclaimed before another instance is provisioned.
func (state InstanceState) holdsPod() bool {
	return state == InstanceProvisioning || state == InstanceReady || state == InstanceFailed
}

// Failure is a recorded failure: its safe code and the bounded detail of why.
type Failure struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// failure is the record of code with its cause, truncated.
func failure(code, cause string) *Failure {
	return &Failure{Code: code, Detail: dexflow.TruncateCause(cause)}
}

// Instance is the Session's current Sandbox instance: its state, the
// request whose command the Flow runs or last ran, the instance InstanceID
// with Resources, whose namespace, service account and volume outlive its
// Pod, what the checkout or the last reclamation found in the volume
// (sandbox.Workspace*), the phases of its provisioning, the lease of a ready
// instance and the last failure. Its JSON keys are those the Session used
// before the Flow owned the instance.
type Instance struct {
	State              InstanceState          `json:"instance"`
	WorkspaceRequestID string                 `json:"workspaceRequestId,omitempty"`
	InstanceID         string                 `json:"instanceId,omitempty"`
	Resources          sandbox.Resources      `json:"resources"`
	VolumeState        sandbox.WorkspaceState `json:"volumeState,omitempty"`
	UpdatedAt          time.Time              `json:"instanceUpdatedAt,omitzero"`
	Timings            []dexflow.PhaseTiming  `json:"timings,omitempty"`
	Lease              *workspace.Lease       `json:"lease,omitempty"`
	Failure            *Failure               `json:"failure,omitempty"`
}

// set records the instance's new state.
func (i *Instance) set(state InstanceState, now time.Time) {
	i.State, i.UpdatedAt = state, now.UTC()
}

// needsReclaim reports whether the instance must be reclaimed before another
// one is provisioned: its Pod may still run on the volume.
func (i Instance) needsReclaim() bool { return i.State.holdsPod() }

// beginProvisioning makes requestID the instance being provisioned, on the
// namespace and the volume that earlier instances left.
func (i *Instance) beginProvisioning(requestID string, now time.Time) {
	i.WorkspaceRequestID, i.InstanceID = requestID, requestID
	i.Resources, i.Timings, i.Lease, i.Failure = i.Resources.WithoutPod(), nil, nil, nil
	i.set(InstanceProvisioning, now)
}

// beginReclaim starts the reclamation of the instance for the command of
// requestID and reports whether its provisioning may still run, whose end
// the reclamation first waits for.
func (i *Instance) beginReclaim(requestID string, now time.Time) (awaitsProvisioning bool) {
	awaitsProvisioning = i.State == InstanceProvisioning
	i.WorkspaceRequestID, i.Lease = requestID, nil
	i.set(InstanceReclaiming, now)
	return awaitsProvisioning
}

// settle records the outcome of the provisioning of the instance.
func (i *Instance) settle(provisioning Provisioning, now time.Time) {
	i.VolumeState, i.Timings = provisioning.VolumeState, provisioning.Timings
	// A provisioning starts from the retained namespace and volume, so its
	// resources include them; one that never ran recorded none.
	if provisioning.Resources != (sandbox.Resources{}) {
		i.Resources = provisioning.Resources
	}
	if provisioning.Failure != nil {
		i.Failure = provisioning.Failure
		i.set(InstanceFailed, now)
		return
	}
	lease := provisioning.Lease
	i.Lease, i.Failure = &lease, nil
	i.set(InstanceReady, now)
}

// reclaimed records the end of a reclamation: the Pod is gone, the namespace
// and the volume remain.
func (i *Instance) reclaimed(resources sandbox.Resources, volumeState string, now time.Time) {
	i.Resources, i.VolumeState, i.Failure = resources.WithoutPod(), volumeState, nil
	i.set(InstanceReclaimed, now)
}

// reclaimFailed records a reclamation that did not finish: the instance
// keeps its resources and records why, for the next reclamation or the
// Project's deletion.
func (i *Instance) reclaimFailed(detail string, now time.Time) {
	i.Failure = failure(sandbox.WorkspaceFailureReclaim, detail)
	i.set(InstanceFailed, now)
}

// sandboxInstance is the instance as its provider sees it.
func (i Instance) sandboxInstance(projectID string) sandbox.Instance {
	return sandbox.Instance{Identity: sandbox.Identity{ProjectID: projectID}, WorkspaceRequestID: i.InstanceID, Generation: sandbox.InstanceGeneration, Resources: i.Resources}
}

// Provisioning is the outcome of one provisioning as the provisioning
// records it: the READY lease, or the failure with its code, both with the
// resources it recorded, the volume state its checkout found and the phases
// it measured.
type Provisioning struct {
	Lease       workspace.Lease
	Resources   sandbox.Resources
	VolumeState string
	Timings     []dexflow.PhaseTiming
	Failure     *Failure
}

// CommandKind is what a WorkspaceCommand asks for. It is a struct that string
// literals cannot convert to; the values below are its only ones, persisted
// by their names. rank orders the commands of one request: a request is
// provisioned before its instance is reclaimed.
type CommandKind struct{ definition *commandKindDefinition }

type commandKindDefinition struct {
	name string
	rank int
}

var (
	// CommandProvision provisions the request's instance, after reclaiming
	// an earlier one whose Pod may still run.
	CommandProvision = CommandKind{&commandKindDefinition{"provision", 1}}
	// CommandReclaim reclaims the request's instance, keeping its volume.
	CommandReclaim = CommandKind{&commandKindDefinition{"reclaim", 2}}
)

// String is the persisted name of the kind, empty for the zero CommandKind
// of a task that runs no command.
func (kind CommandKind) String() string {
	if kind.definition == nil {
		return ""
	}
	return kind.definition.name
}

func (kind CommandKind) MarshalText() ([]byte, error) { return []byte(kind.String()), nil }
func (kind *CommandKind) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, kind, CommandKind.String, CommandProvision, CommandReclaim)
}

// WorkspaceCommand is one command of the Session: its ID ws-<n>/<kind>, its
// kind, the WorkspaceRequestID ws-<n> it names, the Session HEAD that a
// provisioning checks out and the instance the Preview holds, which a
// reclamation asks the Preview to release first.
type WorkspaceCommand struct {
	ID                 string      `json:"id"`
	Kind               CommandKind `json:"kind"`
	WorkspaceRequestID string      `json:"workspaceRequestId"`
	HeadCommit         string      `json:"headCommit,omitempty"`
	PreviewHolds       string      `json:"previewHolds,omitempty"`
}

// NewCommand is the command of kind for the Session request requestID.
func NewCommand(kind CommandKind, requestID, headCommit, previewHolds string) WorkspaceCommand {
	return WorkspaceCommand{ID: requestID + "/" + kind.String(), Kind: kind, WorkspaceRequestID: requestID, HeadCommit: headCommit, PreviewHolds: previewHolds}
}

// validate checks a command's identity before its admission.
func (command WorkspaceCommand) validate() error {
	if _, ok := flowid.WorkspaceRequestNumber(command.WorkspaceRequestID); !ok || command.Kind.definition == nil ||
		command.ID != command.WorkspaceRequestID+"/"+command.Kind.String() {
		return errors.New("a workspace command needs the ID ws-<n>/<kind> of its kind and request")
	}
	return nil
}

// Admission is the admission state of the commands: the last admitted
// command and whether the Flow is closing for the Project's deletion. Only
// the RPCs, which lock it, write it.
type Admission struct {
	Last   *WorkspaceCommand `json:"last,omitempty"`
	Closed bool              `json:"closed,omitempty"`
}

// admit decides one command: publish a new one, answer a repeat of an
// admitted one as accepted without publishing it, and refuse one of an older
// request or any command while closing. Requests are ordered by their
// number, the commands of one request by their kind's rank.
func (a Admission) admit(command WorkspaceCommand) (publishes, accepted bool) {
	if a.Closed {
		return false, false
	}
	if a.Last == nil {
		return true, true
	}
	number, _ := flowid.WorkspaceRequestNumber(command.WorkspaceRequestID)
	last, _ := flowid.WorkspaceRequestNumber(a.Last.WorkspaceRequestID)
	switch {
	case number > last || number == last && command.Kind.definition.rank > a.Last.Kind.definition.rank:
		return true, true
	case number == last:
		return false, true
	}
	return false, false
}

// OutcomeKind is how a command ended for the Session, a struct like
// CommandKind.
type OutcomeKind struct{ name string }

var (
	// OutcomeReady: the request's instance is ready with its lease.
	OutcomeReady = OutcomeKind{"ready"}
	// OutcomeFailed: the request's instance could not be provided.
	OutcomeFailed = OutcomeKind{"failed"}
	// OutcomeReclaimed: the request's instance is reclaimed, its volume kept.
	OutcomeReclaimed = OutcomeKind{"reclaimed"}
)

func (kind OutcomeKind) String() string               { return kind.name }
func (kind OutcomeKind) MarshalText() ([]byte, error) { return []byte(kind.name), nil }
func (kind *OutcomeKind) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, kind, OutcomeKind.String, OutcomeReady, OutcomeFailed, OutcomeReclaimed)
}

// reportAnswer is how the Session answered the report of an outcome, a
// struct like CommandKind; the zero reportAnswer is a report not answered
// yet.
type reportAnswer struct{ name string }

var (
	// reportAdmitted: the Session took the outcome.
	reportAdmitted = reportAnswer{"admitted"}
	// reportDeclined: the Session waits for no such request, or is closed.
	reportDeclined = reportAnswer{"declined"}
	// reportUndelivered: the report's retries ran out; the Session reads the
	// outcome once its own bounded wait ends.
	reportUndelivered = reportAnswer{"undelivered"}
)

func (answer reportAnswer) String() string               { return answer.name }
func (answer reportAnswer) MarshalText() ([]byte, error) { return []byte(answer.name), nil }
func (answer *reportAnswer) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, answer, reportAnswer.String, reportAdmitted, reportDeclined, reportUndelivered)
}

// Outcome is the outcome of the last command that the Flow reports to the
// Session: ready with the lease, failed with the failure, or reclaimed.
// Reported is how the Session answered.
type Outcome struct {
	WorkspaceRequestID string           `json:"workspaceRequestId"`
	Kind               OutcomeKind      `json:"kind"`
	Lease              *workspace.Lease `json:"lease,omitempty"`
	Failure            *Failure         `json:"failure,omitempty"`
	Reported           reportAnswer     `json:"reported,omitzero"`
}

// Snapshot is the SessionWorkspaceFlow, read from its Attributes: the
// Project, its current instance and the outcome of the last command.
type Snapshot struct {
	ProjectID string
	Instance  Instance
	Outcome   *Outcome
	Closed    bool
}

// SettledOutcome is the reported outcome of the request requestID while it
// is ready or failed, which a Session whose own wait ended reads; nil when
// no command of the request settled yet.
func (s Snapshot) SettledOutcome(requestID string) *Outcome {
	if s.Outcome == nil || s.Outcome.WorkspaceRequestID != requestID || s.Outcome.Kind == OutcomeReclaimed {
		return nil
	}
	return s.Outcome
}

// The tests name these values.
var (
	ReportAdmittedForTest = reportAdmitted
)
