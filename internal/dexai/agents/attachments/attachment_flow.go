// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package attachments

import (
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

var (
	startIdentityAttribute     = dex.DefineAttribute[dexflow.StartIdentity]("StartIdentity")
	teamSpaceIDAttribute       = dex.DefineAttribute[string]("TeamSpaceID")
	attachmentIDAttribute      = dex.DefineAttribute[string]("AttachmentID")
	uploaderAttribute          = dex.DefineAttribute[string]("UploaderAccountID")
	uploaderIsAdminAttribute   = dex.DefineAttribute[bool]("UploaderIsPlatformAdmin")
	fileNameAttribute          = dex.DefineAttribute[string]("FileName")
	mediaTypeAttribute         = dex.DefineAttribute[string]("MediaType")
	kindAttribute              = dex.DefineAttribute[conversation.AttachmentKind]("Kind")
	byteSizeAttribute          = dex.DefineAttribute[int64]("ByteSize")
	sha256Attribute            = dex.DefineAttribute[string]("SHA256")
	statusAttribute            = dex.DefineAttribute[string]("AttachmentStatus")
	revisionAttribute          = dex.DefineAttribute[uint64]("Revision")
	activeCommandAttribute     = dex.DefineAttribute[*AttachmentCommand]("ActiveCommand")
	originalAttribute          = dex.DefineAttribute[*objectstore.ObjectRef]("Original")
	preparedAttribute          = dex.DefineAttribute[*PreparedAttachment]("Prepared")
	failureAttribute           = dex.DefineAttribute[*dexflow.Failure]("Failure")
	createdAtAttribute         = dex.DefineAttribute[time.Time]("CreatedAt")
	updatedAtAttribute         = dex.DefineAttribute[time.Time]("UpdatedAt")
	readyAtAttribute           = dex.DefineAttribute[time.Time]("ReadyAt")
	expiresAtAttribute         = dex.DefineAttribute[time.Time]("ExpiresAt")
	uploadClosedAtAttribute    = dex.DefineAttribute[time.Time]("UploadClosedAt")
	stagingHeldUntilAttribute  = dex.DefineAttribute[time.Time]("StagingHeldUntil")
	uploadCompletedChannel     = dex.DefineChannel[UploadCompletion]("UploadCompleted")
	attachmentCommandsChannel  = dex.DefineChannel[AttachmentCommand]("AttachmentCommands")
	statusLock                 = dex.LockAttribute(statusAttribute)
	attachmentAttributeDefines = []dex.AttributeDef{
		startIdentityAttribute, teamSpaceIDAttribute, attachmentIDAttribute, uploaderAttribute, uploaderIsAdminAttribute, fileNameAttribute,
		mediaTypeAttribute, kindAttribute, byteSizeAttribute, sha256Attribute, statusAttribute, revisionAttribute, activeCommandAttribute,
		originalAttribute, preparedAttribute, failureAttribute, createdAtAttribute, updatedAtAttribute, readyAtAttribute, expiresAtAttribute,
		uploadClosedAtAttribute, stagingHeldUntilAttribute,
	}
)

// AttachmentFlow is the owner of one uploaded file of a TeamSpace (attachment-<teamSpaceID>-<attachmentID>): its staging
// slot, its upload, its preparation into the blocks the model reads, its 24 hours as READY and its deletion.
type AttachmentFlow struct {
	dex.FlowDefaults
	objects    *objectstore.Store
	extractor  *fileextract.Extractor
	teamSpaces *teamspaces.TeamSpaceFlowClient
	settings   Settings
}

func (f *AttachmentFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(ReserveAttachmentStaging{f: f}), dex.DefineStep(AwaitAttachmentUpload{f: f}), dex.DefineStep(VerifyAttachmentUpload{f: f}),
		dex.DefineStep(PrepareAttachment{f: f}), dex.DefineStep(DeleteRawAttachment{f: f}), dex.DefineStep(PublishAttachment{f: f}),
		dex.DefineStep(AwaitAttachmentExpiry{f: f}), dex.DefineStep(DeleteAttachmentObjects{f: f}), dex.DefineStep(CompleteAttachment{f: f}),
		dex.DefineStep(RecordAttachmentFailure{f: f}),
	}
}

