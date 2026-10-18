// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package teamspaces

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/superversev2/internal/comm/dexflow"
)

// The values below are persisted in TeamSpaceFlow Attributes, its Channel and
// its RPC payloads. Their field names are their JSON encoding, which the read
// model's columns follow, so they carry no tags and are never renamed.

// TeamSpace is the TeamSpaceFlow snapshot its RPCs and its completion return.
type TeamSpace struct {
	Identity                   dexflow.StartIdentity
	Name, CreatorAccountID     string
	Status                     Status
	ActiveCommand, LastCommand *Command
	LastFailure                *dexflow.Failure
	Revision                   uint64
	CreatedAt, UpdatedAt       time.Time
}

// Status is a value of the TeamSpaceStatus Attribute, which the read model's
// team_space_status column stores, so it is an alias of string; its constants
// declare the set, and the API maps each to the enum value of the same name.
type Status = string

const (
	StatusReady    Status = "READY"
	StatusDeleting Status = "DELETING"
	StatusDeleted  Status = "DELETED"
)

// Role is a member's role in a TeamSpace, which the team_space_members role
// column stores; its constants declare the set.
type Role = string

const (
	RoleAdmin  Role = "ADMIN"
	RoleMember Role = "MEMBER"
)

// Member is one instance of the Members map, keyed by the member's AccountID;
// Dex writes it as a team_space_members row.
type Member struct {
	Role             Role
	AddedAt          time.Time
	AddedByAccountID string
}

// MemberEntry is a member with its AccountID, in the member lists of RPC
// results.
type MemberEntry struct {
	AccountID string
	Member
}

// RegisteredProject is one instance of the Projects map, keyed by ProjectID:
// a Project that the TeamSpace's deletion deletes.
type RegisteredProject struct {
	ProjectFlowID         string
	RegisteredAt          time.Time
	RegisteredByAccountID string
}

// Command is an admitted deletion of the TeamSpace. The same value is the
// Channel message, the ActiveCommand and the LastCommand record. ID is the
// requester's request ID and its identity.
type Command struct {
	ID, Kind, ActorAccountID string
	// Blocker names, for RecordTeamSpaceFailure, the Project that keeps the
	// deletion from completing.
	Blocker string `json:",omitempty"`
}

// commandDeleteTeamSpace is the one kind of Command. The value is persisted.
const commandDeleteTeamSpace = "DELETE_TEAM_SPACE"

// The limits of a TeamSpace.
const (
	// maxMembers bounds the Members map, which member writes load whole.
	maxMembers = 500
	// MaxNameBytes bounds a TeamSpace's name.
	MaxNameBytes = 80
	// deletionBudget is the wait limit of deleting a TeamSpace's Projects; the
	// engine enforces it through PollingStepOptions.
	deletionBudget = 6 * time.Hour
	// deletionPollInterval spaces the rounds of the deletion.
	deletionPollInterval = 5 * time.Second
	// deletionRoundTimeout bounds one round, which reads and asks up to
	// deletionBatch Projects.
	deletionRoundTimeout = 40 * time.Second
	// deletionBatch is how many Projects one round handles.
	deletionBatch = 8
)

// Start is the accepted creation that starts a TeamSpaceFlow; the creator is
// its first admin.
type Start struct {
	Identity               dexflow.StartIdentity
	Name, CreatorAccountID string
}

// Actor is the account a write acts for. PlatformAdmin is the API's
// authenticated finding that the account is the platform admin, who acts in
// every TeamSpace.
type Actor struct {
	AccountID     string
	PlatformAdmin bool
}

// MemberChange adds a member, sets a member's role or removes a member.
type MemberChange struct {
	Actor     Actor
	AccountID string
	Role      Role
}

// Renaming renames the TeamSpace at the revision the caller observed.
type Renaming struct {
	Actor            Actor
	ExpectedRevision uint64
	Name             string
}

// DeletionRequest asks for the TeamSpace's deletion under the request's ID.
type DeletionRequest struct {
	Actor Actor
	ID    string
}

// registration registers a Project of the actor, its creator, or removes it
// from the TeamSpace once the Project is deleted.
type registration struct {
	Actor     Actor
	ProjectID string
}

