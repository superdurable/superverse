// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package preview

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/repohost"
	"github.com/superdurable/superversev2/internal/dexai/agents/sandbox/workspace"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/app/appmanifest"
)

// The automatic deployment: after a turn that pushed a verified commit, the
// Session asks its Preview to deploy exactly that commit
// (RequestPreviewDeployment). The Preview runs it as the internal command
// deploy-preview-<Sequence>, at most one at a time, and the newest request
// waits while another command runs. Every reportable outcome, of a deployment,
// a start or a source preparation, reaches the Session as one delivery
// notification (NotifyPreviewOutcome).

// DeploymentRequest asks to deploy the pushed Commit that the Session handed
// off at conversation Sequence on its READY instance WorkspaceRequestID.
type DeploymentRequest struct {
	WorkspaceRequestID, Commit string
	Sequence                   int64
}

// validate rejects a request no Session sends.
func (in DeploymentRequest) validate() error {
	if in.WorkspaceRequestID == "" || !flowid.IsCommitSHA(in.Commit) || in.Sequence <= 0 {
		return fmt.Errorf("a deployment request names an instance, a full commit SHA and a positive sequence, not %q, %q and %d", in.WorkspaceRequestID, in.Commit, in.Sequence)
	}
	return nil
}

// DeploymentAnswer is how the Preview answered a deployment request. It is a
// struct that string literals cannot convert to: the values below are its
// only ones, and the zero DeploymentAnswer is none.
type DeploymentAnswer struct{ name string }

var (
	// AnswerSkipped: the Preview released, or is releasing, the instance the
	// request names, so nothing runs; the next handoff or a manual start
	// deploys again.
	AnswerSkipped = DeploymentAnswer{"SKIPPED"}
	// AnswerDuplicate: the Preview handled or queued a request at least as new,
	// such as the same request whose answer was lost.
	AnswerDuplicate = DeploymentAnswer{"DUPLICATE"}
	// AnswerAccepted: no command was active; the deployment runs.
	AnswerAccepted = DeploymentAnswer{"ACCEPTED"}
	// AnswerQueued: a command is active; the request waits for it, and an
	// earlier queued request is superseded: the newest wins.
	AnswerQueued = DeploymentAnswer{"QUEUED"}
)

func (answer DeploymentAnswer) String() string               { return answer.name }
func (answer DeploymentAnswer) MarshalText() ([]byte, error) { return []byte(answer.name), nil }
func (answer *DeploymentAnswer) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, answer, DeploymentAnswer.String, AnswerSkipped, AnswerDuplicate, AnswerAccepted, AnswerQueued)
}

// DeploymentStatus is where the newest handled deployment stands. A struct
// like DeploymentAnswer, persisted by its name; the zero DeploymentStatus is
// no deployment.
type DeploymentStatus struct{ name string }

var (
	DeploymentRunning      = DeploymentStatus{"RUNNING"}
	DeploymentReady        = DeploymentStatus{"READY"}
	DeploymentIncompatible = DeploymentStatus{"INCOMPATIBLE"}
	DeploymentFailed       = DeploymentStatus{"FAILED"}
	// DeploymentSkipped: the deployment found no READY instance of the Session
	// and woke none.
	DeploymentSkipped = DeploymentStatus{"SKIPPED"}
	// DeploymentCancelled: a stop, a reclamation or the end of the Preview
	// cleared the queued request before it ran.
	DeploymentCancelled = DeploymentStatus{"CANCELLED"}
)

func (status DeploymentStatus) String() string               { return status.name }
func (status DeploymentStatus) MarshalText() ([]byte, error) { return []byte(status.name), nil }
func (status *DeploymentStatus) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, status, DeploymentStatus.String,
		DeploymentRunning, DeploymentReady, DeploymentIncompatible, DeploymentFailed, DeploymentSkipped, DeploymentCancelled)
}

// QueuedDeployment is a request that waits for the active command.
type QueuedDeployment struct {
	DeploymentRequest
	RequestedAt time.Time
}

