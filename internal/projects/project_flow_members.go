// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/tenancy/teamspaces"
)

// Role is a member's role in a Project, which the project_members role column
// stores, so it is an alias of string; its constants declare the set. Each
// role holds everything the roles below it hold.
type Role = string

const (
	RoleOwner    Role = "OWNER"
	RoleBuilder  Role = "BUILDER"
	RoleOperator Role = "OPERATOR"
)

// rolesByRank orders the roles from the least: each holds what the roles
// before it hold.
var rolesByRank = []Role{RoleOperator, RoleBuilder, RoleOwner}

// Capability is something an account may do with a Project: it needs the
// least Project role, or, where teamSpaceAdmin allows it, a TeamSpace admin.
type Capability struct {
	name           string
	least          Role
	teamSpaceAdmin bool
}

// The capabilities that the API and the Services check.
var (
	// CapabilityView reads the Project, its members and its Releases.
	CapabilityView = Capability{name: "view the Project", least: RoleOperator}
	// CapabilityOperateLive reads and runs the Flows of Live.
	CapabilityOperateLive = Capability{name: "operate Live", least: RoleOperator}
	// CapabilityBuild works in the Session and Preview, builds, deploys and
	// configures, and changes Live engine state.
	CapabilityBuild = Capability{name: "build", least: RoleBuilder}
	// CapabilityManageMembers changes the Project's members.
	CapabilityManageMembers = Capability{name: "manage members", least: RoleOwner}
	// CapabilityDelete deletes the Project, also as an admin of its TeamSpace.
	CapabilityDelete = Capability{name: "delete the Project", least: RoleOwner, teamSpaceAdmin: true}
)

// access is what an account holds in a Project: whether it is the platform
// admin, its Project role and its role in the Project's TeamSpace, each empty
// when it holds none.
type access struct {
	platformAdmin              bool
	projectRole, teamSpaceRole string
}

// effectiveRole is the role the account acts with: OWNER for the platform
// admin, otherwise its Project role while it is a member of the TeamSpace,
// otherwise none.
func (a access) effectiveRole() Role {
	switch {
	case a.platformAdmin:
		return RoleOwner
	case a.teamSpaceRole == "":
		return ""
	}
	return a.projectRole
}

// allows reports whether the account holds the capability.
func (a access) allows(c Capability) bool {
	return slices.Index(rolesByRank, a.effectiveRole()) >= slices.Index(rolesByRank, c.least) ||
		c.teamSpaceAdmin && a.teamSpaceRole == teamspaces.RoleAdmin
}

// Allows applies the same capability policy to roles already joined with a
// currently authenticated account by another SQL authorization boundary.
// TeamSpace membership remains mandatory outside the active platform admin.
func (c Capability) Allows(platformAdmin bool, projectRole, teamSpaceRole string) bool {
	return (access{platformAdmin: platformAdmin, projectRole: projectRole, teamSpaceRole: teamSpaceRole}).allows(c)
}

// ProjectMember is one instance of ProjectFlow's Members map, keyed by the
// member's AccountID; Dex writes it as a project_members row. Its field names
// are the row's columns.
type ProjectMember struct {
	Role             Role
	AddedAt          time.Time
	AddedByAccountID string
}

// MemberEntry is a member with its AccountID, in the member lists of
// RPC results.
type MemberEntry struct {
	AccountID string
	ProjectMember
}

// MemberView is a Project member as the read model lists it, with its
// account's email and whether the account still belongs to the Project's
// TeamSpace, without which its role grants nothing.
type MemberView struct {
	AccountID, Email string
	Role             Role
	AddedAt          time.Time
	AddedByAccountID string
	TeamSpaceMember  bool
}

// maxProjectMembers bounds the Members map, which member writes load whole.
const maxProjectMembers = 200

// MemberChange adds a member, sets a member's role or removes a member,
// as the actor. ActorIsPlatformAdmin is the API's authenticated finding; the
// API also checked that the member belongs to the Project's TeamSpace.
type MemberChange struct {
	ActorAccountID       string
	ActorIsPlatformAdmin bool
	AccountID            string
	Role                 Role
}