func (f *AttachmentFlow) GetRPCs() []dex.RPCDef {
	read := dexflow.ShortRPC()
	write := dexflow.ShortRPC(statusLock)
	return []dex.RPCDef{
		dex.DefineRPC(f.GetDexSummary, read), dex.DefineRPC(f.GetDexDisplay, read), dex.DefineRPC(f.GetAttachmentState, read),
		dex.DefineRPC(f.CompleteAttachmentUpload, write), dex.DefineRPC(f.RequestAttachmentDeletion, write),
	}
}

func (f *AttachmentFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: attachmentAttributeDefines, Channels: []dex.ChannelDef{uploadCompletedChannel, attachmentCommandsChannel}}
}

func (f *AttachmentFlow) InitialAttributes(in upload) []dex.InitialAttributeDef {
	b := &dexflow.InitialAttributes{}
	now := time.Now().UTC()
	dexflow.Add(b, startIdentityAttribute, in.identity)
	dexflow.Add(b, teamSpaceIDAttribute, in.teamSpaceID)
	dexflow.Add(b, attachmentIDAttribute, in.attachmentID)
	dexflow.Add(b, uploaderAttribute, in.uploader.AccountID)
	dexflow.Add(b, uploaderIsAdminAttribute, in.uploader.PlatformAdmin)
	dexflow.Add(b, fileNameAttribute, in.fileName)
	dexflow.Add(b, mediaTypeAttribute, in.mediaType)
	dexflow.Add(b, kindAttribute, in.kind)
	dexflow.Add(b, byteSizeAttribute, in.byteSize)
	dexflow.Add(b, sha256Attribute, in.sha256)
	dexflow.Add(b, statusAttribute, StatusRegistering)
	dexflow.Add(b, revisionAttribute, uint64(1))
	dexflow.Add(b, activeCommandAttribute, nil)
	dexflow.Add(b, originalAttribute, nil)
	dexflow.Add(b, preparedAttribute, nil)
	dexflow.Add(b, failureAttribute, nil)
	dexflow.Add(b, createdAtAttribute, now)
	dexflow.Add(b, updatedAtAttribute, now)
	dexflow.Add(b, readyAtAttribute, time.Time{})
	dexflow.Add(b, expiresAtAttribute, time.Time{})
	dexflow.Add(b, uploadClosedAtAttribute, time.Time{})
	dexflow.Add(b, stagingHeldUntilAttribute, now.Add(f.settings.stagingHold()))
	return b.Values()
}

// dex:field attribute-key:FileName value-type:string editable:false description:"Attached file name"
func (*AttachmentFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"FileName": comm.Must(fileNameAttribute.Get(ctx))}}, nil
}

// dex:field attribute-key:FileName value-type:string editable:false description:"Attached file name"
// dex:field attribute-key:AttachmentStatus value-type:string editable:false description:"Attachment lifecycle status"
func (*AttachmentFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"FileName": comm.Must(fileNameAttribute.Get(ctx)), "AttachmentStatus": comm.Must(statusAttribute.Get(ctx))}}, nil
}

// GetAttachmentState reads every Attribute without a lock, also of a completed execution.
func (*AttachmentFlow) GetAttachmentState(ctx dex.Context, _ dex.None) (*dex.RPCResult[Attachment], error) {
	return &dex.RPCResult[Attachment]{Output: readAttachment(ctx)}, nil
}

// CompleteAttachmentUpload ends the upload wait of an attachment that awaits it, once: a repeat, or a completion in
// another status, answers the state.
func (f *AttachmentFlow) CompleteAttachmentUpload(ctx dex.Context, in UploadCompletion) (*dex.RPCResult[attachmentUpdate], error) {
	a := readAttachment(ctx)
	switch rejection := completionRefusal(teamspaces.Actor{AccountID: in.AccountID})(a); {
	case rejection != nil:
		return updated(ctx, rejection)
	case a.Status != StatusAwaitingUpload || a.ActiveCommand != nil:
		return updated(ctx, nil)
	}
	closeUpload(ctx)
	comm.MustDo(statusAttribute.Set(ctx, StatusVerifying))
	comm.MustDo(uploadCompletedChannel.Publish(ctx, in))
	touch(ctx)
	return updated(ctx, nil)
}

