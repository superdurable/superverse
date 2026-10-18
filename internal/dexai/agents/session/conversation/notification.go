// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package conversation

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/platform/connectorlibrary"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
)

// A Session notification is a fact for the model from outside its own turn: a
// background command or a subagent that ended, with the subagent's report and
// changes, a delivery outcome or a connector event. It reaches the Session only through the Channel
// SessionNotifications and is appended to the conversation as one user-role
// entry. The source keeps the authority; a notification is a historical copy.

// NotificationKind is what a notification reports.
type NotificationKind string

const (
	NotificationKindTask      NotificationKind = "task"
	NotificationKindDelivery  NotificationKind = "delivery"
	NotificationKindConnector NotificationKind = "connector"
)

// NotificationSource is the producer of a notification.
type NotificationSource string

const (
	NotificationSourceSubagent   NotificationSource = "subagent"
	NotificationSourceCommand    NotificationSource = "command"
	NotificationSourcePreview    NotificationSource = "preview"
	NotificationSourceRelease    NotificationSource = "release"
	NotificationSourceDeployment NotificationSource = "deployment"
	NotificationSourceConnector  NotificationSource = "connector"
)

// notificationSources are the sources of each kind.
var notificationSources = map[NotificationKind][]NotificationSource{
	NotificationKindTask:      {NotificationSourceSubagent, NotificationSourceCommand},
	NotificationKindDelivery:  {NotificationSourcePreview, NotificationSourceRelease, NotificationSourceDeployment},
	NotificationKindConnector: {NotificationSourceConnector},
}

// Validate rejects an unknown kind.
func (kind NotificationKind) Validate() error {
	if _, isKnown := notificationSources[kind]; !isKnown {
		return NewEnumValidationError("NotificationKind", string(kind))
	}
	return nil
}

// UnmarshalJSON decodes and validates a kind.
func (kind *NotificationKind) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, kind, NotificationKind.Validate)
}

// validate rejects an unknown source.
func (source NotificationSource) validate() error {
	for _, sources := range notificationSources {
		if slices.Contains(sources, source) {
			return nil
		}
	}
	return NewEnumValidationError("NotificationSource", string(source))
}

// UnmarshalJSON decodes and validates a source.
func (source *NotificationSource) UnmarshalJSON(data []byte) error {
	return DecodeEnum(data, source, NotificationSource.validate)
}

// Notification bounds. Producers truncate text and keep identity fields; long
// evidence stays readable at its source.
const (
	MaximumNotificationBytes = 4 << 10
	// MaximumSubagentReportBytes bounds a notification that carries a
	// subagent's final report.
	MaximumSubagentReportBytes = 24 << 10
	maximumNotificationIDBytes = 256
)

// SessionNotification is the payload of the Channel SessionNotifications. ID is
// derived by the producer from durable facts, so every retry of a producer
// repeats it, for example task/cmd-qjo5x7m2k4ab/exited. The content fields are
// those of its kind: a task names its task, status, summary and report, and a
// subagent's task the changes it saved or ChangesUnsaved for changes that stay
// unsaved in its worktree; a delivery its outcome, the failure's owner and
// code, the commit, the resource it concerns, at most MaximumNotificationGaps
// configuration gaps with their count, a summary and a message.
type SessionNotification struct {
	ID         string             `json:"id"`
	Kind       NotificationKind   `json:"kind"`
	Source     NotificationSource `json:"source"`
	OccurredAt time.Time          `json:"occurred_at"`
	TaskID     string             `json:"task_id,omitempty"`
	TaskStatus TaskEnd            `json:"task_status,omitempty"`
	Summary    string             `json:"summary,omitempty"`
	Report     string             `json:"report,omitempty"`
	Changes    *TaskChanges       `json:"changes,omitempty"`
	// ChangesUnsaved marks a subagent whose end could not save its worktree.
	ChangesUnsaved bool            `json:"changes_unsaved,omitempty"`
	Outcome        DeliveryOutcome `json:"outcome,omitzero"`
	Owner          DeliveryOwner   `json:"owner,omitzero"`
	Code           string          `json:"code,omitempty"`
	Phase          string          `json:"phase,omitempty"`
	Commit         string          `json:"commit,omitempty"`
	// Version is the version of Commit for a delivery, when it was recorded.
	Version          *repohost.CommitVersion `json:"version,omitempty"`
	ResourceID       string                  `json:"resource_id,omitempty"`
	ResolvedNoticeID string                  `json:"resolved_notice_id,omitempty"`
	URL              string                  `json:"url,omitempty"`
	Gaps             []string                `json:"gaps,omitempty"`
	GapCount         int                     `json:"gap_count,omitempty"`
	Message          string                  `json:"message,omitempty"`
	// Remedy is what the person can do about a delivery failure, which its
	// code declared.
	Remedy dexflow.FailureRemedy `json:"remedy,omitzero"`
	// Contribution is what a completed connector_develop subagent contributed
	// to the connectors library; nil for every other notification.
	Contribution *ConnectorContribution `json:"contribution,omitempty"`
	// Slug and Event name the connector and what happened to its
	// contribution, for a connector notification.
	Slug  string         `json:"slug,omitempty"`
	Event ConnectorEvent `json:"event,omitzero"`
}