// DeploymentRecord is the newest deployment the Preview handled: its command,
// pinned commit and handoff sequence, and its status with the failure and
// the failure's owner once it ended.
type DeploymentRecord struct {
	CommandID, Commit string
	Sequence          int64
	Status            DeploymentStatus
	Failure           *dexflow.Failure
	Owner             conversation.DeliveryOwner
	UpdatedAt         time.Time
}

// PreviewDeployment is the automatic deployment of the Session's pushed
// commits. HandledSequence is the newest request that ran or was cancelled,
// so a commit deploys once; NotifiedGapsDigest and NotifiedGapsNoticeID name
// the configuration gaps and notice the Session was last told about.
type PreviewDeployment struct {
	Queued               *QueuedDeployment
	HandledSequence      int64
	Last                 DeploymentRecord
	NotifiedGapsDigest   string
	NotifiedGapsNoticeID string `json:",omitempty"`
}

// requestDeployment answers a deployment request under CommandLock and
// reports whether it changed s. A request is admitted only while no command
// is active; otherwise it waits as the newest request, and the Step that ends
// the active command admits it. A retried request answers as it did. The
// accepted command is returned for the caller to publish.
func (s *Snapshot) requestDeployment(in DeploymentRequest, now time.Time) (answer DeploymentAnswer, accepted *Command, changed bool) {
	queued := s.Deployment.Queued
	switch {
	case queued != nil && queued.Sequence == in.Sequence:
		return AnswerQueued, nil, false
	case in.Sequence <= s.Deployment.HandledSequence || queued != nil && in.Sequence < queued.Sequence:
		return AnswerDuplicate, nil, false
	case s.ActiveCommand.ID != "":
		s.Deployment.Queued = &QueuedDeployment{DeploymentRequest: in, RequestedAt: now}
		return AnswerQueued, nil, true
	}
	command := s.admitDeployment(in, now)
	return AnswerAccepted, &command, true
}

// admitDeployment makes the request the active deploy-preview command, whose
// ID repeats for every retry of the request, and records it running.
func (s *Snapshot) admitDeployment(in DeploymentRequest, now time.Time) Command {
	command := Command{ID: fmt.Sprintf("%s-%d", CommandDeployPreview, in.Sequence), Kind: CommandDeployPreview, Generation: s.Generation,
		WorkspaceRequestID: in.WorkspaceRequestID, Commit: in.Commit}
	s.accept(command)
	s.Deployment.Queued, s.Deployment.HandledSequence = nil, in.Sequence
	s.Deployment.Last = DeploymentRecord{CommandID: command.ID, Commit: in.Commit, Sequence: in.Sequence, Status: DeploymentRunning, UpdatedAt: now}
	return command
}

// cancelQueued ends the queued request unrun.
func (s *Snapshot) cancelQueued(now time.Time) {
	if queued := s.Deployment.Queued; queued != nil {
		s.Deployment.Queued, s.Deployment.HandledSequence = nil, queued.Sequence
		s.Deployment.Last = DeploymentRecord{Commit: queued.Commit, Sequence: queued.Sequence, Status: DeploymentCancelled, UpdatedAt: now}
	}
}