// RequestAttachmentDeletion admits one deletion before the attachment's own deletion starts, from its uploader or
// the platform admin; the same command ID answers the state, another one is refused.
func (f *AttachmentFlow) RequestAttachmentDeletion(ctx dex.Context, in deletionRequest) (*dex.RPCResult[attachmentUpdate], error) {
	a := readAttachment(ctx)
	switch rejection := deletionRefusal(in.Actor)(a); {
	case rejection != nil:
		return updated(ctx, rejection)
	case isTerminal(a.Status), a.Status == StatusDeleting && a.ActiveCommand == nil:
		return updated(ctx, nil)
	case a.ActiveCommand != nil && a.ActiveCommand.ID == in.ID:
		return updated(ctx, nil)
	case a.ActiveCommand != nil:
		return updated(ctx, dexflow.Reject(dexflow.RejectionFailedPrecondition, "the attachment's deletion is in progress"))
	}
	command := AttachmentCommand{ID: in.ID, Kind: commandDeleteAttachment, ActorAccountID: in.Actor.AccountID}
	comm.MustDo(activeCommandAttribute.Set(ctx, &command))
	comm.MustDo(attachmentCommandsChannel.Publish(ctx, command))
	touch(ctx)
	return updated(ctx, nil)
}

// dex:group group-id:upload group-label:"Upload"
// dex:explanation text:"Reserve the attachment's slot in its TeamSpace's staging quota, reclaiming lapsed slots when it is full, then await its upload."
type ReserveAttachmentStaging struct {
	dex.StepDefaultsNoWaitFor[progress]
	f *AttachmentFlow
}

func (s ReserveAttachmentStaging) GetStepOptions() *dex.StepOptions {
	options := dexflow.ProviderStepOptions(failureRoute(s.f))
	options.ExecuteLockAttributes = []dex.AttributeLock{statusLock}
	return options
}

func (s ReserveAttachmentStaging) Execute(ctx dex.Context, _ progress) (*dex.StepDecision, error) {
	a := readAttachment(ctx)
	err := s.f.reserveStaging(ctx, a, ctx.FlowID())
	if rejected, isRejected := errors.AsType[*dexflow.Rejection](err); isRejected {
		return dex.GoTo(RecordAttachmentFailure{f: s.f}, decide(reservationFailure(rejected), rejected.Message)), nil
	}
	if err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("reserve the staging slot of attachment %s: %w", a.AttachmentID, err))
	}
	comm.MustDo(statusAttribute.Set(ctx, StatusAwaitingUpload))
	touch(ctx)
	return dex.GoTo(AwaitAttachmentUpload{f: s.f}, progress{}), nil
}

// dex:group group-id:upload group-label:"Upload"
// dex:explanation text:"Wait for the uploader's completion, a deletion command or the end of the upload wait."
type AwaitAttachmentUpload struct {
	dex.StepDefaults
	f *AttachmentFlow
}

func (AwaitAttachmentUpload) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{statusLock}
	return options
}

func (s AwaitAttachmentUpload) WaitFor(ctx dex.Context, _ progress) (*dex.Wait, error) {
	deadline := comm.Must(createdAtAttribute.Get(ctx)).Add(s.f.settings.UploadWait)
	return dex.AnyOf(uploadCompletedChannel.ForOne(), attachmentCommandsChannel.ForOne(), dex.Timer(max(deadline.Sub(ctx.FirstAttemptAt()), time.Second))), nil
}

func (s AwaitAttachmentUpload) Execute(ctx dex.Context, _ progress) (*dex.StepDecision, error) {
	a := readAttachment(ctx)
	if a.Status == StatusAwaitingUpload {
		closeUpload(ctx)
	}
	if a.ActiveCommand != nil {
		markDeleting(ctx)
		return dex.GoTo(DeleteAttachmentObjects{f: s.f}, progress{Ending: StatusDeleted}), nil
	}
	comm.MustDo(statusAttribute.Set(ctx, StatusVerifying))
	touch(ctx)
	return dex.GoTo(VerifyAttachmentUpload{f: s.f}, progress{UploadWaitEnded: ctx.HasTimerFired()}), nil
}

// dex:group group-id:upload group-label:"Upload"
// dex:explanation text:"Read the head of the uploaded object; storage verified its length and SHA-256 when it accepted it."
type VerifyAttachmentUpload struct {
	dex.StepDefaultsNoWaitFor[progress]
	f *AttachmentFlow
}

func (s VerifyAttachmentUpload) GetStepOptions() *dex.StepOptions {
	options := dexflow.ProviderStepOptions(failureRoute(s.f))
	options.ExecuteLockAttributes = []dex.AttributeLock{statusLock}
	return options
}

