// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"errors"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/api/boundary"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/projects"
	"github.com/superdurable/superversev2/internal/tenancy/accounts"
	"github.com/superdurable/superversev2/internal/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/tenancy/teamspaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// projectMemberServer serves ProjectMemberService: a Project's members and
// their roles. ProjectFlow owns them and admits a change only from an owner or
// the platform admin; the read model lists them. A list needs
// CapabilityView, a change CapabilityManageMembers, which a role holds only
// while its account is a member of the Project's TeamSpace; a member added
// must be one too.
type projectMemberServer struct {
	pb.UnimplementedProjectMemberServiceServer
	*boundary.Boundary
	projects *projects.ProjectFlowClient
	members  *memberdao.MemberDAO
}

// errNotTeamSpaceMember refuses a member who does not belong to the Project's
// TeamSpace, where a Project role grants nothing.
var errNotTeamSpaceMember = status.Error(codes.FailedPrecondition, "the account is not a member of the Project's TeamSpace")

// ListProjectMembers lists the members of a Project the caller may view.
func (s *projectMemberServer) ListProjectMembers(ctx context.Context, in *pb.ListProjectMembersRequest) (*pb.ListProjectMembersResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if _, err = s.projects.Authorize(ctx, account.AccountID, in.GetProjectId(), projects.CapabilityView); err != nil {
		return nil, boundary.RPCStatus(err)
	}
	members, err := s.projects.ListMembers(ctx, in.GetProjectId())
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	out := &pb.ListProjectMembersResponse{}
	for _, member := range members {
		out.Members = append(out.Members, &pb.ProjectMemberSnapshot{AccountId: member.AccountID, Email: member.Email, Role: boundary.Enum[pb.ProjectRole](member.Role),
			AddedAt: boundary.OptionalTimestamp(member.AddedAt), AddedByAccountId: member.AddedByAccountID, IsTeamSpaceMember: member.TeamSpaceMember})
	}
	return out, nil
}

func (s *projectMemberServer) AddProjectMember(ctx context.Context, in *pb.AddProjectMemberRequest) (*pb.AddProjectMemberResponse, error) {
	members, revision, err := s.change(ctx, in.GetCaller(), in.GetProjectId(), in.GetAccountId(), &in.Role, true, s.projects.AddMember)
	return &pb.AddProjectMemberResponse{Members: members, Revision: revision}, err
}

func (s *projectMemberServer) SetProjectMemberRole(ctx context.Context, in *pb.SetProjectMemberRoleRequest) (*pb.SetProjectMemberRoleResponse, error) {
	members, revision, err := s.change(ctx, in.GetCaller(), in.GetProjectId(), in.GetAccountId(), &in.Role, false, s.projects.SetMemberRole)
	return &pb.SetProjectMemberRoleResponse{Members: members, Revision: revision}, err
}

func (s *projectMemberServer) RemoveProjectMember(ctx context.Context, in *pb.RemoveProjectMemberRequest) (*pb.RemoveProjectMemberResponse, error) {
	members, revision, err := s.change(ctx, in.GetCaller(), in.GetProjectId(), in.GetAccountId(), nil, false, s.projects.RemoveMember)
	return &pb.RemoveProjectMemberResponse{Members: members, Revision: revision}, err
}

// change runs one member write of ProjectFlow for a caller who may manage the
// members, and answers with the members and the Project's revision after it.
// role is nil for a removal; a member the change adds must belong to the
// TeamSpace.
func (s *projectMemberServer) change(ctx context.Context, caller *pb.CallerContext, projectID, accountID string, role *pb.ProjectRole, adds bool,
	write func(context.Context, string, projects.MemberChange) (projects.MemberUpdate, error)) ([]*pb.ProjectMemberSnapshot, uint64, error) {
	account, err := s.Authenticate(ctx, caller)
	if err != nil {
		return nil, 0, err
	}
	if err = boundary.InvalidIf(accountID == "", "account_id is required"); err != nil {
		return nil, 0, err
	}
	change := projects.MemberChange{ActorAccountID: account.AccountID, ActorIsPlatformAdmin: accounts.IsAdministrator(account), AccountID: accountID}
	if role != nil {
		if change.Role, err = boundary.DomainValue(*role); err != nil {
			return nil, 0, err
		}
	}
	authorized, err := s.projects.Authorize(ctx, account.AccountID, projectID, projects.CapabilityManageMembers)
	if err != nil {
		return nil, 0, boundary.RPCStatus(err)
	}
	if adds {
		if err = s.requireTeamSpaceMember(ctx, authorized.Project.TeamSpaceID, accountID, errNotTeamSpaceMember); err != nil {
			return nil, 0, err
		}
	}
	update, err := write(ctx, projectID, change)
	if err != nil {
		return nil, 0, boundary.RPCStatus(err)
	}
	members, err := s.memberSnapshots(ctx, authorized.Project.TeamSpaceID, update.Members)
	return members, update.Project.Revision, err
}

// requireTeamSpaceMember answers refusal unless the account is a member of the
// TeamSpace, which the read model shows.
func (s *projectMemberServer) requireTeamSpaceMember(ctx context.Context, teamSpaceID, accountID string, refusal error) error {
	role, err := s.members.TeamSpaceRole(ctx, teamSpaceID, accountID, teamspaces.StatusDeleted)
	if errors.Is(err, postgres.ErrNotFound) || err == nil && role == "" {
		return refusal
	}
	return boundary.RPCStatus(err)
}

// memberSnapshots shows ProjectFlow's members with their emails and whether
// each still belongs to the TeamSpace.
func (s *projectMemberServer) memberSnapshots(ctx context.Context, teamSpaceID string, entries []projects.MemberEntry) ([]*pb.ProjectMemberSnapshot, error) {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.AccountID)
	}
	found, err := s.members.MemberAccounts(ctx, teamSpaceID, ids)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	snapshots := make([]*pb.ProjectMemberSnapshot, 0, len(entries))
	for _, entry := range entries {
		snapshots = append(snapshots, &pb.ProjectMemberSnapshot{AccountId: entry.AccountID, Email: found[entry.AccountID].Email, Role: boundary.Enum[pb.ProjectRole](entry.Role),
			AddedAt: timestamppb.New(entry.AddedAt), AddedByAccountId: entry.AddedByAccountID, IsTeamSpaceMember: found[entry.AccountID].TeamSpaceMember})
	}
	return snapshots, nil
}