// preparePinnedSource prepares the pinned commit of the active deployment in
// s: it exports the commit's source tree in the instance, publishes the
// AppManifest from the commit and generates its definitions, never reading
// the working tree, and records the READY preparation with that AppManifest.
// A definite source refusal does not replace the last successful AppManifest. The admitted preparation
// records FAILED on refusal; serving processes stay intact. Other errors retry.
func (f *PreviewFlow) preparePinnedSource(ctx context.Context, s *Snapshot) (*previewRefusal, error) {
	command := s.ActiveCommand
	tree, err := f.Workspace.EnsurePreviewSource(ctx, s.binding(), command.ID, command.Commit)
	switch {
	case err != nil:
		return nil, fmt.Errorf("export the source tree of %s: %w", command.Commit, err)
	case tree.State == workspace.PreviewStateSourceMissing:
		refused := refusal(failurePreviewSourceNotPrepared, fmt.Errorf("the workspace repository does not hold %s: %s", command.Commit, tree.Reason))
		return &refused, nil
	case tree.State != workspace.PreviewStateReady || tree.Path == "":
		return nil, fmt.Errorf("the source export of %s answered %s", command.Commit, tree.State)
	}
	manifest, version, err := f.Preview.registerPreviewAppManifest(ctx, *s, command.Commit)
	if err != nil {
		refused := manifestRefusal(err)
		return &refused, nil
	}
	prepared, err := f.Preview.preparePinnedPreviewDefinitions(ctx, *s, manifest, f.Workspace, tree.Path)
	if errors.Is(err, errManifestMismatch) {
		refused := refusal(failurePreviewManifest, err)
		return &refused, nil
	}
	if refused, isRefused := sourceRefusal(err); isRefused {
		return &refused, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prepare the Preview definitions of %s: %w", command.Commit, err)
	}
	prepared.Version = version
	s.AppManifest, s.PreparedSource = manifest, prepared
	return nil, nil
}

// StartOutcome is how a recorded start ended, RUNNING until it ends. A struct
// like DeploymentStatus, persisted by its name; the zero StartOutcome is none.
type StartOutcome struct{ name string }

var (
	StartRunning = StartOutcome{"RUNNING"}
	StartReady   = StartOutcome{"READY"}
	// StartFailed: a provider Step's retries or wait limit ran out.
	StartFailed = StartOutcome{"FAILED"}
	// StartRefused: a Step decided a definite refusal (RecordPreviewRefusal).
	StartRefused = StartOutcome{"REFUSED"}
	// StartCancelled: a deployment that found no READY instance, or a start
	// that the deletion of its Project ended.
	StartCancelled = StartOutcome{"CANCELLED"}
)

func (outcome StartOutcome) String() string               { return outcome.name }
func (outcome StartOutcome) MarshalText() ([]byte, error) { return []byte(outcome.name), nil }
func (outcome *StartOutcome) UnmarshalText(text []byte) error {
	return comm.UnmarshalName(text, outcome, StartOutcome.String, StartRunning, StartReady, StartFailed, StartRefused, StartCancelled)
}

// maximumRecentStarts bounds RecentStarts; the oldest record is dropped first.
const maximumRecentStarts = 50

// StartRecord is one start-preview or deploy-preview: who sent it, the commit
// and its version, the configuration revision it ran with and whether it
// applied it, and how and when it ended with the code of its failure and the
// phases it measured. The failure message stays in the Preview's Failure.
type StartRecord struct {
	CommandID             string
	Kind                  CommandKind
	ActorAccountID        string `json:",omitempty"`
	Commit                string `json:",omitempty"`
	ConfigurationRevision uint64 `json:",omitempty"`
	Applied               bool   `json:",omitempty"`
	AdmittedAt            time.Time
	EndedAt               time.Time `json:",omitzero"`
	Outcome               StartOutcome
	FailureCode           string                 `json:",omitempty"`
	Timings               []dexflow.PhaseTiming  `json:",omitempty"`
	Version               repohost.CommitVersion `json:",omitzero"`
}

// Failure is the failure that ended the start, with the message of its code,
// or nil for a start that did not fail. A code that is no longer declared
// keeps its name alone.
func (r StartRecord) Failure() *dexflow.Failure {
	if r.FailureCode == "" {
		return nil
	}
	code, isDeclared := failureCodes[r.FailureCode]
	if !isDeclared {
		return &dexflow.Failure{Code: r.FailureCode, Phase: string(r.Kind)}
	}
	return newFailure(code, r.Kind, "")
}

// recordStart records the admitted start c as RUNNING, dropping the oldest
// record beyond the bound.
func (s *Snapshot) recordStart(c Command, now time.Time) {
	record := StartRecord{CommandID: c.ID, Kind: c.Kind, ActorAccountID: c.ActorAccountID, Commit: c.Commit, ConfigurationRevision: c.ConfigurationRevision,
		Applied: c.ApplyConfiguration, AdmittedAt: now, Outcome: StartRunning}
	s.RecentStarts = append(s.RecentStarts, record)
	s.RecentStarts = s.RecentStarts[max(0, len(s.RecentStarts)-maximumRecentStarts):]
}