func (s VerifyAttachmentUpload) Execute(ctx dex.Context, in progress) (*dex.StepDecision, error) {
	a := readAttachment(ctx)
	if a.ActiveCommand != nil {
		markDeleting(ctx)
		return dex.GoTo(DeleteAttachmentObjects{f: s.f}, progress{Ending: StatusDeleted}), nil
	}
	ref, err := s.f.objects.VerifyUpload(ctx, a.upload())
	switch {
	case errors.Is(err, objectstore.ErrNotFound) && !in.UploadWaitEnded:
		comm.MustDo(statusAttribute.Set(ctx, StatusAwaitingUpload))
		touch(ctx)
		return dex.GoTo(AwaitAttachmentUpload{f: s.f}, progress{}), nil
	case errors.Is(err, objectstore.ErrNotFound):
		return dex.GoTo(RecordAttachmentFailure{f: s.f}, decide(failureUploadMissing, fmt.Sprintf("no upload arrived within %s", s.f.settings.UploadWait))), nil
	case errors.Is(err, objectstore.ErrUploadMismatch):
		return dex.GoTo(RecordAttachmentFailure{f: s.f}, decide(failureUploadMismatch, err.Error())), nil
	case err != nil:
		return nil, dex.ErrorWithStack(fmt.Errorf("verify the upload of attachment %s: %w", a.AttachmentID, err))
	}
	comm.MustDo(originalAttribute.Set(ctx, &ref))
	comm.MustDo(statusAttribute.Set(ctx, StatusPreparing))
	touch(ctx)
	return dex.GoTo(PrepareAttachment{f: s.f}, progress{}), nil
}

// dex:group group-id:prepare group-label:"Preparation"
// dex:explanation text:"Run the extractor child on the original and write the renditions the model reads; JSON gets its redacted copy."
type PrepareAttachment struct {
	dex.StepDefaultsNoWaitFor[progress]
	f *AttachmentFlow
}

func (s PrepareAttachment) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteDurability = dex.StepDurabilitySync
	options.ExecuteMethodTimeout, options.HeartbeatTimeout = preparationAttempt, time.Minute
	options.ExecuteRetry = dexflow.CappedStepRetry(3, preparationBudget)
	options.ExecuteFailure = failureRoute(s.f)
	return options
}

func (s PrepareAttachment) Execute(ctx dex.Context, _ progress) (*dex.StepDecision, error) {
	a := readAttachment(ctx)
	if a.ActiveCommand != nil {
		return dex.GoTo(PublishAttachment{f: s.f}, progress{}), nil
	}
	stop := dexflow.KeepAlive(ctx, nil)
	defer stop()
	prepared, failure, err := s.f.prepare(ctx, a)
	switch {
	case err != nil:
		return nil, dex.ErrorWithStack(fmt.Errorf("prepare attachment %s: %w", a.AttachmentID, err))
	case failure != nil:
		return dex.GoTo(RecordAttachmentFailure{f: s.f}, progress{Failure: failure}), nil
	case prepared.Redacted != nil:
		return dex.GoTo(DeleteRawAttachment{f: s.f}, progress{Prepared: prepared}), nil
	}
	return dex.GoTo(PublishAttachment{f: s.f}, progress{Prepared: prepared}), nil
}

// dex:group group-id:prepare group-label:"Preparation"
// dex:explanation text:"Delete every version of a JSON file's raw upload once its redacted copy exists."
type DeleteRawAttachment struct {
	dex.StepDefaultsNoWaitFor[progress]
	f *AttachmentFlow
}

func (s DeleteRawAttachment) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(rawDeletionBudget, failureRoute(s.f))
}

func (s DeleteRawAttachment) Execute(ctx dex.Context, in progress) (*dex.StepDecision, error) {
	a := readAttachment(ctx)
	raw := stagingPrefix(a.TeamSpaceID, a.AttachmentID) + roleOriginal + "/"
	if err := dexflow.PollUntil(ctx, deletionPollInterval, singleCallRound, func(callCtx context.Context) (bool, error) {
		return s.f.objects.DeletePrefix(callCtx, raw)
	}); err != nil {
		return nil, err
	}
	return dex.GoTo(PublishAttachment{f: s.f}, in), nil
}

// dex:group group-id:prepare group-label:"Preparation"
// dex:explanation text:"Record what preparation made and mark the attachment READY until its staging retention ends."
type PublishAttachment struct {
	dex.StepDefaultsNoWaitFor[progress]
	f *AttachmentFlow
}

func (PublishAttachment) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteDurability = dex.StepDurabilitySync
	options.ExecuteLockAttributes = []dex.AttributeLock{statusLock}
	return options
}

