// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package tenancy

import (
	"encoding/json"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/api/boundary"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/tenancy/connectiondao"
	"github.com/superdurable/superversev2/internal/tenancy/connections"
	"github.com/superdurable/superversev2/internal/tenancy/memberdao"
	"github.com/superdurable/superversev2/internal/tenancy/slackrelay"
	"github.com/superdurable/superversev2/internal/tenancy/teamspacedao"
	"github.com/superdurable/superversev2/internal/tenancy/teamspaces"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Connections is what TeamSpaceConnectionService uses: the connections the
// database owns, the read model's access queries and the Studio origin that
// OAuth providers return to.
type Connections struct {
	Connections  *connections.TeamSpaceConnections
	Access       *connectiondao.ConnectionDAO
	StudioOrigin string
	Unrouted     UnroutedCounts
}

// UnroutedCounts is the Slack event relay as the connection API reads it: how
// many events of a connection's installation found no route.
type UnroutedCounts interface {
	UnroutedEvents(teamSpaceID, connectionID string) slackrelay.UnroutedEvents
}

// Register serves AccountService, TeamSpaceService, TeamSpaceMemberService and
// TeamSpaceConnectionService on server. TeamSpaceFlow owns TeamSpaces and
// their members; the read model serves their reads, and members, the member
// tables. The database owns connections.
func Register(server *grpc.Server, b *boundary.Boundary, teamSpaces *teamspaces.TeamSpaceFlowClient, reads *teamspacedao.TeamSpaceDAO, members *memberdao.MemberDAO, c Connections) {
	pb.RegisterAccountServiceServer(server, &accountServer{Boundary: b})
	pb.RegisterTeamSpaceServiceServer(server, &teamSpaceServer{Boundary: b, teamSpaces: teamSpaces, reads: reads})
	pb.RegisterTeamSpaceMemberServiceServer(server, &teamSpaceMemberServer{Boundary: b, teamSpaces: teamSpaces, members: members})
	pb.RegisterTeamSpaceConnectionServiceServer(server, &teamSpaceConnectionServer{Boundary: b, connections: c.Connections, access: c.Access, studioOrigin: c.StudioOrigin, unrouted: c.Unrouted})
}

// teamSpaceRowSnapshot shows a TeamSpace that the read model lists, with the
// caller's role in it.
func teamSpaceRowSnapshot(row teamspacedao.TeamSpaceRow) *pb.TeamSpaceSnapshot {
	var failure *dexflow.Failure
	if len(row.LastFailure) != 0 {
		// The column holds what Dex wrote from the Attribute, which is always valid JSON of its type.
		if err := json.Unmarshal(row.LastFailure, &failure); err != nil {
			panic("the last_failure column of TeamSpace " + row.TeamSpaceID + " is not a Failure: " + err.Error())
		}
	}
	return &pb.TeamSpaceSnapshot{
		TeamSpaceId: row.TeamSpaceID, Name: row.Name.V, Status: boundary.Enum[pb.TeamSpaceStatus](teamspaces.Status(row.TeamSpaceStatus.V)),
		Revision: uint64(row.Revision.V), CreatorAccountId: row.CreatorAccountID.V, CallerRole: boundary.Enum[pb.TeamSpaceRole](teamspaces.Role(row.CallerRole.V)),
		Failure: boundary.FailureSnapshot(failure), CreatedAt: boundary.OptionalTimestamp(row.CreatedAt.V), UpdatedAt: boundary.OptionalTimestamp(row.UpdatedAt.V),
	}
}

// teamSpaceSnapshot shows the TeamSpace that a TeamSpaceFlow write returned,
// with the caller's role, which the caller of the write reads elsewhere.
func teamSpaceSnapshot(teamSpaceID string, t teamspaces.TeamSpace, callerRole teamspaces.Role) *pb.TeamSpaceSnapshot {
	return &pb.TeamSpaceSnapshot{
		TeamSpaceId: teamSpaceID, Name: t.Name, Status: boundary.Enum[pb.TeamSpaceStatus](t.Status), Revision: t.Revision,
		CreatorAccountId: t.CreatorAccountID, CallerRole: boundary.Enum[pb.TeamSpaceRole](callerRole), Failure: boundary.FailureSnapshot(t.LastFailure),
		CreatedAt: timestamppb.New(t.CreatedAt), UpdatedAt: timestamppb.New(t.UpdatedAt),
	}
}

// memberSnapshot shows a TeamSpace member.
func memberSnapshot(accountID, email string, role teamspaces.Role, addedAt time.Time, addedBy string) *pb.TeamSpaceMemberSnapshot {
	return &pb.TeamSpaceMemberSnapshot{AccountId: accountID, Email: email, Role: boundary.Enum[pb.TeamSpaceRole](role),
		AddedAt: boundary.OptionalTimestamp(addedAt), AddedByAccountId: addedBy}
}