// MemberUpdate answers a member write with the Project and its
// members after it, or with the rejection that left them unchanged.
type MemberUpdate struct {
	Project   Project
	Members   []MemberEntry
	Rejection *dexflow.Rejection
}

// memberOutcome is the outcome of a member write on the loaded members: the
// members after it, whether it changed them, or its rejection.
type memberOutcome struct {
	members   map[string]ProjectMember
	changed   bool
	rejection *dexflow.Rejection
}

// admitMemberChange applies the rules every member write shares: a Project
// that is not deleting, and an actor who is an owner or the platform admin.
func admitMemberChange(p Project, members map[string]ProjectMember, in MemberChange) *dexflow.Rejection {
	if !p.AdmitsChildren() {
		return dexflow.Reject(dexflow.RejectionFailedPrecondition, "the Project is being deleted")
	}
	if !in.ActorIsPlatformAdmin && members[in.ActorAccountID].Role != RoleOwner {
		return dexflow.Reject(dexflow.RejectionPermissionDenied, "only a Project owner changes its members")
	}
	return nil
}

// addMember adds a member; adding a member who has that role changes nothing.
func addMember(members map[string]ProjectMember, in MemberChange, now time.Time) memberOutcome {
	if existing, found := members[in.AccountID]; found {
		if existing.Role != in.Role {
			return memberOutcome{rejection: dexflow.Reject(dexflow.RejectionAlreadyExists, "the account is already a member with another role")}
		}
		return memberOutcome{members: members}
	}
	if len(members) >= maxProjectMembers {
		return memberOutcome{rejection: dexflow.Reject(dexflow.RejectionResourceExhausted, fmt.Sprintf("a Project holds at most %d members", maxProjectMembers))}
	}
	members[in.AccountID] = ProjectMember{Role: in.Role, AddedAt: now, AddedByAccountID: in.ActorAccountID}
	return memberOutcome{members: members, changed: true}
}

// setMemberRole changes a member's role, keeping at least one owner.
func setMemberRole(members map[string]ProjectMember, in MemberChange) memberOutcome {
	member, found := members[in.AccountID]
	switch {
	case !found:
		return memberOutcome{rejection: dexflow.Reject(dexflow.RejectionNotFound, "the account is not a member of the Project")}
	case member.Role == in.Role:
		return memberOutcome{members: members}
	case member.Role == RoleOwner && owners(members) == 1:
		return memberOutcome{rejection: dexflow.Reject(dexflow.RejectionFailedPrecondition, "a Project keeps at least one owner")}
	}
	member.Role = in.Role
	members[in.AccountID] = member
	return memberOutcome{members: members, changed: true}
}

// removeMember removes a member, keeping at least one owner; removing an
// absent member changes nothing.
func removeMember(members map[string]ProjectMember, in MemberChange) memberOutcome {
	member, found := members[in.AccountID]
	switch {
	case !found:
		return memberOutcome{members: members}
	case member.Role == RoleOwner && owners(members) == 1:
		return memberOutcome{rejection: dexflow.Reject(dexflow.RejectionFailedPrecondition, "a Project keeps at least one owner")}
	}
	delete(members, in.AccountID)
	return memberOutcome{members: members, changed: true}
}

// owners counts the owners among members.
func owners(members map[string]ProjectMember) int {
	count := 0
	for _, member := range members {
		if member.Role == RoleOwner {
			count++
		}
	}
	return count
}

// memberEntries lists members in the order they were added, then by
// AccountID.
func memberEntries(members map[string]ProjectMember) []MemberEntry {
	entries := make([]MemberEntry, 0, len(members))
	for accountID, member := range members {
		entries = append(entries, MemberEntry{AccountID: accountID, ProjectMember: member})
	}
	slices.SortFunc(entries, func(a, b MemberEntry) int {
		if order := a.AddedAt.Compare(b.AddedAt); order != 0 {
			return order
		}
		return strings.Compare(a.AccountID, b.AccountID)
	})
	return entries
}
