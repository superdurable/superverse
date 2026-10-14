// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview

import (
	"errors"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
)

var (
	ErrConflict = errors.New("preview revision or generation conflict")
	ErrBusy     = errors.New("preview has an active command")
	ErrClosed   = errors.New("preview is closed")
	ErrNotReady = errors.New("preview is not ready")
	// ErrNotHeld answers a reclaim of an instance the Preview does not use.
	ErrNotHeld = errors.New("the Preview does not use that instance")
)

// Status is the Preview's status, an alias of string as its readers store
// it; the constants declare the set.
type Status = string

const (
	PreviewNotStarted Status = "NOT_STARTED"
	previewStarting   Status = "STARTING"
	PreviewReady      Status = "READY"
	previewStopping   Status = "STOPPING"
	previewStopped    Status = "STOPPED"
)

// Preview environment statuses: the Sandbox-local Preview engine.
const (
	environmentNotStarted = "NOT_STARTED"
	environmentStarting   = "STARTING"
	EnvironmentReady      = "READY"
	environmentStopping   = "STOPPING"
	environmentStopped    = "STOPPED"
	environmentFailed     = "FAILED"
)

// How a reclamation stopped the Preview environment: the supervisor stopped
// it, or deleting the Pod does.
const (
	environmentStopGraceful = "GRACEFUL"
	environmentStopForced   = "FORCED"
)

// SourceStatus is the status of the Preview's source preparation, an alias of
// string as its readers store it; the constants declare the set.
type SourceStatus = string

const (
	PreviewSourceNotPrepared SourceStatus = "NOT_PREPARED"
	previewSourcePreparing   SourceStatus = "PREPARING"
	previewSourceReady       SourceStatus = "READY"
	previewSourceFailed      SourceStatus = "FAILED"
)

// CommandKind names what one admitted command does to the Preview.
type CommandKind string

const (
	CommandPreparePreviewSource CommandKind = "prepare-preview-source"
	CommandStartPreview         CommandKind = "start-preview"
	CommandStopPreview          CommandKind = "stop-preview"
	// CommandReclaim releases the Session's instance WorkspaceRequestID before
	// the SessionWorkspaceFlow reclaims it: it stops the Preview served on it and its
	// engine. Only the Session sends it.
	CommandReclaim CommandKind = "reclaim"
	// CommandDeployPreview deploys the pushed Commit of a turn: it prepares the
	// source of exactly that commit and starts it. Only RequestPreviewDeployment
	// admits it, as deploy-preview-<Sequence>; RequestPreviewCommand refuses it.
	CommandDeployPreview CommandKind = "deploy-preview"
	// CommandRelease releases what a refused or failed deploy-preview started
	// for a Preview that serves nothing, as an explicit stop does: the grants,
	// the route and the application, keeping the instance and its engine.
	// Only the Preview admits it, as release-<deploy command ID>, when that
	// deployment ends; it keeps the deployment's failure, and a deployment
	// queued meanwhile runs after it. RequestPreviewCommand refuses it.
	CommandRelease CommandKind = "release"
)

// stops reports whether the command stops the Preview: its route,
// application and grants.
func (kind CommandKind) stops() bool {
	return kind == CommandStopPreview || kind == CommandReclaim || kind == CommandRelease
}

// releasesInstance reports whether the command's stop gives up the instance,
// whose Pod the SessionWorkspaceFlow deletes next: a reclaim, and a command to which the
// Session lends no instance while it holds one. An explicit stop and the
// release of a deployment keep the instance and its engine.
func (kind CommandKind) releasesInstance() bool {
	return kind == CommandReclaim || kind.acquiresWorkspace()
}

// acquiresWorkspace reports whether the command runs on the Session's current
// instance, which it acquires from the Session first.
func (kind CommandKind) acquiresWorkspace() bool {
	return kind == CommandPreparePreviewSource || kind.starts()
}

// starts reports whether the command starts the Preview application from a
// prepared source.
func (kind CommandKind) starts() bool {
	return kind == CommandStartPreview || kind == CommandDeployPreview
}

