// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package tenancy

import (
	"context"
	"fmt"
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/projects"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspacedao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/teamspaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// teamSpaceServer serves TeamSpaceService. TeamSpaceFlow admits its writes;
// its reads come from the read model, for a member of the TeamSpace or the
// platform admin.
type teamSpaceServer struct {
	pb.UnimplementedTeamSpaceServiceServer
	*boundary.Boundary
	teamSpaces *teamspaces.TeamSpaceFlowClient
	reads      *teamspacedao.TeamSpaceDAO
}

// CreateTeamSpace starts a TeamSpace whose one admin is the caller. Repeating
// a request returns the same TeamSpace; the same request ID with another name
// is ALREADY_EXISTS.
func (s *teamSpaceServer) CreateTeamSpace(ctx context.Context, in *pb.CreateTeamSpaceRequest) (*pb.CreateTeamSpaceResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	if err = validateName(in.GetName()); err != nil {
		return nil, err
	}
	teamSpaceID := flowid.TeamSpaceCreationID(account.AccountID, in.GetRequestId())
	start := teamspaces.Start{Name: in.GetName(), CreatorAccountID: account.AccountID}
	fingerprint := flowid.Fingerprint(struct{ AccountID, RequestID, Name string }{account.AccountID, in.GetRequestId(), in.GetName()})
	start.Identity = dexflow.StartIdentity{RequestID: fingerprint, Fingerprint: fingerprint, ActorAccountID: account.AccountID}
	if err = s.teamSpaces.Create(ctx, teamSpaceID, start); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.CreateTeamSpaceResponse{TeamSpaceId: teamSpaceID}, nil
}

// ListTeamSpaces lists the caller's TeamSpaces, or every TeamSpace for the
// platform admin, newest first; DELETED is never listed.
func (s *teamSpaceServer) ListTeamSpaces(ctx context.Context, in *pb.ListTeamSpacesRequest) (*pb.ListTeamSpacesResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	statuses, err := boundary.DomainValues(in.GetStatuses())
	if err != nil {
		return nil, err
	}
	all := accounts.IsAdministrator(account)
	page, err := s.ReadPage(fmt.Sprintf("teamspaces/%s/%t/%s", account.AccountID, all, strings.Join(statuses, ",")), in)
	if err != nil {
		return nil, err
	}
	rows, next, err := s.reads.ListTeamSpaces(ctx, account.AccountID, all, teamspaces.StatusDeleted, statuses, page.After, page.Size)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.ListTeamSpacesResponse{NextPageToken: s.NextPageToken(page, next)}
	for _, row := range rows {
		out.TeamSpaces = append(out.TeamSpaces, teamSpaceRowSnapshot(row))
	}
	return out, nil
}

func (s *teamSpaceServer) GetTeamSpace(ctx context.Context, in *pb.GetTeamSpaceRequest) (*pb.GetTeamSpaceResponse, error) {
	detail, err := s.readTeamSpace(ctx, in.GetCaller(), in.GetTeamSpaceId())
	if err != nil {
		return nil, err
	}
	return &pb.GetTeamSpaceResponse{TeamSpace: teamSpaceRowSnapshot(detail.TeamSpaceRow)}, nil
}

func (s *teamSpaceServer) GetTeamSpaceOverview(ctx context.Context, in *pb.GetTeamSpaceOverviewRequest) (*pb.GetTeamSpaceOverviewResponse, error) {
	detail, err := s.readTeamSpace(ctx, in.GetCaller(), in.GetTeamSpaceId())
	if err != nil {
		return nil, err
	}
	return &pb.GetTeamSpaceOverviewResponse{Overview: &pb.TeamSpaceOverview{TeamSpace: teamSpaceRowSnapshot(detail.TeamSpaceRow),
		MemberCount: detail.MemberCount, ProjectCount: detail.ProjectCount, LiveProjectCount: detail.LiveProjectCount, ConnectionCount: detail.ConnectionCount}}, nil
}

// readTeamSpace reads a TeamSpace that is not DELETED for one of its members
// or the platform admin. The row of a TeamSpace created a moment ago is
// waited for (teamspaces.AwaitRow).
func (s *teamSpaceServer) readTeamSpace(ctx context.Context, caller *pb.CallerContext, teamSpaceID string) (teamspacedao.TeamSpaceDetail, error) {
	account, err := s.Authenticate(ctx, caller)
	if err != nil {
		return teamspacedao.TeamSpaceDetail{}, err
	}
	if err = flowid.ValidateTeamSpaceID(teamSpaceID); err != nil {
		return teamspacedao.TeamSpaceDetail{}, boundary.RPCStatus(err)
	}
	detail, err := teamspaces.AwaitRow(ctx, s.teamSpaces, teamSpaceID, func(ctx context.Context) (teamspacedao.TeamSpaceDetail, error) {
		return s.reads.TeamSpace(ctx, teamSpaceID, account.AccountID, teamspaces.StatusDeleted, projects.StatusDeleted, string(connectiondao.ConnectionActive))
	})
	if err != nil {
		return teamspacedao.TeamSpaceDetail{}, boundary.RPCStatus(err)
	}
	if !detail.CallerRole.Valid && !accounts.IsAdministrator(account) {
		return teamspacedao.TeamSpaceDetail{}, errTeamSpaceForbidden
	}
	return detail, nil
}

// RenameTeamSpace renames the TeamSpace at the revision the caller observed;
// TeamSpaceFlow admits only an admin or the platform admin.
func (s *teamSpaceServer) RenameTeamSpace(ctx context.Context, in *pb.RenameTeamSpaceRequest) (*pb.RenameTeamSpaceResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = flowid.ValidateTeamSpaceID(in.GetTeamSpaceId()); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	if err = validateName(in.GetName()); err != nil {
		return nil, err
	}
	actor := teamspaces.Actor{AccountID: account.AccountID, PlatformAdmin: accounts.IsAdministrator(account)}
	renamed, err := s.teamSpaces.Rename(ctx, in.GetTeamSpaceId(), teamspaces.Renaming{Actor: actor, ExpectedRevision: in.GetExpectedRevision(), Name: in.GetName()})
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	// TeamSpaceFlow admitted the rename only from an admin, unless from the platform admin.
	role := teamspaces.RoleAdmin
	if actor.PlatformAdmin {
		role = ""
	}
	return &pb.RenameTeamSpaceResponse{TeamSpace: teamSpaceSnapshot(in.GetTeamSpaceId(), renamed, role)}, nil
}

// DeleteTeamSpace admits the platform admin's deletion of a TeamSpace, which
// deletes its Projects; it answers with the DELETING TeamSpace.
func (s *teamSpaceServer) DeleteTeamSpace(ctx context.Context, in *pb.DeleteTeamSpaceRequest) (*pb.DeleteTeamSpaceResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = flowid.ValidateTeamSpaceID(in.GetTeamSpaceId()); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	if err = boundary.ValidateRequestID(in.GetRequestId()); err != nil {
		return nil, err
	}
	actor := teamspaces.Actor{AccountID: account.AccountID, PlatformAdmin: accounts.IsAdministrator(account)}
	deleting, err := s.teamSpaces.RequestDeletion(ctx, in.GetTeamSpaceId(), teamspaces.DeletionRequest{Actor: actor, ID: in.GetRequestId()})
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.DeleteTeamSpaceResponse{TeamSpace: teamSpaceSnapshot(in.GetTeamSpaceId(), deleting, "")}, nil
}

// errTeamSpaceForbidden refuses a caller who is neither a member of the
// TeamSpace nor the platform admin.
var errTeamSpaceForbidden = status.Error(codes.PermissionDenied, "TeamSpace access denied")

// validateName admits a TeamSpace name of 1 to MaxNameBytes bytes.
func validateName(name string) error {
	return boundary.InvalidIf(name == "" || len(name) > teamspaces.MaxNameBytes, fmt.Sprintf("TeamSpace name must contain 1 to %d bytes", teamspaces.MaxNameBytes))
}
