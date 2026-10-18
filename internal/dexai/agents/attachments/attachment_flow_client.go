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
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/fileextract"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// The bounded waits of the upload commands and their writes.
const (
	// registrationWait bounds the wait of a creation for the staging slot.
	registrationWait = 10 * time.Second
	// preparationWait bounds the wait of a completion for READY or FAILED.
	preparationWait = 20 * time.Second
	// lockWait bounds a write, including its wait for a Step that holds the
	// status lock.
	lockWait = 30 * time.Second
)

// maxFileNameCharacters bounds a file's name.
const maxFileNameCharacters = 200

// ErrInvalidUpload is a declared file the attachments refuse before any Flow
// starts: its name, its type or its size. Its message is safe to show.
var ErrInvalidUpload = errors.New("the file cannot be attached")

// ErrRegistrationPending is a creation whose Flow did not reserve its staging
// slot within the creation's wait; a retry with the same request joins it.
var ErrRegistrationPending = errors.New("the attachment's registration is still pending")

// Upload is a file its uploader declares before the browser PUTs it: the
// TeamSpace it is staged in, the uploader's idempotency key, the name, the
// media type, the exact length and the lowercase hex SHA-256 of its bytes.
type Upload struct {
	TeamSpaceID, RequestID, FileName, MediaType string
	ByteSize                                    int64
	SHA256                                      string
	Uploader                                    teamspaces.Actor
}

// AttachmentFlowClient fronts AttachmentFlow: the start of an upload, its
// completion and deletion with their bounded waits, its reads and the
// presigned upload of its original.
type AttachmentFlowClient struct {
	dex  *dex.Client
	flow *AttachmentFlow
}

// Create starts the AttachmentFlow of a declared file and waits, bounded, for
// its staging slot. It returns the attachment and, while it awaits its upload,
// the presigned PUT of the original, a bearer capability for the response
// only. A replay of the request joins the Flow it started, also a completed
// one, and answers its status; the same request with another file is a
// *dex.FlowAlreadyStartedError.
func (c *AttachmentFlowClient) Create(ctx context.Context, in Upload) (Attachment, *objectstore.PresignedRequest, error) {
	start, err := c.admit(in)
	if err != nil {
		return Attachment{}, nil, err
	}
	flowID := flowid.AttachmentFlowID(in.TeamSpaceID, start.attachmentID)
	if _, err = c.dex.StartFlow(ctx, c.flow, flowID, progress{}, dex.StartFlowOptions{
		IDReusePolicy: dex.IDReuseDisallow, RequestID: &start.identity.RequestID,
		AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: c.flow.InitialAttributes(start),
	}); err != nil {
		return Attachment{}, nil, err
	}
	a, settled, err := c.await(ctx, flowID, registrationWait, func(a Attachment) bool { return a.Status != StatusRegistering })
	switch {
	case err != nil:
		return Attachment{}, nil, err
	case !settled:
		return a, nil, ErrRegistrationPending
	case a.VisibleStatus() != StatusAwaitingUpload:
		return a, nil, nil
	}
	target, err := c.flow.objects.PresignPut(ctx, a.upload(), c.flow.settings.UploadURLValidity)
	if err != nil {
		return Attachment{}, nil, err
	}
	return a, &target, nil
}

// admit checks a declared file and returns the start of its Flow: the name,
// the extension and media type pair, the size of its kind and the digest.
func (c *AttachmentFlowClient) admit(in Upload) (upload, error) {
	invalid := func(format string, args ...any) (upload, error) {
		return upload{}, fmt.Errorf("%w: "+format, append([]any{ErrInvalidUpload}, args...)...)
	}
	format, accepted := fileextract.FormatOf(in.MediaType, in.FileName)
	switch {
	case !validFileName(in.FileName):
		return invalid("a file name has 1 to %d characters and no path separator or control character", maxFileNameCharacters)
	case !accepted:
		return invalid("attach PNG, JPEG, WebP or GIF images, Markdown, text, Word or PDF documents, or JSON, with the extension of their type")
	case in.ByteSize < 1 || in.ByteSize > c.flow.settings.maxBytes(format.Kind()):
		return invalid("a %s file has 1 to %d bytes", format.Kind(), c.flow.settings.maxBytes(format.Kind()))
	case !flowid.IsSHA256Hex(in.SHA256):
		return invalid("the SHA-256 digest is 64 lowercase hex characters")
	}
	if err := flowid.ValidateTeamSpaceID(in.TeamSpaceID); err != nil {
		return upload{}, err
	}
	start := upload{
		teamSpaceID: in.TeamSpaceID, attachmentID: flowid.AttachmentCreationID(in.Uploader.AccountID, in.RequestID), uploader: in.Uploader,
		fileName: in.FileName, mediaType: format.MediaType(), kind: attachmentKinds[format.Kind()], byteSize: in.ByteSize, sha256: in.SHA256,
	}
	fingerprint := flowid.Fingerprint(struct {
		AccountID, RequestID, TeamSpaceID, FileName, MediaType string
		ByteSize                                               int64
		SHA256                                                 string
	}{in.Uploader.AccountID, in.RequestID, in.TeamSpaceID, in.FileName, start.mediaType, in.ByteSize, in.SHA256})
	start.identity = dexflow.StartIdentity{RequestID: fingerprint, Fingerprint: fingerprint, ActorAccountID: in.Uploader.AccountID}
	return start, nil
}