func (s PublishAttachment) Execute(ctx dex.Context, in progress) (*dex.StepDecision, error) {
	if readAttachment(ctx).ActiveCommand != nil {
		markDeleting(ctx)
		return dex.GoTo(DeleteAttachmentObjects{f: s.f}, progress{Ending: StatusDeleted}), nil
	}
	now := time.Now().UTC()
	if in.Prepared.Redacted != nil {
		comm.MustDo(originalAttribute.Set(ctx, &in.Prepared.Redacted.Ref))
	}
	comm.MustDo(preparedAttribute.Set(ctx, in.Prepared))
	comm.MustDo(statusAttribute.Set(ctx, StatusReady))
	comm.MustDo(readyAtAttribute.Set(ctx, now))
	comm.MustDo(expiresAtAttribute.Set(ctx, s.f.settings.expiry(now, comm.Must(stagingHeldUntilAttribute.Get(ctx)))))
	touch(ctx)
	return dex.GoTo(AwaitAttachmentExpiry{f: s.f}, progress{}), nil
}

// dex:group group-id:ready group-label:"Ready"
// dex:explanation text:"Keep the READY attachment staged until a deletion command or the end of its staging retention."
type AwaitAttachmentExpiry struct {
	dex.StepDefaults
	f *AttachmentFlow
}

func (AwaitAttachmentExpiry) GetStepOptions() *dex.StepOptions {
	options := dexflow.StateStepOptions()
	options.ExecuteLockAttributes = []dex.AttributeLock{statusLock}
	return options
}

func (AwaitAttachmentExpiry) WaitFor(ctx dex.Context, _ progress) (*dex.Wait, error) {
	expiresAt := comm.Must(expiresAtAttribute.Get(ctx))
	return dex.AnyOf(attachmentCommandsChannel.ForOne(), dex.Timer(max(expiresAt.Sub(ctx.FirstAttemptAt()), time.Second))), nil
}

func (s AwaitAttachmentExpiry) Execute(ctx dex.Context, _ progress) (*dex.StepDecision, error) {
	ending := StatusExpired
	if readAttachment(ctx).ActiveCommand != nil {
		ending = StatusDeleted
	}
	markDeleting(ctx)
	return dex.GoTo(DeleteAttachmentObjects{f: s.f}, progress{Ending: ending}), nil
}

// dex:group group-id:end group-label:"End"
// dex:explanation text:"Once every presigned upload of the attachment expired, delete its staging prefix with every version."
type DeleteAttachmentObjects struct {
	dex.StepDefaults
	f *AttachmentFlow
}

func (s DeleteAttachmentObjects) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(deletionBudget, failureRoute(s.f))
}

func (s DeleteAttachmentObjects) WaitFor(ctx dex.Context, _ progress) (*dex.Wait, error) {
	return dex.Until(dex.Timer(s.f.uploadsExpire(ctx))), nil
}

func (s DeleteAttachmentObjects) Execute(ctx dex.Context, in progress) (*dex.StepDecision, error) {
	if err := s.f.deleteObjects(ctx, readAttachment(ctx)); err != nil {
		return nil, err
	}
	return dex.GoTo(CompleteAttachment{f: s.f}, in), nil
}

// dex:group group-id:end group-label:"End"
// dex:explanation text:"Release the staging slot, which a deleted TeamSpace holds no more, and complete with the final status."
type CompleteAttachment struct {
	dex.StepDefaultsNoWaitFor[progress]
	f *AttachmentFlow
}

func (CompleteAttachment) GetStepOptions() *dex.StepOptions {
	options := dexflow.ProviderStepOptions(nil)
	options.ExecuteRetry = dexflow.StepRetry(releaseBudget)
	options.ExecuteLockAttributes = []dex.AttributeLock{statusLock}
	return options
}

func (s CompleteAttachment) Execute(ctx dex.Context, in progress) (*dex.StepDecision, error) {
	a := readAttachment(ctx)
	if err := s.f.teamSpaces.ReleaseAttachmentStaging(ctx, a.TeamSpaceID, a.AttachmentID); err != nil {
		return nil, dex.ErrorWithStack(fmt.Errorf("release the staging slot of attachment %s: %w", a.AttachmentID, err))
	}
	if in.Failure != nil {
		comm.MustDo(failureAttribute.Set(ctx, recordedFailure(*in.Failure)))
	}
	comm.MustDo(statusAttribute.Set(ctx, in.Ending))
	touch(ctx)
	return dex.GracefulComplete(readAttachment(ctx)), nil
}

