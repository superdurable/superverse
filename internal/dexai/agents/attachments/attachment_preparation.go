// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package attachments

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/fileextract"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// Settings are the bounds and durations of attachments, which the server's
// attachments configuration sets: the largest file of each kind, an image's
// long edge, the inline text one message renders, how long an upload waits,
// how long a READY attachment stays staged and how long a presigned upload
// stays valid.
type Settings struct {
	ImageMaxBytes, DocumentMaxBytes, JSONMaxBytes int64
	ImageLongEdge, InlineTextBytes                int
	UploadWait, StagingRetention                  time.Duration
	UploadURLValidity                             time.Duration
}

// DefaultUploadURLValidity is how long a presigned upload stays valid.
const DefaultUploadURLValidity = 15 * time.Minute

// maxUploadURLMargin covers the moment between the read that finds an
// attachment awaiting its upload and the signature of its upload, after which
// the Flow may stop awaiting it; a shorter URL validity has a margin as long.
const maxUploadURLMargin = time.Minute

// The waits and retries of the Steps.
const (
	// preparationAttempt bounds one attempt of the preparation, which waits for
	// the replica's extractor slot and runs the child for at most two minutes.
	preparationAttempt = 10 * time.Minute
	// preparationBudget bounds every attempt of the preparation together.
	preparationBudget = 45 * time.Minute
	// rawDeletionBudget bounds the deletion of a JSON file's raw upload.
	rawDeletionBudget = 30 * time.Minute
	// deletionBudget bounds the deletion of the staging prefix.
	deletionBudget = 2 * time.Hour
	// releaseBudget bounds the retries of the staging slot's release.
	releaseBudget = time.Hour
	// deletionPollInterval spaces the rounds of a deletion.
	deletionPollInterval = 5 * time.Second
	// singleCallRound and deletionRound bound one round of one call, and of
	// the bounded batch of deletions DeletePrefix makes.
	singleCallRound, deletionRound = 10 * time.Second, 40 * time.Second
	// admissionSpan bounds the reservation, the verification, the
	// preparation and the deletion of a JSON file's raw upload together.
	admissionSpan = 10*time.Minute + preparationBudget + rawDeletionBudget
	// holdMargin covers the moments between the Steps, such as a Worker
	// restart, in an attachment's staging hold.
	holdMargin = time.Hour
	// reclaimDeletions bounds the deletion batches of one lapsed slot's prefix.
	reclaimDeletions = 3
)

// stagingTail is how long an attachment may still hold its staging slot after
// it expired: until its presigned uploads expired, its prefix's deletion and
// its failure route's, and its release.
func (s Settings) stagingTail() time.Duration {
	return s.UploadURLValidity + maxUploadURLMargin + 2*deletionBudget + releaseBudget + holdMargin
}

// stagingHold is the longest an attachment holds its staging slot from its
// start: its upload wait, its admission, its staging retention and its tail.
// Past it the TeamSpace's next upload that finds the quota full reclaims the
// slot, as an AttachmentFlow that failed leaves it.
func (s Settings) stagingHold() time.Duration {
	return s.UploadWait + admissionSpan + s.StagingRetention + s.stagingTail()
}

// expiry is when an attachment published READY at now expires: after its
// staging retention, and early enough for its tail to end within its hold,
// heldUntil, so that no reclaim deletes the objects of an attachment that
// still holds its slot on schedule.
func (s Settings) expiry(now, heldUntil time.Time) time.Time {
	expires := now.Add(s.StagingRetention)
	if latest := heldUntil.Add(-s.stagingTail()); latest.Before(expires) {
		return latest
	}
	return expires
}

// maxBytes is the largest file of kind.
func (s Settings) maxBytes(kind fileextract.Kind) int64 {
	switch kind {
	case fileextract.KindImage:
		return s.ImageMaxBytes
	case fileextract.KindDocument:
		return s.DocumentMaxBytes
	default:
		return s.JSONMaxBytes
	}
}