// validFileName admits 1 to maxFileNameCharacters characters of valid UTF-8
// without a path separator or a control character.
func validFileName(name string) bool {
	count := utf8.RuneCountInString(name)
	return utf8.ValidString(name) && count >= 1 && count <= maxFileNameCharacters && !strings.ContainsAny(name, `/\`) &&
		strings.IndexFunc(name, unicode.IsControl) < 0 && strings.TrimSpace(name) == name
}

// Complete ends the upload wait as the uploader and waits, bounded, until the
// attachment is READY or FAILED, or no longer verifies or prepares its upload;
// a repeat publishes nothing new and waits again.
func (c *AttachmentFlowClient) Complete(ctx context.Context, teamSpaceID, attachmentID string, uploader teamspaces.Actor) (Attachment, error) {
	flowID := flowid.AttachmentFlowID(teamSpaceID, attachmentID)
	if err := c.write(ctx, flowID, c.flow.CompleteAttachmentUpload, UploadCompletion{AccountID: uploader.AccountID}, completionRefusal(uploader)); err != nil {
		return Attachment{}, err
	}
	a, _, err := c.await(ctx, flowID, preparationWait, func(a Attachment) bool {
		return a.ActiveCommand != nil || a.Status != StatusVerifying && a.Status != StatusPreparing
	})
	return a, err
}

// RequestDeletion admits the deletion of the attachment as actor under the
// command of the actor's request ID, and returns the attachment.
func (c *AttachmentFlowClient) RequestDeletion(ctx context.Context, teamSpaceID, attachmentID, requestID string, actor teamspaces.Actor) (Attachment, error) {
	flowID := flowid.AttachmentFlowID(teamSpaceID, attachmentID)
	command := deletionRequest{ID: flowid.Fingerprint(struct{ AccountID, RequestID string }{actor.AccountID, requestID}), Actor: actor}
	if err := c.write(ctx, flowID, c.flow.RequestAttachmentDeletion, command, deletionRefusal(actor)); err != nil {
		return Attachment{}, err
	}
	return c.get(ctx, teamSpaceID, attachmentID)
}

// get reads the attachment, also a completed one; an attachment that never
// existed is *dex.FlowNotFoundError.
func (c *AttachmentFlowClient) get(ctx context.Context, teamSpaceID, attachmentID string) (Attachment, error) {
	return c.read(ctx, flowid.AttachmentFlowID(teamSpaceID, attachmentID))
}

// GetForTest is get for the tests of other packages.
func (c *AttachmentFlowClient) GetForTest(ctx context.Context, teamSpaceID, attachmentID string) (Attachment, error) {
	return c.get(ctx, teamSpaceID, attachmentID)
}

func (c *AttachmentFlowClient) read(ctx context.Context, flowID string) (Attachment, error) {
	var a Attachment
	err := dexflow.InvokeRetainedRead(ctx, c.dex, flowID, c.flow.GetAttachmentState, nil, &a)
	return a, err
}

// write calls a write RPC, with its rejection as the error. A completed
// attachment changes no more, so a write to it is answered by reading it,
// after refused applies the RPC's caller check to what it read: the RPC
// cannot check a closed execution's caller itself.
func (c *AttachmentFlowClient) write(ctx context.Context, flowID string, rpc, in any, refused func(Attachment) *dexflow.Rejection) error {
	result, err := dexflow.InvokeLocked[attachmentUpdate](ctx, c.dex, lockWait, flowID, rpc, in)
	if _, closed := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err); closed {
		a, err := c.read(ctx, flowID)
		if err != nil {
			return err
		}
		return refused(a).Err()
	}
	if err != nil {
		return err
	}
	return result.Rejection.Err()
}

// await reads the attachment until done reports true or it ended, waiting
// for each change of its Revision within budget; false when budget ended
// first, with the last state read.
func (c *AttachmentFlowClient) await(ctx context.Context, flowID string, budget time.Duration, done func(Attachment) bool) (Attachment, bool, error) {
	deadline := time.Now().Add(budget)
	for {
		a, err := c.read(ctx, flowID)
		switch {
		case err != nil:
			return Attachment{}, false, err
		case done(a) || isTerminal(a.Status):
			return a, true, nil
		case time.Until(deadline) < time.Second:
			return a, false, nil
		}
		var revision uint64
		err = c.dex.WaitForAttributeMatch(ctx, flowID, revisionAttribute, dex.AttributeMatchGreaterThan(a.Revision), &revision,
			dex.WaitForAttributeOptions{RequestTimeout: time.Until(deadline).Truncate(time.Second)})
		_, timedOut := errors.AsType[*dex.RequestTimeoutError](err)
		_, closed := errors.AsType[*dex.FlowNotActiveOrNotFoundError](err)
		switch {
		case timedOut:
			return a, false, nil
		case closed:
			// A closed execution changes no more: its last state is the answer.
			a, err = c.read(ctx, flowID)
			return a, err == nil && (done(a) || isTerminal(a.Status)), err
		case err != nil:
			return Attachment{}, false, err
		}
	}
}