// Update answers a TeamSpace RPC with the snapshot after it and,
// for member writes, the members; or with the business rejection that left
// the TeamSpace unchanged.
type Update struct {
	TeamSpace TeamSpace
	Members   []MemberEntry
	Rejection *dexflow.Rejection
}

// registrationResult confirms a Project's registration or an attachment's
// staging reservation without the snapshot.
type registrationResult struct{ Rejection *dexflow.Rejection }

// StagingLimits bound the attachments a TeamSpace stages at once: their
// number and their declared bytes together (attachments.teamSpaceStagedFiles
// and attachments.teamSpaceStagedBytes).
type StagingLimits struct {
	Files int
	Bytes int64
}

// StagingTotals counts the attachments the AttachmentStaging map holds.
type StagingTotals struct {
	Count int
	Bytes int64
}

// StagedAttachment is one instance of the AttachmentStaging map, keyed by
// the attachment ID: the AttachmentFlow that holds the slot, its declared
// bytes, its uploader and when its hold lapses, the latest moment that Flow
// releases it on its own.
type StagedAttachment struct {
	FlowID            string
	Bytes             int64
	UploaderAccountID string
	ReservedAt        time.Time
	HeldUntil         time.Time `json:",omitempty"`
}

// StagingReservation reserves a staging slot for the attachment of an
// AttachmentFlow, uploaded by Actor, until HeldUntil at the latest.
type StagingReservation struct {
	AttachmentID, FlowID string
	Bytes                int64
	Actor                Actor
	HeldUntil            time.Time
}

// maxLapsedStaging bounds the lapsed slots one read lists.
const maxLapsedStaging = 10

// lapsedStaging lists the attachments whose staging hold lapsed, oldest
// first.
type lapsedStaging struct{ AttachmentIDs []string }

// lapsedOf lists the staged attachments whose hold lapsed before now, oldest
// first and at most maxLapsedStaging. A slot reserved without a hold never
// lapses.
func lapsedOf(staged map[string]StagedAttachment, now time.Time) lapsedStaging {
	ids := make([]string, 0, len(staged))
	for id, attachment := range staged {
		if !attachment.HeldUntil.IsZero() && attachment.HeldUntil.Before(now) {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Or(staged[a].HeldUntil.Compare(staged[b].HeldUntil), strings.Compare(a, b))
	})
	return lapsedStaging{AttachmentIDs: ids[:min(len(ids), maxLapsedStaging)]}
}

// stagingRelease frees the staging slot of an attachment.
type stagingRelease struct{ AttachmentID string }

// attachmentStagingPrefix is the object prefix of the TeamSpace's staged
// attachments, which every AttachmentFlow of the TeamSpace writes below.
func attachmentStagingPrefix(teamSpaceID string) string {
	return "attachment-staging/" + teamSpaceID + "/"
}

// memberEntries lists members in the order they were added, then by
// AccountID.
func memberEntries(members map[string]Member) []MemberEntry {
	entries := make([]MemberEntry, 0, len(members))
	for accountID, member := range members {
		entries = append(entries, MemberEntry{AccountID: accountID, Member: member})
	}
	slices.SortFunc(entries, func(a, b MemberEntry) int {
		if order := a.AddedAt.Compare(b.AddedAt); order != 0 {
			return order
		}
		return strings.Compare(a.AccountID, b.AccountID)
	})
	return entries
}

// admins counts the admins among members.
func admins(members map[string]Member) int {
	count := 0
	for _, member := range members {
		if member.Role == RoleAdmin {
			count++
		}
	}
	return count
}

// mayAdminister reports whether the actor may change the TeamSpace: the
// platform admin, or an admin of members.
func (a Actor) mayAdminister(members map[string]Member) bool {
	return a.PlatformAdmin || members[a.AccountID].Role == RoleAdmin
}

// runsCommand reports whether command is still the TeamSpace's active one; a
// command completed or failed meanwhile leaves its Steps idle.
func (t TeamSpace) runsCommand(command Command) bool {
	return t.ActiveCommand != nil && t.ActiveCommand.ID == command.ID
}