// attachmentKinds are the attachment kinds of the extractor's kinds.
var attachmentKinds = map[fileextract.Kind]conversation.AttachmentKind{
	fileextract.KindImage: conversation.AttachmentKindImage, fileextract.KindDocument: conversation.AttachmentKindDocument, fileextract.KindJSON: conversation.AttachmentKindJSON,
}

// jsonFormats are the attachment JSON formats of the extractor's formats.
var jsonFormats = map[fileextract.JSONFormat]conversation.AttachmentJSONFormat{
	fileextract.JSONFormatN8N: conversation.AttachmentJSONFormatN8n, fileextract.JSONFormatGeneric: conversation.AttachmentJSONFormatGeneric,
}

// upload is the original the browser PUTs: its key below the staging prefix,
// its media type, its length and its digest, which storage verifies.
func (a Attachment) upload() objectstore.Upload {
	digest := "sha256:" + a.SHA256
	return objectstore.Upload{Key: objectKey(stagingPrefix(a.TeamSpaceID, a.AttachmentID), roleOriginal, digest), MediaType: a.MediaType, Size: a.ByteSize, Digest: digest}
}

// label names the attachment in its renditions.
func (a Attachment) label() conversation.AttachmentLabel {
	return conversation.AttachmentLabel{ID: a.AttachmentID, Name: a.FileName, Kind: a.Kind}
}

// prepare reads the verified original, runs the extractor child on it and
// writes the renditions of the result and, for JSON, the redacted copy; it
// returns what it prepared, or the failure the extractor's refusal decides. An
// error is a transient fault the Step retries: every object is written once
// under its digest, so a retry writes the same objects.
func (f *AttachmentFlow) prepare(ctx context.Context, a Attachment) (*PreparedAttachment, *decidedFailure, error) {
	data, err := f.objects.Get(ctx, *a.Original)
	if err != nil {
		return nil, nil, fmt.Errorf("read the upload: %w", err)
	}
	format, accepted := fileextract.FormatOf(a.MediaType, a.FileName)
	if !accepted {
		panic("attachment " + a.AttachmentID + " was admitted with an unaccepted format")
	}
	// The TeamSpace is the extraction's queue, so one TeamSpace's backlog never starves another's preparations.
	request := fileextract.Request{Format: format, MaxBytes: f.settings.maxBytes(format.Kind()), MaxImageEdge: f.settings.ImageLongEdge, Queue: a.TeamSpaceID}
	result, err := f.extractor.Extract(ctx, request, data)
	if err != nil {
		return nil, nil, err
	}
	if result.Refusal != nil {
		failure := decide(extractorFailures[result.Refusal.Code], result.Refusal.Detail)
		return nil, failure.Failure, nil
	}
	writer := &preparedWriter{ctx: ctx, objects: f.objects, prefix: stagingPrefix(a.TeamSpaceID, a.AttachmentID)}
	prepared := &PreparedAttachment{ObjectBytes: a.ByteSize, ExtractorVersion: result.Version, RenditionVersion: renditionVersion}
	label := a.label()
	switch {
	case result.Image != nil:
		probe := *result.Image
		prepared.Image = &PreparedImage{Width: probe.Width, Height: probe.Height, Frames: probe.Frames}
		prepared.Rendition = writer.rendition(roleRendition, imageRendition(a.MediaType, data, probe))
	case result.Document != nil:
		document := *result.Document
		full, brief, shown := documentRenditions(label, document, f.settings.InlineTextBytes)
		prepared.Document = &PreparedDocument{Sections: len(document.Sections), TextBytes: document.TextBytes, Pages: document.PageCount, ExcerptSections: shown, Truncated: document.Truncated}
		prepared.Rendition = writer.rendition(roleRendition, full)
		prepared.Brief = new(writer.rendition(roleBrief, brief))
		bundle := writer.object(roleBundle, objectstore.MediaTypeJSON, comm.Must(objectstore.EncodeJSON(sectionsBundleOf(document))))
		prepared.Bundle = &bundle
	default:
		f.prepareJSON(writer, label, *result.JSON, prepared)
		prepared.ObjectBytes -= a.ByteSize
	}
	if writer.err != nil {
		return nil, nil, writer.err
	}
	prepared.ObjectBytes += writer.bytes
	return prepared, nil, nil
}