// dex:group group-id:end group-label:"End"
// dex:explanation text:"Record why the attachment failed, delete its staging prefix and end it FAILED; a failed deletion is asked again."
type RecordAttachmentFailure struct {
	dex.StepDefaults
	f *AttachmentFlow
}

func (RecordAttachmentFailure) GetStepOptions() *dex.StepOptions {
	return dexflow.PollingStepOptions(deletionBudget, nil)
}

func (s RecordAttachmentFailure) WaitFor(ctx dex.Context, _ progress) (*dex.Wait, error) {
	return dex.Until(dex.Timer(s.f.uploadsExpire(ctx))), nil
}

func (s RecordAttachmentFailure) Execute(ctx dex.Context, in progress) (*dex.StepDecision, error) {
	ending := failureEnding(ctx, in)
	if err := s.f.deleteObjects(ctx, readAttachment(ctx)); err != nil {
		return nil, err
	}
	return dex.GoTo(CompleteAttachment{f: s.f}, ending), nil
}

// readAttachment reads the snapshot of the invoking execution.
func readAttachment(ctx dex.Context) Attachment {
	return Attachment{
		Identity:                comm.Must(startIdentityAttribute.Get(ctx)),
		TeamSpaceID:             comm.Must(teamSpaceIDAttribute.Get(ctx)),
		AttachmentID:            comm.Must(attachmentIDAttribute.Get(ctx)),
		UploaderAccountID:       comm.Must(uploaderAttribute.Get(ctx)),
		UploaderIsPlatformAdmin: comm.Must(uploaderIsAdminAttribute.Get(ctx)),
		FileName:                comm.Must(fileNameAttribute.Get(ctx)),
		MediaType:               comm.Must(mediaTypeAttribute.Get(ctx)),
		Kind:                    comm.Must(kindAttribute.Get(ctx)),
		ByteSize:                comm.Must(byteSizeAttribute.Get(ctx)),
		SHA256:                  comm.Must(sha256Attribute.Get(ctx)),
		Status:                  comm.Must(statusAttribute.Get(ctx)),
		Revision:                comm.Must(revisionAttribute.Get(ctx)),
		ActiveCommand:           dexflow.Optional(activeCommandAttribute.Get(ctx)),
		Original:                dexflow.Optional(originalAttribute.Get(ctx)),
		Prepared:                dexflow.Optional(preparedAttribute.Get(ctx)),
		Failure:                 dexflow.Optional(failureAttribute.Get(ctx)),
		CreatedAt:               comm.Must(createdAtAttribute.Get(ctx)),
		UpdatedAt:               comm.Must(updatedAtAttribute.Get(ctx)),
		ReadyAt:                 comm.Must(readyAtAttribute.Get(ctx)),
		ExpiresAt:               comm.Must(expiresAtAttribute.Get(ctx)),
		UploadClosedAt:          comm.Must(uploadClosedAtAttribute.Get(ctx)),
		StagingHeldUntil:        comm.Must(stagingHeldUntilAttribute.Get(ctx)),
	}
}

// markDeleting marks the attachment DELETING, before its Step routes to the deletion of its objects.
func markDeleting(ctx dex.Context) {
	comm.MustDo(statusAttribute.Set(ctx, StatusDeleting))
	touch(ctx)
}

// closeUpload records when the attachment stopped awaiting its upload.
func closeUpload(ctx dex.Context) {
	comm.MustDo(uploadClosedAtAttribute.Set(ctx, time.Now().UTC()))
}

// attachmentUpdate answers a write RPC with the snapshot after it, or with the rejection that left it unchanged.
type attachmentUpdate struct {
	Attachment Attachment
	Rejection  *dexflow.Rejection
}

func updated(ctx dex.Context, rejection *dexflow.Rejection) (*dex.RPCResult[attachmentUpdate], error) {
	return &dex.RPCResult[attachmentUpdate]{Output: attachmentUpdate{Attachment: readAttachment(ctx), Rejection: rejection}}, nil
}

func failureRoute(f *AttachmentFlow) *dex.ExecuteFailure {
	return dex.ProceedToOnExecuteFailure(RecordAttachmentFailure{f: f}, nil)
}

func touch(ctx dex.Context) {
	comm.MustDo(revisionAttribute.Set(ctx, comm.Must(revisionAttribute.Get(ctx))+1))
	comm.MustDo(updatedAtAttribute.Set(ctx, time.Now().UTC()))
}