// startRecord is the record of the start commandID, or nil once it was
// dropped; a dropped record is never recreated.
func (s *Snapshot) startRecord(commandID string) *StartRecord {
	for index := len(s.RecentStarts) - 1; index >= 0; index-- {
		if s.RecentStarts[index].CommandID == commandID {
			return &s.RecentStarts[index]
		}
	}
	return nil
}

// endStart ends the running record of the start commandID with outcome, the
// failure's code and the phases the start measured since its admission.
func (s *Snapshot) endStart(commandID string, outcome StartOutcome, failure *dexflow.Failure, now time.Time) {
	record := s.startRecord(commandID)
	if record == nil || record.Outcome != StartRunning {
		return
	}
	record.Outcome, record.EndedAt = outcome, now
	if failure != nil {
		record.FailureCode = failure.Code
	}
	if timings := s.Preview.Timings; len(timings) > 0 && !timings[0].StartedAt.Before(record.AdmittedAt) {
		record.Timings = append([]dexflow.PhaseTiming(nil), timings...)
	}
}

// completion is the input of CompletePreviewCommand: Skipped marks a
// deployment that found no READY instance and ran nothing.
type completion struct {
	Skipped bool `json:"skipped,omitempty"`
}

// commandEnd is how the active command ended: completed without a failure,
// or with the failure and its owner, the configuration gaps of an
// incompatible start, or skipped, a deployment that found no READY instance.
// refused marks a definite refusal and cancelled the end by the Project's
// deletion.
type commandEnd struct {
	failure            *dexflow.Failure
	owner              conversation.DeliveryOwner
	gaps               []appmanifest.RequirementGap
	skipped            bool
	refused, cancelled bool
}

// startOutcome is the outcome of a start that ended so.
func (end commandEnd) startOutcome() StartOutcome {
	switch {
	case end.skipped || end.cancelled:
		return StartCancelled
	case end.failure == nil:
		return StartReady
	case end.refused:
		return StartRefused
	}
	return StartFailed
}

// endCommand ends the active command of s as end says, admits the command
// that follows it and saves both with the deployment, under CommandLock like
// every Step that ends a command. It discards a pending settlement, which
// names the ended command, so the next command never consumes it. It reports
// whether the Session is to be told, and returns the admitted command, which
// the calling Step publishes (finishCommand, admitNext).
func endCommand(ctx dex.Context, s Snapshot, end commandEnd) (bool, *Command) {
	discardSettlements(ctx)
	now, ended := time.Now().UTC(), s.ActiveCommand
	reports := s.finishCommand(end, now)
	admitted := s.admitNext(ended, now)
	save(ctx, &s)
	saveDeployment(ctx, s)
	return reports, admitted
}

// discardSettlements deletes the pending WorkspaceSettled messages; the
// calling Step loads the Channel (commandEndStepOptions).
func discardSettlements(ctx dex.Context) {
	for _, message := range comm.Must(WorkspaceSettled.PendingMessages(ctx)) {
		comm.MustDo(WorkspaceSettled.Delete(ctx, message.MessageID))
	}
}

// maximumConfigurationGaps bounds the gaps a refused start keeps.
const maximumConfigurationGaps = 32