// ConnectorEvent is what a connector notification reports of a contribution:
// its pull request could not be opened, or its release candidate was
// published or could not be. A struct like DeliveryOutcome.
type ConnectorEvent struct{ name string }

var (
	ConnectorPullRequestFailed         = ConnectorEvent{"pull_request_failed"}
	ConnectorReleaseCandidatePublished = ConnectorEvent{"release_candidate_published"}
	ConnectorReleaseCandidateFailed    = ConnectorEvent{"release_candidate_failed"}
	ConnectorChecksFailed              = ConnectorEvent{"checks_failed"}
)

func (event ConnectorEvent) String() string               { return event.name }
func (event ConnectorEvent) MarshalText() ([]byte, error) { return []byte(event.name), nil }
func (event *ConnectorEvent) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, event, ConnectorEvent.String, ConnectorPullRequestFailed, ConnectorReleaseCandidatePublished, ConnectorReleaseCandidateFailed, ConnectorChecksFailed)
}

// ContributionKind is what a connector_develop subagent contributes to the
// connectors library: a new connector, a new operation of an existing one, or
// a fix of an existing one. A struct like DeliveryOutcome; the values below are
// its only ones, persisted by their names.
type ContributionKind struct{ name string }

var (
	ContributionNew       = ContributionKind{"new"}
	ContributionOperation = ContributionKind{"operation"}
	ContributionFix       = ContributionKind{"fix"}
)

// contributionKinds are every kind, as start_agent offers them.
var contributionKinds = []ContributionKind{ContributionNew, ContributionOperation, ContributionFix}

func (kind ContributionKind) String() string               { return kind.name }
func (kind ContributionKind) MarshalText() ([]byte, error) { return []byte(kind.name), nil }
func (kind *ContributionKind) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, kind, ContributionKind.String, contributionKinds...)
}

// ConnectorContribution is the contribution a completed connector_develop
// subagent made: the connector's slug and module, the kind, the branch with
// its head commit, which the App's bot committed, and where the branch was
// pushed. Branch, commit and module are public data.
type ConnectorContribution struct {
	Slug       string                    `json:"slug"`
	Module     string                    `json:"module"`
	Kind       ContributionKind          `json:"kind"`
	Branch     string                    `json:"branch"`
	HeadCommit string                    `json:"head_commit"`
	Location   connectorlibrary.Location `json:"location"`
	// Summary is the start_agent description, which titles the commit and the
	// pull request: connector(<slug>): <summary>.
	Summary string `json:"summary"`
	// ReleaseCandidate is the candidate of the pushed head, or CandidateRefused
	// says why the library's rules refuse one; neither for a branch that
	// stayed in the Sandbox. CandidatePublished marks a candidate the finish
	// already published, on the local Git host's copy, which this notification
	// reports; the upstream library publishes the others later.
	ReleaseCandidate   *connectorlibrary.ReleaseCandidate `json:"release_candidate,omitempty"`
	CandidateRefused   string                             `json:"candidate_refused,omitempty"`
	CandidatePublished bool                               `json:"candidate_published,omitempty"`
}