// reports reports whether the command's outcome reaches the Session: a
// deployment's, a start's and a source preparation's (deliveryReport).
func (kind CommandKind) reports() bool {
	return kind == CommandPreparePreviewSource || kind.starts()
}

// Command is one admitted command. ID identifies it across retries; the
// instance fields name the Preview the caller observed, for reclaim the
// instance the SessionWorkspaceFlow reclaims and for deploy-preview the instance the
// Session named. Commit is the pinned commit of a deploy-preview.
// ActorAccountID is the person who sent a start, stop or source preparation,
// empty for the Agent's deployment and the platform's commands. A start-preview
// with ApplyConfiguration applies exactly ConfigurationRevision of the
// configuration head; every field takes part in the repeat comparison.
type Command struct {
	ExpectedRevision      uint64
	ID                    string
	Kind                  CommandKind
	Generation            uint64
	WorkspaceRequestID    string
	Commit                string `json:",omitempty"`
	ActorAccountID        string `json:",omitempty"`
	ApplyConfiguration    bool   `json:",omitempty"`
	ConfigurationRevision uint64 `json:",omitempty"`
}

// ReclaimCommand is the command with which the SessionWorkspaceFlow reclaims
// instance requestID; its ID repeats for every retry of the reclamation.
func ReclaimCommand(requestID string) Command {
	return Command{ID: "reclaim-" + requestID, Kind: CommandReclaim, WorkspaceRequestID: requestID}
}

// commandRejection is why RequestPreviewCommand refused a command.
type commandRejection string

const (
	rejectionInvalidCommand  commandRejection = "INVALID_COMMAND"
	rejectionCommandConflict commandRejection = "COMMAND_CONFLICT"
	rejectionStaleGeneration commandRejection = "STALE_GENERATION"
	rejectionBusy            commandRejection = "BUSY"
	// rejectionNotHeld answers a reclaim of an instance the Preview does not use.
	rejectionNotHeld commandRejection = "NOT_HELD"
)

// commandResult answers a command with the Preview after it.
type commandResult struct {
	Snapshot  Snapshot
	Rejection commandRejection
}

// admit decides whether c may become the active command. An exact repeat of
// the active or last command is admitted again as a repeat, without a second
// effect. A reclaim is fenced by the instance it names alone, because the
// Session observes no Preview revision. A deployment is no command of this
// admission: RequestPreviewDeployment admits it.
func (s Snapshot) admit(c Command) (rejection commandRejection, repeat bool) {
	switch c.Kind {
	case CommandPreparePreviewSource, CommandStartPreview, CommandStopPreview, CommandReclaim:
	default:
		return rejectionInvalidCommand, false
	}
	if c.ID == "" {
		return rejectionInvalidCommand, false
	}
	if s.ActiveCommand.ID == c.ID || s.LastCommand.ID == c.ID {
		original := s.ActiveCommand
		if original.ID != c.ID {
			original = s.LastCommand
		}
		// Revision is the admission precondition, not a new logical operation: a
		// retry after acceptance reads a later revision, while the instance and
		// kind fields must still match the original command.
		c.ExpectedRevision = original.ExpectedRevision
		if original != c {
			return rejectionCommandConflict, false
		}
		return "", true
	}
	switch {
	case c.Kind == CommandReclaim && (c.WorkspaceRequestID == "" || c.WorkspaceRequestID != s.WorkspaceRequestID || s.Resources.Instance.UID == ""):
		return rejectionNotHeld, false
	case c.Kind != CommandReclaim && !s.observedBy(c):
		return rejectionStaleGeneration, false
	case s.ActiveCommand.ID != "":
		return rejectionBusy, false
	}
	return "", false
}