// finishCommand ends the active command as end says and records what it
// leaves: a failure, a released instance, the outcome of a deployment. It
// keeps the notification of a reportable outcome in Report and reports
// whether the Session is to be told. A start-preview that ends before it
// pinned its configuration started nothing: it leaves a READY or STOPPING
// Preview as it found it, and any other STOPPED, with its failure. One that
// pinned is released next (admitNext), and the release that completes keeps
// the failure of the start it released.
func (s *Snapshot) finishCommand(end commandEnd, now time.Time) (reports bool) {
	command, failure, owner := s.ActiveCommand, end.failure, end.owner
	if command.Kind == CommandRelease && failure == nil {
		failure, owner = s.Failure, s.FailureOwner
	}
	s.LastCommand, s.ActiveCommand, s.Failure, s.FailureOwner = command, Command{}, failure, owner
	switch {
	case end.failure != nil:
		s.Preview.Timings = dexflow.EndPhase(s.Preview.Timings, now)
		if (command.Kind == CommandPreparePreviewSource || command.Kind == CommandDeployPreview) &&
			s.PreparedSource.RequestID == command.ID && s.PreparedSource.Status == previewSourcePreparing {
			s.PreparedSource.Status, s.PreparedSource.Failure = previewSourceFailed, end.failure
		}
		if len(end.gaps) > 0 {
			s.Preview.ConfigurationGaps = end.gaps[:min(len(end.gaps), maximumConfigurationGaps)]
		}
	case command.Kind == CommandReclaim:
		s.releaseInstance()
	}
	if end.failure != nil && command.Kind == CommandStartPreview && s.Preview.Status == previewStarting && s.Configuration.CommandID != command.ID {
		s.Preview.Status = previewStopped
		if before := s.Preview.StatusBeforeStart; before == PreviewReady || before == previewStopping {
			s.Preview.Status = before
		}
	}
	if command.Kind.starts() {
		s.endStart(command.ID, end.startOutcome(), end.failure, now)
	}
	status := end.status()
	if last := &s.Deployment.Last; command.Kind == CommandDeployPreview && last.CommandID == command.ID {
		last.Status, last.Failure, last.Owner, last.UpdatedAt = status, end.failure, end.owner, now
	}
	return s.keepReport(command, status, end, now)
}

// admitNext admits the command that follows the ended one, which the caller
// publishes: the queued deployment, unless a stop or a reclamation ended,
// which cancels it; else, after a start or a deployment that started the
// Preview but did not reach READY, the release of what it started
// (CommandRelease). So a start that does not reach READY, and the deployments
// queued behind it, leave nothing serving and no grant behind, and the
// Preview ends STOPPED with the start's failure, which a person starts again.
func (s *Snapshot) admitNext(ended Command, now time.Time) *Command {
	switch {
	case s.Deployment.Queued != nil:
		// The next deployment takes over what the ended command left.
		next := s.admitDeployment(s.Deployment.Queued.DeploymentRequest, now)
		return &next
	case ended.Kind.starts() && s.startedUnserved():
		release := Command{ID: string(CommandRelease) + "-" + ended.ID, Kind: CommandRelease, Generation: s.Generation, WorkspaceRequestID: s.WorkspaceRequestID}
		s.accept(release)
		return &release
	}
	return nil
}

// status is the deployment status of the end.
func (end commandEnd) status() DeploymentStatus {
	switch {
	case end.skipped:
		return DeploymentSkipped
	case end.failure == nil:
		return DeploymentReady
	case end.failure.Code == failurePreviewIncompatible.String():
		return DeploymentIncompatible
	}
	return DeploymentFailed
}

// keepReport keeps in Report the notification of the outcome of command and
// reports whether the Session is to be told: every outcome of a deployment, a
// start that is READY or ended with a failure, and a failed source
// preparation; never a stop, a reclamation, a skipped deployment, or the same
// configuration gaps twice in a row.
func (s *Snapshot) keepReport(command Command, status DeploymentStatus, end commandEnd, now time.Time) bool {
	resolvedNoticeID := ""
	switch {
	case !command.Kind.reports() || status == DeploymentSkipped || status == DeploymentReady && !command.Kind.starts():
		return false
	case status == DeploymentIncompatible:
		digest := flowid.Fingerprint(end.gaps)
		if digest == s.Deployment.NotifiedGapsDigest {
			return false
		}
		s.Deployment.NotifiedGapsDigest = digest
	case status == DeploymentReady:
		resolvedNoticeID = s.Deployment.NotifiedGapsNoticeID
		s.Deployment.NotifiedGapsDigest = ""
		s.Deployment.NotifiedGapsNoticeID = ""
	}
	report := s.deliveryReport(command, status, end, now, resolvedNoticeID)
	if status == DeploymentIncompatible {
		s.Deployment.NotifiedGapsNoticeID = report.ID
	}
	s.Report = &report
	return true
}

