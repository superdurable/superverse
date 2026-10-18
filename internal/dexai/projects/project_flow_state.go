// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects

import (
	"encoding/json"
	"slices"
	"time"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/comm/platform/objectstore"
	"github.com/superdurable/superversev2/internal/dexai/projects/infra"
)

// The values below are persisted in ProjectFlow Attributes, its Channel and
// its RPC payloads. Their field names are their JSON encoding.

// Project is the complete ProjectFlow snapshot.
type Project struct {
	Identity                                            dexflow.StartIdentity
	ProjectID, CreatorAccountID, Name, TemplateRevision string
	SetupFlowID, TeamSpaceID                            string
	ProjectStatus                                       Status
	RepositoryBinding                                   RepositoryBindingRef
	AppManifest                                         *pb.AppManifestSnapshot
	ActiveDeployment                                    *DeploymentLease
	CurrentLive                                         *LivePointer
	Storage                                             infra.StorageRefs
	LiveExecution                                       infra.ExecutionRefs
	ActiveCommand, LastCommand                          *ProjectCommand
	LastFailure                                         *dexflow.Failure
	// SessionStartFailure is SESSION_START_FAILED while the READY Project has
	// no Session; LastFailure stays the command's.
	SessionStartFailure *dexflow.Failure
	// AttachmentTotals count the attachments the Project's messages sent.
	AttachmentTotals     AttachmentTotals
	Revision             uint64
	CreatedAt, UpdatedAt time.Time
}

// Status is a value of the ProjectStatus Attribute, which keyword3 indexes, so
// it is an alias of string; its constants declare the set. They are persisted,
// and the API maps each to the ProjectStatus enum value of the same name.
type Status = string

const (
	StatusCreating    Status = "CREATING"
	StatusReady       Status = "READY"
	statusSetupFailed Status = "SETUP_FAILED"
	StatusDeleting    Status = "DELETING"
	StatusDeleted     Status = "DELETED"
)

// SetupID is the ID of the Project's Setup within the Project. The API
// derives SetupFlowID at creation, so a failure means corrupted state.
func (p Project) SetupID() string {
	return registeredChildID(p.ProjectID, ChildFlowReference{FlowID: p.SetupFlowID, FlowType: ChildSetup.FlowType()})
}

// AdmitsChildren reports whether the Project still accepts new children and
// commands: neither deleting nor deleted.
func (p Project) AdmitsChildren() bool {
	return p.ProjectStatus != StatusDeleting && p.ProjectStatus != StatusDeleted
}

// RepositoryBindingRef is the repository a Project works in on its repository host;
// URL is where the Sandbox clones it from.
type RepositoryBindingRef struct {
	RepositoryID, InstallationID int64
	FullName, URL, DefaultBranch string
	Generation                   uint64
}

// ProjectCommand is an admitted Live removal or Project deletion. The same
// value is the Channel message, the ActiveCommand and the LastCommand record;
// Phase is the phase it runs, QUEUED, FAILED or COMPLETE.
type ProjectCommand struct {
	ID, Kind, Phase  string
	ExpectedRevision uint64
}

// The kinds of ProjectCommand. The values are persisted.
const (
	CommandRemoveLive    = "REMOVE_LIVE"
	CommandDeleteProject = "DELETE_PROJECT"
)

// ChildFlowReference is one ChildFlows entry: a Flow the Project must clean up
// before its deletion completes.
type ChildFlowReference struct {
	FlowID, FlowType string
}

// ChildFlow is a type of Flow a Project owns as a child, declared once with
// the kind its FlowID names. Its type is the Go type name that Dex gives the
// Flow, which this package cannot import; the server's registry check
// compares the two. The Session has no kind: the Project has one, whose
// FlowID is flowid.SessionFlowID.
type ChildFlow struct{ flowType, kind string }

var (
	ChildSetup   = ChildFlow{flowType: "ProjectSetupFlow", kind: "setup"}
	ChildSession = ChildFlow{flowType: "SessionAgentFlow"}
	ChildRelease = ChildFlow{flowType: "ProjectReleaseFlow", kind: "release"}
	ChildDeploy  = ChildFlow{flowType: "ProjectDeployFlow", kind: "deploy"}
)

// FlowType is the Dex Flow type of the child, which ChildFlowReference keeps.
func (child ChildFlow) FlowType() string { return child.flowType }

// belongs reports whether flowID is a FlowID of this child type of the
// Project: its one Session's, or <kind>-<projectID>-<childID>.
func (child ChildFlow) belongs(projectID, flowID string) bool {
	if child == ChildSession {
		return flowID == flowid.SessionFlowID(projectID)
	}
	_, ok := flowid.ParseProjectChild(child.kind, projectID, flowID)
	return ok
}

// childFlows are the child Flows a Project admits, by their Flow type.
var childFlows = map[string]ChildFlow{
	ChildSetup.flowType: ChildSetup, ChildSession.flowType: ChildSession, ChildRelease.flowType: ChildRelease, ChildDeploy.flowType: ChildDeploy,
}

