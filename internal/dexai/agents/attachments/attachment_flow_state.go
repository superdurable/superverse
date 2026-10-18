// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package attachments

import (
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/fileextract"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
)

// The values below are persisted in AttachmentFlow Attributes, its Channels,
// its Step inputs and its RPC payloads. Their field names are their JSON
// encoding, so they carry no tags and are never renamed.

// Status is a value of the AttachmentStatus Attribute; its constants declare
// the set, and the API maps each to the enum value of the same name.
type Status = string

const (
	StatusRegistering    Status = "REGISTERING"
	StatusAwaitingUpload Status = "AWAITING_UPLOAD"
	StatusVerifying      Status = "VERIFYING"
	StatusPreparing      Status = "PREPARING"
	StatusReady          Status = "READY"
	StatusDeleting       Status = "DELETING"
	StatusExpired        Status = "EXPIRED"
	StatusDeleted        Status = "DELETED"
	StatusFailed         Status = "FAILED"
)

// isTerminal reports whether status ends the attachment: its objects are gone
// and its reservation is released.
func isTerminal(status Status) bool {
	return status == StatusExpired || status == StatusDeleted || status == StatusFailed
}

// failureCode is why an attachment FAILED, with the sentence the API shows
// for it. A struct that string literals cannot convert to; its values below
// are persisted by their names, and Studio says each in words of its own.
type failureCode struct{ name, sentence string }

const unreadable = "The file could not be read. Try attaching it again."

var (
	failureQuotaExceeded      = failureCode{"attachment_quota_exceeded", "Your team has too many files waiting to be sent. Send or remove some, or try again tomorrow."}
	failureTeamSpaceClosed    = failureCode{"teamspace_closed", "The TeamSpace is being deleted."}
	failureNotMember          = failureCode{"not_member", "Only a member of the TeamSpace attaches files to it."}
	failureUploadMissing      = failureCode{"upload_missing", unreadable}
	failureUploadMismatch     = failureCode{"upload_mismatch", unreadable}
	failureUnsupportedContent = failureCode{"unsupported_content", "This file is not a valid PNG, JPEG, WebP, GIF, Markdown, text, Word, PDF or JSON file."}
	failureTooLarge           = failureCode{"too_large", "This file is larger than the limit for its type."}
	failureEncrypted          = failureCode{"encrypted", "This PDF is password-protected."}
	failureNoText             = failureCode{"no_text", "This PDF has no selectable text. Attach a PDF with text, or an image of the page."}
	failureInvalidJSON        = failureCode{"invalid_json", "This JSON could not be read: it is not valid or is nested too deeply."}
	failureExtractionFailed   = failureCode{"extraction_failed", unreadable}
	failurePreparationFailed  = failureCode{"preparation_failed", unreadable}
	failureCodes              = []failureCode{
		failureQuotaExceeded, failureTeamSpaceClosed, failureNotMember, failureUploadMissing, failureUploadMismatch, failureUnsupportedContent,
		failureTooLarge, failureEncrypted, failureNoText, failureInvalidJSON, failureExtractionFailed, failurePreparationFailed,
	}
)

func (code failureCode) String() string { return code.name }

// extractorFailures are the failure codes of the extractor's refusals: a
// mismatched or oversized image is unsupported or too large as any file is.
var extractorFailures = map[fileextract.Code]failureCode{
	fileextract.CodeUnsupportedContent: failureUnsupportedContent, fileextract.CodeImageMismatch: failureUnsupportedContent,
	fileextract.CodeTooLarge: failureTooLarge, fileextract.CodeImageTooLarge: failureTooLarge, fileextract.CodeEncrypted: failureEncrypted,
	fileextract.CodeNoText: failureNoText, fileextract.CodeInvalidJSON: failureInvalidJSON, fileextract.CodeExtractionFailed: failureExtractionFailed,
}

// failurePhase is the phase every failure record of an attachment names.
const failurePhase = "PrepareAttachment"

// AttachmentCommand is an admitted deletion of the attachment. The same value
// is the Channel message and the ActiveCommand record. ID is derived by the
// API from the caller and the request ID, and is the command's identity.
type AttachmentCommand struct {
	ID, Kind, ActorAccountID string
}

// commandDeleteAttachment is the one kind of AttachmentCommand. The value is
// persisted.
const commandDeleteAttachment = "DELETE_ATTACHMENT"