// MaximumNotificationGaps bounds the configuration gaps a delivery
// notification names; GapCount counts them all, and the source keeps the list.
const MaximumNotificationGaps = 8

// TaskChanges are the changes a finished subagent left in its worktree: the
// commit that saved them on top of the snapshot it started from, and how many
// files, lines added and lines removed they hold. merge_agent_changes merges
// them into the workspace; a subagent without changes has none.
type TaskChanges struct {
	ResultCommit string `json:"result_commit"`
	Files        int    `json:"files"`
	Insertions   int    `json:"insertions"`
	Deletions    int    `json:"deletions"`
}

// Validate admits a notification of a known kind and source with an identity
// within its size bound.
func (notification SessionNotification) Validate() error {
	limit := MaximumNotificationBytes
	if notification.Source == NotificationSourceSubagent {
		limit = MaximumSubagentReportBytes
	}
	switch {
	case notification.ID == "" || len(notification.ID) > maximumNotificationIDBytes || strings.ContainsFunc(notification.ID, func(r rune) bool { return r <= ' ' || r > '~' }):
		return errors.New("a notification ID is 1 to 256 printable ASCII characters without spaces")
	case notification.Kind.Validate() != nil || notification.Source.validate() != nil:
		return fmt.Errorf("unknown notification kind %q or source %q", notification.Kind, notification.Source)
	case !slices.Contains(notificationSources[notification.Kind], notification.Source):
		return fmt.Errorf("a %s notification does not come from %s", notification.Kind, notification.Source)
	case notification.OccurredAt.IsZero():
		return errors.New("a notification needs the time it occurred")
	case notification.Kind == NotificationKindTask && (notification.TaskID == "" || notification.TaskStatus == "" || notification.Summary == ""):
		return errors.New("a task notification names its task, status and summary")
	case notification.Kind == NotificationKindConnector && (notification.Slug == "" || notification.Event == ConnectorEvent{} || notification.Summary == ""):
		return errors.New("a connector notification names its connector, event and summary")
	case notification.Kind == NotificationKindDelivery && (notification.Outcome == DeliveryOutcome{} || notification.Summary == ""):
		return errors.New("a delivery notification names its outcome and summary")
	case notification.Outcome != DeliveryReady && notification.Kind == NotificationKindDelivery && (notification.Code == "" || notification.Owner == DeliveryOwner{}):
		return errors.New("a delivery failure or notice names its code and owner")
	case notification.ResolvedNoticeID != "" && (notification.Kind != NotificationKindDelivery || notification.Outcome != DeliveryReady):
		return errors.New("only a ready delivery resolves a notification")
	case len(notification.Gaps) > MaximumNotificationGaps || notification.GapCount < len(notification.Gaps):
		return fmt.Errorf("a notification names at most %d of its gaps", MaximumNotificationGaps)
	case len(notification.Text()) > limit:
		return fmt.Errorf("a %s notification holds at most %d bytes", notification.Source, limit)
	}
	return nil
}

// DeliveryOutcome is what a delivery notification reports: a deployment that
// is ready, a failure the application owns, or a notice. It is a struct that
// string literals cannot convert to: the values below are its only ones,
// persisted by their names, and the zero DeliveryOutcome is none.
type DeliveryOutcome struct{ name string }

var (
	// DeliveryReady: the deployment serves. A ready delivery that resolves an
	// earlier notice enters the conversation; an ordinary one does not.
	DeliveryReady = DeliveryOutcome{"ready"}
	// DeliveryFailure: a failure of the application's own source.
	DeliveryFailure = DeliveryOutcome{"failure"}
	// DeliveryNotice: a failure the application does not own, a configuration
	// the user completes, or a failure of a commit that is no longer HEAD.
	DeliveryNotice = DeliveryOutcome{"notice"}
)