// deliveryReport is the delivery notification of the outcome of command.
// Its ID derives from the instance, the command and the outcome, so every
// retry of NotifyPreviewOutcome repeats it. READY is ready, INCOMPATIBLE a
// notice with the gaps, and FAILED a failure when the application owns it and
// a notice otherwise. The summary names no commit or code, which the
// notification carries in its own fields. The message is cut until the
// notification fits its bound.
func (s Snapshot) deliveryReport(command Command, status DeploymentStatus, end commandEnd, now time.Time, resolvedNoticeID string) conversation.SessionNotification {
	commit := s.outcomeCommit(command)
	report := conversation.SessionNotification{
		Kind: conversation.NotificationKindDelivery, Source: conversation.NotificationSourcePreview, OccurredAt: now,
		Outcome: conversation.DeliveryNotice, Owner: end.owner, Phase: string(command.Kind), Commit: commit, ResourceID: s.WorkspaceRequestID,
		GapCount: len(end.gaps), Gaps: appmanifest.RequirementGapNames(end.gaps[:min(len(end.gaps), conversation.MaximumNotificationGaps)]),
	}
	if source := s.PreparedSource; source.SourceCommit == commit && source.Version != (repohost.CommitVersion{}) {
		report.Version = &source.Version
	}
	switch {
	case status == DeploymentReady:
		report.Outcome, report.Summary = conversation.DeliveryReady, "The Preview is ready."
		if resolvedNoticeID != "" {
			report.ResolvedNoticeID, report.URL = resolvedNoticeID, s.Preview.URL
			report.Summary = previewConfigurationCompleteSummary
		}
	case status == DeploymentFailed && end.owner == conversation.DeliveryOwnerApplication:
		report.Outcome = conversation.DeliveryFailure
	}
	if end.failure != nil {
		report.Code, report.Message, report.Remedy = end.failure.Code, end.failure.Message, end.failure.Remedy
		report.Summary = "The Preview failed."
	}
	if status == DeploymentIncompatible {
		report.Summary = comm.TruncateUTF8("The Preview needs configuration: "+strings.Join(report.Gaps, ", ")+".", maximumSummaryBytes)
	}
	report.ID = "preview/" + cmp.Or(s.WorkspaceRequestID, "none") + "/" + notificationCommandID(command) + "/" + report.Outcome.String()
	for len(report.Text()) > conversation.MaximumNotificationBytes && report.Message != "" {
		report.Message = comm.TruncateUTF8(report.Message, len(report.Message)/2)
	}
	return report
}

// maximumSummaryBytes bounds the summary of a report, which Studio shows.
const maximumSummaryBytes = 512

const previewConfigurationCompleteSummary = "The Preview configuration is complete."

// outcomeCommit is the commit an outcome of command concerns: a deployment's
// pinned commit, the prepared commit a start runs, or the HEAD a preparation
// observed.
func (s Snapshot) outcomeCommit(command Command) string {
	switch command.Kind {
	case CommandDeployPreview:
		return command.Commit
	case CommandStartPreview:
		return cmp.Or(s.Configuration.SourceCommit, s.PreparedSource.SourceCommit)
	default:
		return cmp.Or(s.PreparedSource.SourceCommit, s.HeadCommit)
	}
}

// notificationCommandID is the command's part of a notification ID: its own
// ID when that is printable ASCII without spaces, else its kind and the
// fingerprint of its ID. Only the internal commands choose their ID.
func notificationCommandID(command Command) string {
	if len(command.ID) <= 128 && !strings.ContainsFunc(command.ID, func(r rune) bool { return r <= ' ' || r > '~' || r == '/' }) {
		return command.ID
	}
	return string(command.Kind) + "-" + flowid.Fingerprint(command.ID)[:16]
}