// UploadCompletion ends the wait for the upload early; the head read decides
// whether the upload arrived, so nothing else in it is trusted.
type UploadCompletion struct{ AccountID string }

// completionRefusal refuses a completion of an attachment by anyone but its
// uploader; nil admits it. The RPC and, for a completed execution, the
// client apply it.
func completionRefusal(caller teamspaces.Actor) func(Attachment) *dexflow.Rejection {
	return func(a Attachment) *dexflow.Rejection {
		if caller.AccountID != a.UploaderAccountID {
			return dexflow.Reject(dexflow.RejectionPermissionDenied, "only the uploader completes the upload")
		}
		return nil
	}
}

// deletionRefusal refuses a deletion of an attachment by anyone but its
// uploader or the platform admin; nil admits it.
func deletionRefusal(actor teamspaces.Actor) func(Attachment) *dexflow.Rejection {
	return func(a Attachment) *dexflow.Rejection {
		if !actor.PlatformAdmin && actor.AccountID != a.UploaderAccountID {
			return dexflow.Reject(dexflow.RejectionPermissionDenied, "only the uploader or the platform admin deletes an attachment")
		}
		return nil
	}
}

// deletionRequest asks for the attachment's deletion as Actor under the
// command ID.
type deletionRequest struct {
	ID    string
	Actor teamspaces.Actor
}

// preparedObject is one prepared object of an attachment: its exact staged
// version, its size and, for a rendition, its estimated context tokens.
type preparedObject struct {
	Ref           objectstore.ObjectRef
	Bytes         int64
	ContextTokens int64 `json:",omitempty"`
}

// PreparedImage is what an image's header says.
type PreparedImage struct{ Width, Height, Frames int }

// PreparedDocument is what a document's text holds: its sections, its text
// bytes, a PDF's pages, the sections the full rendition shows, and whether
// the extractor cut the text.
type PreparedDocument struct {
	Sections, TextBytes, Pages, ExcerptSections int
	Truncated                                   bool
}

// PreparedJSON is what a JSON file is: its format, an export's workflows,
// nodes and triggers and the credentials it names (at most
// maxNamedCredentials), and how many secret values preparation replaced.
type PreparedJSON struct {
	Format                     conversation.AttachmentJSONFormat
	Workflows, Nodes, Triggers int
	Credentials                []string `json:",omitempty"`
	Redactions                 int
}

// PreparedAttachment is what preparation made of the file: the rendition the
// model reads, a document's or a small generic JSON file's brief, a
// document's sections bundle, a JSON file's redacted copy, which replaces the
// upload, the facts of its kind, the bytes of every object it wrote and the
// versions of the rules that made them.
type PreparedAttachment struct {
	Rendition                          preparedObject
	Brief, Bundle, Redacted            *preparedObject   `json:",omitempty"`
	Image                              *PreparedImage    `json:",omitempty"`
	Document                           *PreparedDocument `json:",omitempty"`
	JSON                               *PreparedJSON     `json:",omitempty"`
	ObjectBytes                        int64
	ExtractorVersion, RenditionVersion string
}

// progress is what one Step hands the next: whether the upload wait's timer
// ended the wait, what preparation made, a failure a Step decided and the
// status the attachment ends with.
type progress struct {
	UploadWaitEnded bool                `json:",omitempty"`
	Prepared        *PreparedAttachment `json:",omitempty"`
	Failure         *decidedFailure     `json:",omitempty"`
	Ending          Status              `json:",omitempty"`
}

// decidedFailure is a failure a Step decided: its code's name and the
// detail of its cause, which names no content.
type decidedFailure struct{ Code, Detail string }

// decide is the progress into RecordAttachmentFailure of code for detail.
func decide(code failureCode, detail string) progress {
	return progress{Failure: &decidedFailure{Code: code.name, Detail: dexflow.TruncateCause(detail)}}
}

// upload is the declared file an AttachmentFlow starts with.
type upload struct {
	identity                  dexflow.StartIdentity
	teamSpaceID, attachmentID string
	uploader                  teamspaces.Actor
	fileName, mediaType       string
	kind                      conversation.AttachmentKind
	byteSize                  int64
	sha256                    string
}