// observedBy reports whether c names the Preview as it is now: its revision,
// its generation and the instance it acquired, which is empty while it never
// acquired one, as after a refused first command. The first-demand form
// observed no Preview and names only the generation, which the Session
// Service supplies.
func (s Snapshot) observedBy(c Command) bool {
	if c.ExpectedRevision == 0 {
		return c.Generation == s.Generation && c.WorkspaceRequestID == ""
	}
	return c.Generation == s.Generation && c.ExpectedRevision == s.Revision && c.WorkspaceRequestID == s.WorkspaceRequestID
}

// accept makes the admitted c the active command and records what it begins,
// a start in RecentStarts.
// A start clears the configuration gaps of the last one and keeps the status
// it found, which it restores if it pins nothing (finishCommand). A deployment clears
// them too, but leaves the Preview's status until it starts the Preview
// (PreparePreviewConfiguration), so a deployment that ends before leaves the
// Preview as it found it; it keeps the instance's preparation until it
// prepares its own commit. A release keeps the failure of the deployment it
// releases.
func (s *Snapshot) accept(c Command) {
	s.ActiveCommand = c
	if c.Kind.starts() {
		s.recordStart(c, time.Now().UTC())
	}
	if c.Kind != CommandRelease {
		s.Failure, s.FailureOwner = nil, conversation.DeliveryOwner{}
	}
	switch {
	case c.Kind == CommandStartPreview:
		s.Preview.StatusBeforeStart = s.Preview.Status
		s.Preview.Status, s.Preview.Timings, s.Preview.ConfigurationGaps = previewStarting, nil, nil
	case c.Kind == CommandDeployPreview:
		s.Preview.ConfigurationGaps = nil
		s.PreparedSource = PreparedPreviewSource{Status: previewSourcePreparing, RequestID: c.ID, SourceCommit: c.Commit, WorkspaceRequestID: c.WorkspaceRequestID, Generation: s.Generation}
	case c.Kind == CommandPreparePreviewSource:
		s.PreparedSource = PreparedPreviewSource{Status: previewSourcePreparing, RequestID: c.ID, WorkspaceRequestID: s.WorkspaceRequestID, Generation: s.Generation}
	case c.Kind.stops():
		s.Preview.Status = previewStopping
	}
}

// startsPreview records that the active command starts the Preview: the
// first save of the start that changes what the Preview runs, its pinned
// configuration and engine, marks it STARTING.
func (s *Snapshot) startsPreview(configuration PreviewConfigurationRef) {
	configuration.CommandID = s.ActiveCommand.ID
	s.Configuration, s.Environment.Status, s.Preview.Status = configuration, environmentStarting, previewStarting
}

// startedUnserved reports whether a start that did not reach READY started
// the Preview, its pinned configuration and engine and perhaps its grant,
// application and route, which a release removes again. A start-preview that
// ended before it pinned its configuration no longer reads STARTING
// (finishCommand).
func (s Snapshot) startedUnserved() bool {
	return s.Preview.Status == previewStarting
}

// releaseInstance records that the Preview no longer holds the acquired
// instance, whose Pod the SessionWorkspaceFlow deletes: a reclaim of the instance answers
// NOT_HELD from now on. The Preview names the instance until it acquires the
// next one.
func (s *Snapshot) releaseInstance() { s.Resources = s.Resources.WithoutPod() }

func commandAnswer(s Snapshot, rejection commandRejection) *dex.RPCResult[commandResult] {
	return &dex.RPCResult[commandResult]{Output: commandResult{Snapshot: s, Rejection: rejection}}
}

// WorkspaceSettlement announces to the Preview command CommandID that the
// Session's workspace request it waits for settled: READY, or FAILED with the
// Session's safe code.
type WorkspaceSettlement struct {
	CommandID          string
	WorkspaceRequestID string
	Failure            string
}

// The tests name these constants.
const (
	PreviewStoppedForTest          = previewStopped
	EnvironmentStoppedForTest      = environmentStopped
	EnvironmentStopGracefulForTest = environmentStopGraceful
	PreviewSourceReadyForTest      = previewSourceReady
	PreviewSourcePreparingForTest  = previewSourcePreparing
	PreviewSourceFailedForTest     = previewSourceFailed
)