func (outcome DeliveryOutcome) String() string               { return outcome.name }
func (outcome DeliveryOutcome) MarshalText() ([]byte, error) { return []byte(outcome.name), nil }
func (outcome *DeliveryOutcome) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, outcome, DeliveryOutcome.String, DeliveryReady, DeliveryFailure, DeliveryNotice)
}

// DeliveryOwner is who must act on a delivery failure: the application's
// source, the user's configuration, or the platform. A struct like
// DeliveryOutcome; the zero DeliveryOwner is none, the owner of a ready
// outcome.
type DeliveryOwner struct{ name string }

var (
	DeliveryOwnerApplication   = DeliveryOwner{"application"}
	DeliveryOwnerConfiguration = DeliveryOwner{"configuration"}
	DeliveryOwnerPlatform      = DeliveryOwner{"platform"}
)

func (owner DeliveryOwner) String() string               { return owner.name }
func (owner DeliveryOwner) MarshalText() ([]byte, error) { return []byte(owner.name), nil }
func (owner *DeliveryOwner) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, owner, DeliveryOwner.String, DeliveryOwnerApplication, DeliveryOwnerConfiguration, DeliveryOwnerPlatform)
}

// OpensPlatformTurn reports whether the notification may open an automatic
// turn under the platform bound: a delivery failure the application owns. The
// consuming Session made a failure of a commit that is not its HEAD a notice
// first (AtHead).
func (notification SessionNotification) OpensPlatformTurn() bool {
	return notification.Kind == NotificationKindDelivery && notification.Outcome == DeliveryFailure && notification.Owner == DeliveryOwnerApplication
}

// IsPreviewReady reports a Preview that serves and holds the Sandbox it names.
func (notification SessionNotification) IsPreviewReady() bool {
	return notification.Kind == NotificationKindDelivery && notification.Source == NotificationSourcePreview && notification.Outcome == DeliveryReady
}

// IsSilentPreviewReady reports a ready Preview that has no earlier agent
// notice to resolve, so it does not enter the conversation.
func (notification SessionNotification) IsSilentPreviewReady() bool {
	return notification.IsPreviewReady() && notification.ResolvedNoticeID == ""
}

// AtHead is the notification as the Session records it while head is its
// HEAD: a delivery failure of another commit is stale, so it becomes a notice
// that opens no turn.
func (notification SessionNotification) AtHead(head string) SessionNotification {
	if notification.Kind == NotificationKindDelivery && notification.Outcome == DeliveryFailure && notification.Commit != head {
		notification.Outcome = DeliveryNotice
	}
	return notification
}

// TaskEnd is how a task ended, as its notification reports it: a background
// command exited, was lost or stopped, and a subagent completed, failed, was
// stopped or timed out. An alias of string, as the notification stores it; the
// constants declare the set, whose spellings the tasks' own statuses use.
type TaskEnd = string

const (
	TaskEndExited    TaskEnd = "exited"
	TaskEndLost      TaskEnd = "lost"
	TaskEndCompleted TaskEnd = "completed"
	TaskEndFailed    TaskEnd = "failed"
	TaskEndStopped   TaskEnd = "stopped"
	TaskEndTimedOut  TaskEnd = "timed_out"
)

// NotificationPreamble tells the model that the entry is an automated report;
// the mock model knows a notification by it.
const NotificationPreamble = "Superverse notification (automated report, not written by the user):\n"

// Text is what the model reads: the preamble and the notification as JSON.
func (notification SessionNotification) Text() string {
	return NotificationPreamble + string(comm.Must(json.Marshal(notification)))
}

// NotificationEntry records a notification as one user-role entry. Its Meta
// keeps the notification without the report, which only the model reads.
func NotificationEntry(notification SessionNotification) Entry {
	shown := notification
	shown.Report = ""
	return Entry{Role: EntryRoleUser, Kind: EntryKindNotification, Display: EntryDisplayPublic,
		Blocks: []json.RawMessage{textBlock(notification.Text())}, Meta: EntryMeta{Notification: &shown}}
}
