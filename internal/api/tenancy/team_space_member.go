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
	"errors"
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/api/boundary"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/tenancy/teamspaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// soleOwnedShown bounds the Projects that a refused removal names.
const soleOwnedShown = 5

// teamSpaceMemberServer serves TeamSpaceMemberService. TeamSpaceFlow admits a
// change only from an admin or the platform admin and keeps one admin; the
// read model lists the members and guards a removal.
type teamSpaceMemberServer struct {
	pb.UnimplementedTeamSpaceMemberServiceServer
	*boundary.Boundary
	teamSpaces *teamspaces.TeamSpaceFlowClient
	members    *memberdao.MemberDAO
}

// ListTeamSpaceMembers lists the members of a TeamSpace for one of them or the
// platform admin.
func (s *teamSpaceMemberServer) ListTeamSpaceMembers(ctx context.Context, in *pb.ListTeamSpaceMembersRequest) (*pb.ListTeamSpaceMembersResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = flowid.ValidateTeamSpaceID(in.GetTeamSpaceId()); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	role, err := teamspaces.AwaitRow(ctx, s.teamSpaces, in.GetTeamSpaceId(), func(ctx context.Context) (string, error) {
		return s.members.TeamSpaceRole(ctx, in.GetTeamSpaceId(), account.AccountID, teamspaces.StatusDeleted)
	})
	switch {
	case errors.Is(err, postgres.ErrNotFound):
		return nil, status.Error(codes.NotFound, "TeamSpace not found")
	case err != nil:
		return nil, boundary.RPCStatus(err)
	case role == "" && !accounts.IsAdministrator(account):
		return nil, errTeamSpaceForbidden
	}
	rows, err := s.members.TeamSpaceMembers(ctx, in.GetTeamSpaceId())
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.ListTeamSpaceMembersResponse{}
	for _, row := range rows {
		out.Members = append(out.Members, memberSnapshot(row.AccountID, row.Email.V, teamspaces.Role(row.Role.V), row.AddedAt.V, row.AddedByAccountID.V))
	}
	return out, nil
}

// AddTeamSpaceMember adds the registered account of an email. It also counts
// the TeamSpace's Projects in which the account still holds a role, which
// count again now.
func (s *teamSpaceMemberServer) AddTeamSpaceMember(ctx context.Context, in *pb.AddTeamSpaceMemberRequest) (*pb.AddTeamSpaceMemberResponse, error) {
	change, err := s.memberChange(ctx, in.GetCaller(), in.GetTeamSpaceId(), &in.Role)
	if err != nil {
		return nil, err
	}
	change.AccountID, err = s.Accounts.AccountIDByEmail(ctx, in.GetEmail())
	if errors.Is(err, accounts.ErrUnknownEmail) {
		return nil, status.Error(codes.FailedPrecondition, accounts.ErrUnknownEmail.Error())
	}
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	update, err := s.teamSpaces.AddMember(ctx, in.GetTeamSpaceId(), change)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	regained, err := s.members.RetainedProjectRoles(ctx, in.GetTeamSpaceId(), change.AccountID, projects.StatusDeleted)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	members, err := s.memberSnapshots(ctx, in.GetTeamSpaceId(), update.Members)
	if err != nil {
		return nil, err
	}
	return &pb.AddTeamSpaceMemberResponse{Members: members, Revision: update.TeamSpace.Revision, RegainedProjectCount: regained}, nil
}

func (s *teamSpaceMemberServer) SetTeamSpaceMemberRole(ctx context.Context, in *pb.SetTeamSpaceMemberRoleRequest) (*pb.SetTeamSpaceMemberRoleResponse, error) {
	change, err := s.memberChange(ctx, in.GetCaller(), in.GetTeamSpaceId(), &in.Role)
	if err != nil {
		return nil, err
	}
	if change.AccountID = in.GetAccountId(); change.AccountID == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	update, err := s.teamSpaces.SetMemberRole(ctx, in.GetTeamSpaceId(), change)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	members, err := s.memberSnapshots(ctx, in.GetTeamSpaceId(), update.Members)
	return &pb.SetTeamSpaceMemberRoleResponse{Members: members, Revision: update.TeamSpace.Revision}, err
}

// RemoveTeamSpaceMember removes a member unless the member is the only owner
// of one of the TeamSpace's Projects, as the read model shows, whose
// ownership the caller must transfer first.
func (s *teamSpaceMemberServer) RemoveTeamSpaceMember(ctx context.Context, in *pb.RemoveTeamSpaceMemberRequest) (*pb.RemoveTeamSpaceMemberResponse, error) {
	change, err := s.memberChange(ctx, in.GetCaller(), in.GetTeamSpaceId(), nil)
	if err != nil {
		return nil, err
	}
	if change.AccountID = in.GetAccountId(); change.AccountID == "" {
		return nil, status.Error(codes.InvalidArgument, "account_id is required")
	}
	owned, err := s.members.SoleOwnedProjects(ctx, in.GetTeamSpaceId(), change.AccountID, projects.RoleOwner, projects.StatusDeleted, soleOwnedShown)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	if len(owned) != 0 {
		names := make([]string, 0, len(owned))
		for _, project := range owned {
			names = append(names, project.Name.V)
		}
		return nil, status.Error(codes.FailedPrecondition, "the member is the only owner of "+strings.Join(names, ", ")+"; transfer the ownership first")
	}
	update, err := s.teamSpaces.RemoveMember(ctx, in.GetTeamSpaceId(), change)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	members, err := s.memberSnapshots(ctx, in.GetTeamSpaceId(), update.Members)
	return &pb.RemoveTeamSpaceMemberResponse{Members: members, Revision: update.TeamSpace.Revision}, err
}

// memberChange authenticates the caller of a member write and validates its
// TeamSpace and role; role is nil for a removal. TeamSpaceFlow decides
// whether the caller may change the members.
func (s *teamSpaceMemberServer) memberChange(ctx context.Context, caller *pb.CallerContext, teamSpaceID string, role *pb.TeamSpaceRole) (teamspaces.MemberChange, error) {
	account, err := s.Authenticate(ctx, caller)
	if err != nil {
		return teamspaces.MemberChange{}, err
	}
	if err = flowid.ValidateTeamSpaceID(teamSpaceID); err != nil {
		return teamspaces.MemberChange{}, boundary.RPCStatus(err)
	}
	change := teamspaces.MemberChange{Actor: teamspaces.Actor{AccountID: account.AccountID, PlatformAdmin: accounts.IsAdministrator(account)}}
	if role != nil {
		if change.Role, err = boundary.DomainValue(*role); err != nil {
			return teamspaces.MemberChange{}, err
		}
	}
	return change, nil
}

// memberSnapshots shows TeamSpaceFlow's members with the emails of their
// accounts.
func (s *teamSpaceMemberServer) memberSnapshots(ctx context.Context, teamSpaceID string, entries []teamspaces.MemberEntry) ([]*pb.TeamSpaceMemberSnapshot, error) {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.AccountID)
	}
	found, err := s.members.MemberAccounts(ctx, teamSpaceID, ids)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	snapshots := make([]*pb.TeamSpaceMemberSnapshot, 0, len(entries))
	for _, entry := range entries {
		snapshots = append(snapshots, memberSnapshot(entry.AccountID, found[entry.AccountID].Email, entry.Role, entry.AddedAt, entry.AddedByAccountID))
	}
	return snapshots, nil
}
