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
	"encoding/json"
	"errors"

	"golang.org/x/sync/errgroup"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/projects"
)

// maximumParallelReads bounds the AttachmentFlow reads of one admission.
const maximumParallelReads = 4

// AttachmentFlowService admits the attachments a person's message carries, a
// coordination that spans AttachmentFlow, which owns each file, and
// ProjectFlow, which counts what the Project's messages sent over its
// lifetime. The API's SendMessage and CreateProject use it; no Flow does.
type AttachmentFlowService struct {
	attachments   *AttachmentFlowClient
	projects      *projects.ProjectFlowClient
	maxPerMessage int
}

// NewAttachmentFlowService admits at most maxPerMessage attachments
// (attachments.maxPerMessage) per message, which never exceeds
// conversation.MaximumMessageAttachments.
func NewAttachmentFlowService(attachments *AttachmentFlowClient, projectClient *projects.ProjectFlowClient, maxPerMessage int) *AttachmentFlowService {
	if maxPerMessage < 1 || maxPerMessage > conversation.MaximumMessageAttachments {
		panic("attachments.maxPerMessage exceeds the Session's bound")
	}
	return &AttachmentFlowService{attachments: attachments, projects: projectClient, maxPerMessage: maxPerMessage}
}

// MaxPerMessage is how many attachments one message carries at most.
func (s *AttachmentFlowService) MaxPerMessage() int { return s.maxPerMessage }

// RefusalCode is why an admission refused a message's attachment. A struct
// that string literals cannot convert to; the API maps each value to its
// status, and its name is the stable code the status message starts with.
type RefusalCode struct{ name string }

var (
	// RefusalNotFound is an attachment the Project's TeamSpace does not hold.
	RefusalNotFound = RefusalCode{"attachment_not_found"}
	// RefusalNotYours is another person's upload.
	RefusalNotYours = RefusalCode{"attachment_not_yours"}
	// RefusalNotReady is an attachment still uploading or preparing, or one that failed.
	RefusalNotReady = RefusalCode{"attachment_not_ready"}
	// RefusalExpired is an attachment whose staging ended or that was deleted.
	RefusalExpired = RefusalCode{"attachment_expired"}
	// RefusalQuotaExceeded is a message past the Project's lifetime quota.
	RefusalQuotaExceeded = RefusalCode{"attachment_quota_exceeded"}
)

func (code RefusalCode) String() string { return code.name }

// AdmissionRefusal is the definite refusal of a message for one of its
// attachments, before anything was reserved. Its message names no content.
type AdmissionRefusal struct {
	Code    RefusalCode
	message string
}

func (refusal *AdmissionRefusal) Error() string {
	return refusal.Code.name + ": " + refusal.message
}

// MessageAdmission is one admission of a message's attachments: their
// references in order, and the admission that holds them in the Project.
type MessageAdmission struct {
	References []conversation.AttachmentRef
	id         string
	ids        []string
}

// AdmitMessage admits the attachments ids of a message the account sends to
// the Project projectID of the TeamSpace teamSpaceID: each READY and the
// account's own upload. It reserves them in the Project under messageID and
// a new admission, which holds them until ReleaseMessage. A refused
// attachment is an *AdmissionRefusal and reserves nothing; a Project that
// admits no attachments, such as one being deleted, is ProjectFlow's
// *dexflow.Rejection. A retry with the same message ID counts nothing twice.
func (s *AttachmentFlowService) AdmitMessage(ctx context.Context, accountID, projectID, teamSpaceID, messageID string, ids []string) (MessageAdmission, error) {
	admitted, err := s.admit(ctx, accountID, teamSpaceID, ids)
	if err != nil {
		return MessageAdmission{}, err
	}
	admission := MessageAdmission{References: make([]conversation.AttachmentRef, 0, len(admitted)), id: flowid.RandomID(24), ids: ids}
	files := make([]projects.AttachedFile, 0, len(admitted))
	for _, a := range admitted {
		files, admission.References = append(files, a.attachedFile()), append(admission.References, a.reference())
	}
	err = s.projects.ReserveAttachments(ctx, projectID, projects.AttachmentReservation{MessageID: messageID, AdmissionID: admission.id, Files: files})
	if rejected, isRejected := errors.AsType[*dexflow.Rejection](err); isRejected && rejected.HasCode(dexflow.RejectionResourceExhausted) {
		return MessageAdmission{}, &AdmissionRefusal{Code: RefusalQuotaExceeded, message: rejected.Message}
	}
	if err != nil {
		return MessageAdmission{}, err
	}
	return admission, nil
}