// prepareJSON writes a JSON file's redacted copy and its renditions: an
// export's summary, which routes it to the workflow import, or a generic
// file's text or summary.
func (f *AttachmentFlow) prepareJSON(writer *preparedWriter, label conversation.AttachmentLabel, prepared fileextract.PreparedJSON, into *PreparedAttachment) {
	redacted := writer.object(roleRedacted, objectstore.MediaTypeJSON, prepared.Redacted)
	into.Redacted = &redacted
	facts := &PreparedJSON{Format: jsonFormats[prepared.Format], Workflows: len(prepared.Workflows), Redactions: prepared.Redactions}
	for _, workflow := range prepared.Workflows {
		facts.Nodes += workflow.Nodes
		facts.Triggers += len(workflow.Triggers)
		for _, credential := range workflow.Credentials {
			if len(facts.Credentials) < maxNamedCredentials {
				facts.Credentials = append(facts.Credentials, comm.TruncateUTF8(credential.Name, maxCredentialName))
			}
		}
	}
	into.JSON = facts
	if prepared.Format == fileextract.JSONFormatN8N {
		into.Rendition = writer.rendition(roleRendition, exportRendition(label, prepared))
		return
	}
	full, brief := genericRenditions(label, prepared)
	into.Rendition = writer.rendition(roleRendition, full)
	if brief != nil {
		into.Brief = new(writer.rendition(roleBrief, *brief))
	}
}

// preparedWriter writes the objects of one preparation below prefix, each once
// under its digest, and keeps the first error and the bytes written.
type preparedWriter struct {
	ctx     context.Context
	objects *objectstore.Store
	prefix  string
	bytes   int64
	err     error
}

func (w *preparedWriter) object(role, mediaType string, data []byte) preparedObject {
	if w.err != nil {
		return preparedObject{}
	}
	ref, err := w.objects.Put(w.ctx, objectKey(w.prefix, role, objectstore.Digest(data)), mediaType, data)
	if err != nil {
		w.err = fmt.Errorf("write the %s: %w", role, err)
		return preparedObject{}
	}
	w.bytes += int64(len(data))
	return preparedObject{Ref: ref, Bytes: int64(len(data))}
}

func (w *preparedWriter) rendition(role string, rendition rendered) preparedObject {
	object := w.object(role, objectstore.MediaTypeRendition, rendition.block)
	object.ContextTokens = rendition.tokens
	return object
}

// reservationFailure is the failure of a reservation TeamSpaceFlow refused.
func reservationFailure(rejected *dexflow.Rejection) failureCode {
	switch {
	case rejected.HasCode(dexflow.RejectionResourceExhausted):
		return failureQuotaExceeded
	case rejected.HasCode(dexflow.RejectionPermissionDenied):
		return failureNotMember
	default:
		return failureTeamSpaceClosed
	}
}

// reserveStaging reserves the attachment's staging slot for the Flow flowID
// until its hold lapses. A quota that is full first gets back the slots whose
// hold lapsed, which an AttachmentFlow that ended without releasing them
// left, with their objects, and is asked once more.
func (f *AttachmentFlow) reserveStaging(ctx context.Context, a Attachment, flowID string) error {
	reservation := teamspaces.StagingReservation{AttachmentID: a.AttachmentID, FlowID: flowID, Bytes: a.ByteSize, HeldUntil: a.StagingHeldUntil,
		Actor: teamspaces.Actor{AccountID: a.UploaderAccountID, PlatformAdmin: a.UploaderIsPlatformAdmin}}
	err := f.teamSpaces.ReserveAttachmentStaging(ctx, a.TeamSpaceID, reservation)
	if rejected, isRejected := errors.AsType[*dexflow.Rejection](err); !isRejected || !rejected.HasCode(dexflow.RejectionResourceExhausted) {
		return err
	}
	reclaimed, reclaimErr := f.reclaimLapsedStaging(ctx, a.TeamSpaceID)
	if reclaimErr != nil || reclaimed == 0 {
		return cmp.Or(reclaimErr, err)
	}
	return f.teamSpaces.ReserveAttachmentStaging(ctx, a.TeamSpaceID, reservation)
}

