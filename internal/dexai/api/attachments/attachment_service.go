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
	"maps"
	"slices"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	attachmentflow "github.com/superdurable/superversev2/internal/dexai/agents/attachments"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// maxDownloads bounds the attachments one download request names.
const maxDownloads = 20

// attachmentServer serves AttachmentService. AttachmentFlow owns each
// attachment and checks its uploader on every write; the read model admits a
// TeamSpace member's upload before any Flow starts, and TeamSpaceFlow's
// reservation confirms the membership. Downloads are of the Project's copies,
// which only the Project's Session makes.
type attachmentServer struct {
	pb.UnimplementedAttachmentServiceServer
	*boundary.Boundary
	attachments *attachmentflow.AttachmentFlowClient
	renditions  *attachmentflow.RenditionStore
	members     *memberdao.MemberDAO
	projects    *projects.ProjectFlowClient
}

// Register serves AttachmentService on server.
func Register(server *grpc.Server, b *boundary.Boundary, attachments *attachmentflow.AttachmentFlowClient, renditions *attachmentflow.RenditionStore,
	members *memberdao.MemberDAO, projectClient *projects.ProjectFlowClient) {
	pb.RegisterAttachmentServiceServer(server, &attachmentServer{Boundary: b, attachments: attachments, renditions: renditions, members: members, projects: projectClient})
}

// CreateAttachmentUpload declares a file of a TeamSpace member or the
// platform admin and returns the attachment with, while it awaits its upload,
// the presigned PUT of its bytes.
func (s *attachmentServer) CreateAttachmentUpload(ctx context.Context, in *pb.CreateAttachmentUploadRequest) (*pb.CreateAttachmentUploadResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	if err = flowid.ValidateTeamSpaceID(in.GetTeamSpaceId()); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	if err = s.admitUploader(ctx, account, in.GetTeamSpaceId()); err != nil {
		return nil, err
	}
	a, target, err := s.attachments.Create(ctx, attachmentflow.Upload{
		TeamSpaceID: in.GetTeamSpaceId(), RequestID: in.GetRequestId(), FileName: in.GetFileName(), MediaType: in.GetMediaType(),
		ByteSize: in.GetByteSize(), SHA256: in.GetSha256(), Uploader: actor(account),
	})
	switch {
	case errors.Is(err, attachmentflow.ErrInvalidUpload):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, attachmentflow.ErrRegistrationPending):
		return nil, status.Error(codes.Unavailable, "the attachment is still being registered; retry with the same request ID")
	case err != nil:
		return nil, boundary.RPCStatus(err)
	}
	return &pb.CreateAttachmentUploadResponse{Attachment: attachmentSnapshot(a), Upload: uploadTarget(target)}, nil
}

// admitUploader refuses, before any Flow starts, an uploader whom the read
// model shows outside a TeamSpace it holds; TeamSpaceFlow's reservation
// confirms the membership strongly. A TeamSpace the read model does not hold
// yet, such as one created a moment ago, is left to that reservation.
func (s *attachmentServer) admitUploader(ctx context.Context, account accounts.AccountSummary, teamSpaceID string) error {
	if accounts.IsAdministrator(account) {
		return nil
	}
	role, err := s.members.TeamSpaceRole(ctx, teamSpaceID, account.AccountID, teamspaces.StatusDeleted)
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		return nil
	case err != nil:
		return boundary.RPCStatus(err)
	case role == "":
		return status.Error(codes.PermissionDenied, "only a member of the TeamSpace attaches files to it")
	}
	return nil
}

// CompleteAttachmentUpload ends the upload wait of the caller's attachment
// and answers once it is READY or FAILED, or after at most 20 seconds with the
// status it reached.
func (s *attachmentServer) CompleteAttachmentUpload(ctx context.Context, in *pb.CompleteAttachmentUploadRequest) (*pb.CompleteAttachmentUploadResponse, error) {
	account, err := s.authorizeAttachment(ctx, in.GetCaller(), in.GetTeamSpaceId(), in.GetAttachmentId())
	if err != nil {
		return nil, err
	}
	a, err := s.attachments.Complete(ctx, in.GetTeamSpaceId(), in.GetAttachmentId(), actor(account))
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.CompleteAttachmentUploadResponse{Attachment: attachmentSnapshot(a)}, nil
}

// DeleteAttachment deletes a staged attachment of the caller, or of anyone
// for the platform admin.
func (s *attachmentServer) DeleteAttachment(ctx context.Context, in *pb.DeleteAttachmentRequest) (*pb.DeleteAttachmentResponse, error) {
	account, err := s.authorizeAttachment(ctx, in.GetCaller(), in.GetTeamSpaceId(), in.GetAttachmentId())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	a, err := s.attachments.RequestDeletion(ctx, in.GetTeamSpaceId(), in.GetAttachmentId(), in.GetRequestId(), actor(account))
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.DeleteAttachmentResponse{Attachment: attachmentSnapshot(a)}, nil
}