// ReleaseMessage drops the holds of an admission whose send the Session
// definitely refused. An attachment another admission holds stays counted:
// another message, or another send of the same message, may have delivered
// it.
func (s *AttachmentFlowService) ReleaseMessage(ctx context.Context, projectID string, admission MessageAdmission) error {
	return s.projects.ReleaseAttachments(ctx, projectID, admission.id, admission.ids)
}

// ProjectStartAttachments are the attachments of a creation's first message,
// as the admission read them.
type ProjectStartAttachments struct {
	read     []Attachment
	complete bool
}

// UploadIdentity names an attachment's file in a creation's fingerprint: its
// ID and the SHA-256 of its upload.
type UploadIdentity struct{ ID, SHA256 string }

// AdmitProjectStart admits the attachments ids of the first message of a
// Project the account creates in teamSpaceID, as AdmitMessage admits them;
// ProjectFlow counts them when it starts. A refusal also returns what the
// admission read, so the API can recognize a retry of an accepted creation
// whose attachments expired since.
func (s *AttachmentFlowService) AdmitProjectStart(ctx context.Context, accountID, teamSpaceID string, ids []string) (ProjectStartAttachments, error) {
	read, err := s.admit(ctx, accountID, teamSpaceID, ids)
	complete := len(read) == len(ids)
	for _, a := range read {
		complete = complete && a.AttachmentID != ""
	}
	return ProjectStartAttachments{read: read, complete: complete}, err
}

// Uploads are the identities of every attachment the admission read, in
// order; false when it could not read one of them.
func (p ProjectStartAttachments) Uploads() ([]UploadIdentity, bool) {
	uploads := make([]UploadIdentity, 0, len(p.read))
	for _, a := range p.read {
		uploads = append(uploads, UploadIdentity{ID: a.AttachmentID, SHA256: a.SHA256})
	}
	return uploads, p.complete
}

// Session are the admitted attachments as the Project's start carries them to
// its Session.
func (p ProjectStartAttachments) Session() []projects.SessionAttachment {
	out := make([]projects.SessionAttachment, 0, len(p.read))
	for _, a := range p.read {
		out = append(out, projects.SessionAttachment{File: a.attachedFile(), Reference: comm.Must(json.Marshal(a.reference()))})
	}
	return out
}

// WorkflowExport reports whether an admitted attachment is an automation
// export, which is the behavior authority of the Project it starts.
func (p ProjectStartAttachments) WorkflowExport() bool {
	for _, a := range p.read {
		if a.reference().IsWorkflowExport() {
			return true
		}
	}
	return false
}

// admit reads the attachments ids of teamSpaceID, at most
// maximumParallelReads at a time, and returns them in order once each is
// READY and uploaded by accountID. Otherwise it returns the *AdmissionRefusal
// of the first refused one, in order, with every snapshot it read; an
// attachment the TeamSpace does not hold reads as the zero snapshot.
// Another person's upload is refused before its status is looked at.
func (s *AttachmentFlowService) admit(ctx context.Context, accountID, teamSpaceID string, ids []string) ([]Attachment, error) {
	read := make([]Attachment, len(ids))
	group, reads := errgroup.WithContext(ctx)
	group.SetLimit(maximumParallelReads)
	for index, id := range ids {
		group.Go(func() error {
			a, err := s.attachments.get(reads, teamSpaceID, id)
			if _, absent := errors.AsType[*dex.FlowNotFoundError](err); absent {
				return nil
			}
			read[index] = a
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	for index, a := range read {
		refuse := func(code RefusalCode, message string) ([]Attachment, error) {
			return read, &AdmissionRefusal{Code: code, message: message}
		}
		switch status := a.VisibleStatus(); {
		case a.AttachmentID == "":
			return refuse(RefusalNotFound, "the TeamSpace holds no attachment "+ids[index])
		case a.UploaderAccountID != accountID:
			return refuse(RefusalNotYours, "only its uploader sends attachment "+a.AttachmentID)
		case status == StatusExpired || status == StatusDeleted || status == StatusDeleting:
			return refuse(RefusalExpired, "attachment "+a.AttachmentID+" expired or was removed; attach the file again")
		case status != StatusReady:
			return refuse(RefusalNotReady, "attachment "+a.AttachmentID+" is "+status+", not READY")
		}
	}
	return read, nil
}

// attachedFile is the file a READY attachment adds to a Project: what its
// Project copy holds.
func (a Attachment) attachedFile() projects.AttachedFile {
	return projects.AttachedFile{ID: a.AttachmentID, Kind: a.Kind.String(), MediaType: a.MediaType, FileName: a.FileName, SHA256: a.SHA256, Bytes: a.Prepared.ObjectBytes}
}