// reclaimLapsedStaging deletes the staging prefix of each lapsed slot of the
// TeamSpace, at most ten, then releases the slot, and returns how many it
// released. A prefix that reclaimDeletions batches leave unclean keeps its
// slot for a later reclaim, so that no object outlives its slot.
func (f *AttachmentFlow) reclaimLapsedStaging(ctx context.Context, teamSpaceID string) (int, error) {
	lapsed, err := f.teamSpaces.LapsedAttachmentStaging(ctx, teamSpaceID)
	if err != nil {
		return 0, fmt.Errorf("list the lapsed staging slots: %w", err)
	}
	reclaimed := 0
	for _, attachmentID := range lapsed {
		// DeletePrefix reports clean once a batch finds nothing left; a staging prefix holds a few objects.
		clean := false
		for batch := 0; batch < reclaimDeletions && !clean && err == nil; batch++ {
			clean, err = f.objects.DeletePrefix(ctx, stagingPrefix(teamSpaceID, attachmentID))
		}
		if err != nil {
			return reclaimed, fmt.Errorf("delete the objects of lapsed attachment %s: %w", attachmentID, err)
		}
		if !clean {
			continue
		}
		if err = f.teamSpaces.ReleaseAttachmentStaging(ctx, teamSpaceID, attachmentID); err != nil {
			return reclaimed, fmt.Errorf("release the lapsed slot of attachment %s: %w", attachmentID, err)
		}
		reclaimed++
	}
	return reclaimed, nil
}

// uploadsExpire is how long until every PUT the API presigned for the
// attachment expired, at least a second: an upload that lands after its
// deletion would outlive the Flow.
func (f *AttachmentFlow) uploadsExpire(ctx dex.Context) time.Duration {
	closedAt := comm.Must(uploadClosedAtAttribute.Get(ctx))
	if closedAt.IsZero() {
		return time.Second
	}
	validity := f.settings.UploadURLValidity
	return max(closedAt.Add(validity+min(validity, maxUploadURLMargin)).Sub(ctx.FirstAttemptAt()), time.Second)
}

// deleteObjects deletes every version under the attachment's staging prefix,
// in bounded batches, until a round finds it clean.
func (f *AttachmentFlow) deleteObjects(ctx dex.Context, a Attachment) error {
	prefix := stagingPrefix(a.TeamSpaceID, a.AttachmentID)
	return dexflow.PollUntil(ctx, deletionPollInterval, deletionRound, func(callCtx context.Context) (bool, error) {
		return f.objects.DeletePrefix(callCtx, prefix)
	})
}

// failureEnding is how RecordAttachmentFailure ends the attachment: FAILED
// with the failure a Step decided, or with preparation_failed and the cause
// that exhausted a Step's retries; a deletion that failed keeps the status it
// was deleting to.
func failureEnding(ctx dex.Context, in progress) progress {
	switch {
	case in.Failure != nil:
		return progress{Ending: StatusFailed, Failure: in.Failure}
	case in.Ending != "":
		return progress{Ending: in.Ending}
	}
	cause := dexflow.RecoveryDetail(ctx)
	if dexflow.RecoveryTimedOut(ctx) {
		cause = fmt.Sprintf("%s did not finish within its wait limit: %s", dexflow.RecoveryStep(ctx), cause)
	}
	return decide(failurePreparationFailed, cause).failed()
}

// failed is a decided failure's ending.
func (p progress) failed() progress {
	return progress{Ending: StatusFailed, Failure: p.Failure}
}

// recordedFailure is the durable record of a decided failure: its code, its
// sentence and the cause, which the API never shows.
func recordedFailure(failure decidedFailure) *dexflow.Failure {
	for _, code := range failureCodes {
		if code.name == failure.Code {
			return &dexflow.Failure{Code: code.name, Phase: failurePhase, Message: dexflow.FailureMessage(code.sentence, failure.Detail)}
		}
	}
	panic("attachment failure code " + failure.Code + " is not declared")
}