// authorizeAttachment authenticates the caller of a write to an attachment
// and checks its identifiers; the attachment's Flow checks the uploader.
func (s *attachmentServer) authorizeAttachment(ctx context.Context, caller *pb.CallerContext, teamSpaceID, attachmentID string) (accounts.AccountSummary, error) {
	account, err := s.Authenticate(ctx, caller)
	if err != nil {
		return accounts.AccountSummary{}, err
	}
	if err = flowid.ValidateTeamSpaceID(teamSpaceID); err != nil {
		return accounts.AccountSummary{}, boundary.RPCStatus(err)
	}
	if err = flowid.ValidateAttachmentID(attachmentID); err != nil {
		return accounts.AccountSummary{}, boundary.RPCStatus(err)
	}
	return account, nil
}

// GetAttachmentDownloads presigns the downloads of the attachments the
// Project holds copies of, for a caller who may read its conversation; an
// attachment the Project's Session never showed has none.
func (s *attachmentServer) GetAttachmentDownloads(ctx context.Context, in *pb.GetAttachmentDownloadsRequest) (*pb.GetAttachmentDownloadsResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	ids := in.GetAttachmentIds()
	if err = boundary.InvalidIf(len(ids) == 0 || len(ids) > maxDownloads, "name 1 to 20 attachments"); err != nil {
		return nil, err
	}
	for index, id := range ids {
		if err = flowid.ValidateAttachmentID(id); err != nil {
			return nil, boundary.RPCStatus(err)
		}
		if slices.Contains(ids[:index], id) {
			return nil, status.Error(codes.InvalidArgument, "an attachment is named twice")
		}
	}
	disposition, err := boundary.DomainValue(in.GetDisposition())
	if err != nil {
		return nil, err
	}
	if _, err = s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityBuild); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.GetAttachmentDownloadsResponse{}
	for _, id := range ids {
		download, found, err := s.renditions.Download(ctx, in.GetProjectId(), id, disposition == "INLINE")
		if err != nil {
			return nil, boundary.RPCStatus(err)
		}
		if found {
			out.Downloads = append(out.Downloads, &pb.AttachmentDownload{AttachmentId: id, Url: download.URL, ExpiresAt: timestamp(download.Expires)})
		}
	}
	return out, nil
}

// actor is the account a request acts for.
func actor(account accounts.AccountSummary) teamspaces.Actor {
	return teamspaces.Actor{AccountID: account.AccountID, PlatformAdmin: accounts.IsAdministrator(account)}
}

// attachmentSnapshot shows an attachment: never its objects or content.
func attachmentSnapshot(a attachmentflow.Attachment) *pb.Attachment {
	out := &pb.Attachment{
		AttachmentId: a.AttachmentID, TeamSpaceId: a.TeamSpaceID, FileName: a.FileName, MediaType: a.MediaType,
		Kind: boundary.Enum[pb.AttachmentKind](a.Kind), ByteSize: a.ByteSize, Status: boundary.Enum[pb.AttachmentStatus](a.VisibleStatus()),
	}
	if prepared := a.Prepared; prepared != nil {
		summary := &pb.AttachmentSummary{}
		if image := prepared.Image; image != nil {
			summary.Width, summary.Height = int32(image.Width), int32(image.Height)
		}
		if document := prepared.Document; document != nil {
			summary.Sections, summary.ShownSections, summary.Truncated, summary.Pages = int32(document.Sections), int32(document.ExcerptSections), document.Truncated, int32(document.Pages)
		}
		if prepared.JSON != nil {
			summary.JsonFormat, summary.Nodes, summary.Redactions = string(prepared.JSON.Format), int32(prepared.JSON.Nodes), int32(prepared.JSON.Redactions)
		}
		out.Summary = summary
	}
	if code, sentence, failed := a.PublicFailure(); failed {
		out.Failure = &pb.AttachmentFailure{Code: code, Message: sentence}
	}
	if !a.ExpiresAt.IsZero() {
		out.ExpiresAt = timestamp(a.ExpiresAt)
	}
	return out
}

// uploadTarget shows the presigned PUT, with its headers in name order.
func uploadTarget(target *objectstore.PresignedRequest) *pb.AttachmentUploadTarget {
	if target == nil {
		return nil
	}
	out := &pb.AttachmentUploadTarget{Url: target.URL, ExpiresAt: timestamp(target.Expires)}
	for _, name := range slices.Sorted(maps.Keys(target.Headers)) {
		out.Headers = append(out.Headers, &pb.UploadHeader{Name: name, Value: target.Headers[name]})
	}
	return out
}

// timestamp is the RFC 3339 form of a time, as AgentMessage writes times.
func timestamp(at time.Time) string { return at.UTC().Format(time.RFC3339) }