// Attachment is the AttachmentFlow snapshot its RPCs and its completion
// return.
type Attachment struct {
	Identity                                 dexflow.StartIdentity
	TeamSpaceID, AttachmentID                string
	UploaderAccountID                        string
	UploaderIsPlatformAdmin                  bool
	FileName, MediaType                      string
	Kind                                     conversation.AttachmentKind
	ByteSize                                 int64
	SHA256                                   string
	Status                                   Status
	Revision                                 uint64
	ActiveCommand                            *AttachmentCommand
	Original                                 *objectstore.ObjectRef
	Prepared                                 *PreparedAttachment
	Failure                                  *dexflow.Failure
	CreatedAt, UpdatedAt, ReadyAt, ExpiresAt time.Time
	// UploadClosedAt is when the attachment stopped awaiting its upload, the
	// last moment the API presigned a PUT of it.
	UploadClosedAt time.Time
	// StagingHeldUntil is the latest moment the Flow releases its staging
	// slot; past it the TeamSpace's next upload that finds the quota full
	// reclaims the slot and deletes its objects.
	StagingHeldUntil time.Time
}

// VisibleStatus is the status the attachment's owner sees: DELETING once a
// deletion command is admitted, before its Steps start it.
func (a Attachment) VisibleStatus() Status {
	if a.ActiveCommand != nil && !isTerminal(a.Status) {
		return StatusDeleting
	}
	return a.Status
}

// PublicFailure is the code of a FAILED attachment's failure and the sentence
// the API shows for it; false for any other attachment. The recorded message
// keeps the cause for diagnosis, which may name internal resources.
func (a Attachment) PublicFailure() (code, sentence string, failed bool) {
	if a.Failure == nil || a.Status != StatusFailed {
		return "", "", false
	}
	for _, known := range failureCodes {
		if known.name == a.Failure.Code {
			return known.name, known.sentence, true
		}
	}
	panic("attachment failure code " + a.Failure.Code + " is not declared")
}

// stagingPrefix is the staging prefix of the attachment, which its Flow owns:
// attachment-staging/<teamSpaceID>/<attachmentID>/.
func stagingPrefix(teamSpaceID, attachmentID string) string {
	return "attachment-staging/" + teamSpaceID + "/" + attachmentID + "/"
}

// The roles of an attachment's objects, the first path element below its
// prefix; each object is <role>/<hex sha256> of its own bytes.
const (
	roleOriginal  = "original"
	roleRedacted  = "redacted"
	roleRendition = "rendition"
	roleBrief     = "brief"
	roleBundle    = "bundle"
)

// objectKey is the key of the object with digest in role below prefix.
func objectKey(prefix, role, digest string) string {
	return prefix + role + "/" + strings.TrimPrefix(digest, "sha256:")
}

// ReferenceForTest is reference for the tests of other packages.
func (a Attachment) ReferenceForTest() conversation.AttachmentRef { return a.reference() }

// reference is what a message carries of a READY attachment: identity,
// description and its staged objects by exact version, never content.
func (a Attachment) reference() conversation.AttachmentRef {
	prepared := a.Prepared
	object := func(prepared preparedObject) conversation.AttachmentObject {
		return conversation.AttachmentObject{Digest: prepared.Ref.Digest, Bytes: prepared.Bytes, Version: prepared.Ref.Version}
	}
	rendition := func(prepared *preparedObject) *conversation.AttachmentRendition {
		if prepared == nil {
			return nil
		}
		return &conversation.AttachmentRendition{AttachmentObject: object(*prepared), ContextTokens: prepared.ContextTokens}
	}
	ref := conversation.AttachmentRef{
		ID: a.AttachmentID, TeamSpaceID: a.TeamSpaceID, Kind: a.Kind, Name: a.FileName, MediaType: a.MediaType, ByteSize: a.ByteSize,
		Original:  conversation.AttachmentObject{Digest: a.Original.Digest, Bytes: a.ByteSize, Version: a.Original.Version},
		Rendition: *rendition(&prepared.Rendition), Brief: rendition(prepared.Brief),
	}
	if prepared.Bundle != nil {
		bundle := object(*prepared.Bundle)
		ref.Bundle = &bundle
	}
	if prepared.Image != nil {
		ref.Image = &conversation.AttachmentImage{Width: prepared.Image.Width, Height: prepared.Image.Height}
	}
	if prepared.JSON != nil {
		ref.Original.Bytes = prepared.Redacted.Bytes
		ref.JSON = &conversation.AttachmentJSON{Format: prepared.JSON.Format, Redactions: prepared.JSON.Redactions}
	}
	return ref
}