// DeploymentLease is the single admitted Live deployment of a Project.
type DeploymentLease struct {
	DeploymentID                     string
	Generation, ExpectedLiveRevision uint64
}

// LivePointer is the deployment that serves the Project's Live route.
type LivePointer struct {
	BuildRevisionID                                         string
	DeploymentID, ReleaseID, URL, ImageDigest, BundleDigest string
	Revision                                                uint64
	Ready                                                   bool
	Manifest                                                objectstore.ObjectRef
	FDGActivation                                           objectstore.ObjectRef
}

// ProjectStart is the accepted creation that starts a ProjectFlow in its
// TeamSpace. CreatorIsPlatformAdmin is the API's authenticated finding, which
// lets the platform admin create Projects in a TeamSpace it is no member of.
type ProjectStart struct {
	Identity                                                                      dexflow.StartIdentity
	ProjectID, CreatorAccountID, Name, TemplateRevision, SetupFlowID, TeamSpaceID string
	CreatorIsPlatformAdmin                                                        bool
	// InitialSession is what the creator chose for the Session's first turn,
	// or nil.
	InitialSession *SessionStartRequest
}

// SessionStartRequest is what the Project's creator chose for the Session's
// first turn, which the API admitted: a model, or empty for the server's
// default, and a first message with its ID and author, absent without one. The
// message is bounded by the message limit.
type SessionStartRequest struct {
	Model     string `json:"model,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	Text      string `json:"text,omitempty"`
	// AuthorAccountID and AuthorEmail name the creator, the message's author.
	AuthorAccountID string `json:"author_account_id,omitempty"`
	AuthorEmail     string `json:"author_email,omitempty"`
	// MatchedExample is what the server matched the first message on when it
	// started the Project from the closest example, which TemplateRevision
	// names; nil otherwise.
	MatchedExample *MatchedExample `json:"matched_example,omitempty"`
	// Attachments are the first message's attachments, which the API
	// admitted, in the order the creator attached them; the message may then
	// have no text. ProjectFlow counts them when it starts.
	Attachments []SessionAttachment `json:"attachments,omitempty"`
}

// SessionAttachment is one attachment of the first message: the file the
// Project's lifetime quota counts, and the Session's reference to it, which
// the Session's conversation package encodes (conversation.AttachmentRef)
// and the Project only carries to StartForProject.
type SessionAttachment struct {
	File      AttachedFile    `json:"file"`
	Reference json.RawMessage `json:"reference"`
}

// AttachedFile is one attachment a message of the Project carries, as its
// AttachmentFlow prepared it: its ID, its kind, media type and name, the
// SHA-256 of the upload and the bytes of every object the Project's copy of
// it holds.
type AttachedFile struct {
	ID, Kind, MediaType, FileName, SHA256 string
	Bytes                                 int64
}

// ProjectAttachment is one instance of the Attachments map, keyed by the
// attachment ID: the file, the message that first carried it and the
// admissions that hold it. Holds are the message admissions that reserved it
// and that no definite refusal released; Kept marks an attachment that nothing
// releases any more: a Project's first message carries it, or more than
// maxAttachmentHolds admissions held it. An instance with neither is deleted,
// otherwise it stays for the Project's lifetime, as its copy does.
type ProjectAttachment struct {
	Kind, MediaType, FileName, SHA256 string
	Bytes                             int64
	FirstMessageID                    string
	ReservedAt                        time.Time
	Holds                             []string `json:",omitempty"`
	Kept                              bool     `json:",omitempty"`
}

// maxAttachmentHolds bounds the admissions an instance of the Attachments map
// lists; one more keeps the attachment counted for the Project's lifetime,
// which only ever counts too much.
const maxAttachmentHolds = 8

// held is the instance once the admission admissionID holds it too, and
// whether that changed it.
func (a ProjectAttachment) held(admissionID string) (ProjectAttachment, bool) {
	switch {
	case a.Kept, slices.Contains(a.Holds, admissionID):
		return a, false
	case len(a.Holds) == maxAttachmentHolds:
		a.Holds, a.Kept = nil, true
	default:
		a.Holds = append(slices.Clone(a.Holds), admissionID)
	}
	return a, true
}

// released is the instance once the admission admissionID no longer holds it,
// whether that changed it, and whether nothing holds it any more.
func (a ProjectAttachment) released(admissionID string) (ProjectAttachment, bool, bool) {
	index := slices.Index(a.Holds, admissionID)
	if index < 0 {
		return a, false, false
	}
	a.Holds = slices.Delete(slices.Clone(a.Holds), index, index+1)
	return a, true, len(a.Holds) == 0 && !a.Kept
}

// AttachmentTotals counts the instances of the Attachments map and their
// bytes.
type AttachmentTotals struct {
	Count int
	Bytes int64
}

// AttachmentLimits bound the attachments a Project's messages send over its
// lifetime: their number and their bytes (attachments.projectLifetimeFiles
// and attachments.projectLifetimeBytes).
type AttachmentLimits struct {
	Files int
	Bytes int64
}

// AttachmentReservation reserves the attachments of the message MessageID,
// at most conversation.MaximumMessageAttachments, for one admission of it:
// AdmissionID is new for each SendMessage call, so a retry of the message
// holds its attachments under an admission of its own.
type AttachmentReservation struct {
	MessageID, AdmissionID string
	Files                  []AttachedFile
}

// attachmentRelease frees the holds of the admission AdmissionID.
type attachmentRelease struct {
	AdmissionID   string
	AttachmentIDs []string
}

// attachmentResult answers an attachment RPC with its rejection only.
type attachmentResult struct{ Rejection *dexflow.Rejection }

// projectAttachment is the instance of file as the message messageID
// reserves it at reservedAt, held by the admission admissionID, or kept when
// that is empty.
func projectAttachment(file AttachedFile, messageID, admissionID string, reservedAt time.Time) ProjectAttachment {
	a := ProjectAttachment{Kind: file.Kind, MediaType: file.MediaType, FileName: file.FileName, SHA256: file.SHA256, Bytes: file.Bytes,
		FirstMessageID: messageID, ReservedAt: reservedAt, Kept: admissionID == ""}
	if admissionID != "" {
		a.Holds = []string{admissionID}
	}
	return a
}

// MatchedExample is what the closest-example decision found in the first
// message: the facets of the example's match block it mentions and its score.
type MatchedExample struct {
	Facets []string `json:"facets"`
	Score  int      `json:"score"`
}

// teamSpaceAdmission is the input of the Project's registration in its
// TeamSpace and of the Step that records its refusal, which carries the
// TeamSpace's rejection, or none when the registration could not be confirmed.
type teamSpaceAdmission struct {
	CreatorIsPlatformAdmin bool
	Rejection              *dexflow.Rejection `json:",omitempty"`
}

// projectResult answers a Project RPC with the snapshot after it, or with the
// business rejection that left the Project unchanged.
type projectResult struct {
	Project   Project
	Rejection *dexflow.Rejection
}

// childAdmissionResult confirms exact cleanup membership without reading or
// returning the unrelated Project snapshot.
type childAdmissionResult struct{ Rejection *dexflow.Rejection }

// SetupCommit hands Setup's repository binding and manifest to the Project.
type SetupCommit struct {
	SetupFlowID string
	Repository  RepositoryBindingRef
	AppManifest *pb.AppManifestSnapshot
}

// ProjectStorageUpdate hands the configuration storage identities Setup
// discovered to the Project, against the references Setup last read.
type ProjectStorageUpdate struct {
	SetupFlowID       string
	Expected, Storage infra.StorageRefs
}

// LiveExecutionUpdate hands the Live execution identities a deployment
// discovered to the Project, under the deployment's lease and against the
// references the deployment last read.
type LiveExecutionUpdate struct {
	Lease               DeploymentLease
	Expected, Execution infra.ExecutionRefs
}

// DeploymentCommit makes a verified deployment the Project's Live.
type DeploymentCommit struct {
	Lease   DeploymentLease
	Pointer LivePointer
}

// storageExtends and executionExtends report whether next keeps every
// identity old already owns: a recorded UID or name may be added, never
// replaced.
func storageExtends(old, next infra.StorageRefs) bool {
	return keepsIdentities([][2]infra.ResourceRef{{old.Boundary, next.Boundary}, {old.Storage, next.Storage}}, [][2]string{{old.StorageBucket, next.StorageBucket}, {old.BlobPrefix, next.BlobPrefix}})
}

func executionExtends(old, next infra.ExecutionRefs) bool {
	credential := next.TemporalCredential
	if rotatesCredential(old.TemporalCredential, credential) {
		credential = old.TemporalCredential
	}
	return keepsIdentities([][2]infra.ResourceRef{{old.TemporalCredential, credential}, {old.DexService, next.DexService}}, [][2]string{{old.TemporalNamespace, next.TemporalNamespace}})
}

// rotatesCredential reports whether next is the following generation of a
// recorded Temporal credential in the same boundary: a rotated API key.
func rotatesCredential(old, next infra.ResourceRef) bool {
	return old.UID != "" && next.UID != "" && next.Boundary == old.Boundary && next.Generation == old.Generation+1
}

// keepsIdentities reports whether each (old, next) pair keeps a recorded old
// value: a reference with a UID or a nonempty name.
func keepsIdentities(references [][2]infra.ResourceRef, names [][2]string) bool {
	for _, pair := range references {
		if pair[0].UID != "" && pair[0] != pair[1] {
			return false
		}
	}
	for _, pair := range names {
		if pair[0] != "" && pair[0] != pair[1] {
			return false
		}
	}
	return true
}

// The tests name these constants.
const (
	StatusSetupFailedForTest = statusSetupFailed
)
