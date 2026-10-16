// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package projects

import (
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
	SessionStartFailure  *dexflow.Failure
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
