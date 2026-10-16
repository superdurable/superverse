// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package repobinding

import (
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
)

var (
	RepositoryStartIdentity = dex.DefineAttribute[dexflow.StartIdentity]("StartIdentity")
	RepositoryRevision      = dex.DefineAttribute[uint64]("Revision")
	RepositoryCreatedAt     = dex.DefineAttribute[time.Time]("CreatedAt")
	RepositoryUpdatedAt     = dex.DefineAttribute[time.Time]("UpdatedAt")
)

var (
	RepositoryID      = dex.DefineAttribute[int64]("RepositoryID")
	BoundProjectID    = dex.DefineAttribute[string]("BoundProjectID")
	BindingGeneration = dex.DefineAttribute[uint64]("BindingGeneration")
	// dex:indexed-attribute attribute-key:BindingStatus index-key:keyword3 index-type:keyword value-type:string description:"Business status"
	BindingStatus   = dex.DefineAttribute[string]("BindingStatus", dex.Indexed(dex.AttributeIndex{Type: dex.IndexKeyword, IndexKey: "keyword3"}))
	AcceptedClaimID = dex.DefineAttribute[string]("AcceptedClaimID")
)

// The statuses of a binding. They stay untyped because the indexed
// BindingStatus Attribute is a plain string.
const (
	// StatusBound: one Project's Setup holds the repository.
	StatusBound = "BOUND"
	// StatusUnbound: started, no claim committed yet (generation 0).
	StatusUnbound = "UNBOUND"
	// StatusReleased: terminal; the releasing Project archived the repository, and the Flow completes.
	StatusReleased = "RELEASED"
)

// RepositoryBinding is the complete RepositoryBindingFlow snapshot.
type RepositoryBinding struct {
	Identity             dexflow.StartIdentity
	CreatedAt, UpdatedAt time.Time
	Revision             uint64

	RepositoryID               int64
	ProjectID, Status, ClaimID string
	Generation                 uint64
}

// repositoryClaim binds the repository to one Project's Setup.
type repositoryClaim struct {
	RepositoryID       int64
	ProjectID, ClaimID string
}

// repositoryRelease ends exactly one claim generation.
type repositoryRelease struct {
	ProjectID  string
	Generation uint64
}

// repositoryResult answers a claim or release with the binding after it.
type repositoryResult struct {
	Binding   RepositoryBinding
	Rejection *dexflow.Rejection
}

// RepositoryBindingFlow owns the Project a managed repository is bound to, addressed by
// repository-<numeric repository ID>; claims and releases are RPCs, and the release ends it.
type RepositoryBindingFlow struct{ dex.FlowDefaults }

func (f *RepositoryBindingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{dex.DefineStep(CompleteRepositoryBinding{flow: f})}
}

func (f *RepositoryBindingFlow) GetRPCs() []dex.RPCDef {
	write := dexflow.ShortRPC(dex.LockAttribute(RepositoryRevision))
	return []dex.RPCDef{
		dex.DefineRPC(f.GetDexSummary, dexflow.ShortRPC()), dex.DefineRPC(f.GetDexDisplay, dexflow.ShortRPC()), dex.DefineRPC(f.GetRepositoryBinding, dexflow.ShortRPC()),
		dex.DefineRPC(f.ClaimRepository, write), dex.DefineRPC(f.ReleaseRepository, write),
	}
}

func (*RepositoryBindingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{RepositoryStartIdentity, RepositoryRevision, RepositoryCreatedAt, RepositoryUpdatedAt, RepositoryID, BoundProjectID, BindingGeneration, BindingStatus, AcceptedClaimID}}
}

func (f *RepositoryBindingFlow) InitialAttributes(repositoryID int64, start dexflow.StartIdentity) []dex.InitialAttributeDef {
	b := &dexflow.InitialAttributes{}
	now := time.Now().UTC()
	dexflow.Add(b, RepositoryStartIdentity, start)
	dexflow.Add(b, RepositoryRevision, uint64(1))
	dexflow.Add(b, RepositoryCreatedAt, now)
	dexflow.Add(b, RepositoryUpdatedAt, now)
	dexflow.Add(b, RepositoryID, repositoryID)
	dexflow.Add(b, BoundProjectID, "")
	dexflow.Add(b, BindingGeneration, uint64(0))
	dexflow.Add(b, BindingStatus, StatusUnbound)
	dexflow.Add(b, AcceptedClaimID, "")
	return b.Values()
}

func (f *RepositoryBindingFlow) GetRepositoryBinding(ctx dex.Context, _ dex.None) (*dex.RPCResult[RepositoryBinding], error) {
	return &dex.RPCResult[RepositoryBinding]{Output: RepositoryBinding{
		Identity:     comm.Must(RepositoryStartIdentity.Get(ctx)),
		CreatedAt:    comm.Must(RepositoryCreatedAt.Get(ctx)),
		UpdatedAt:    comm.Must(RepositoryUpdatedAt.Get(ctx)),
		Revision:     comm.Must(RepositoryRevision.Get(ctx)),
		RepositoryID: comm.Must(RepositoryID.Get(ctx)),
		ProjectID:    comm.Must(BoundProjectID.Get(ctx)),
		Generation:   comm.Must(BindingGeneration.Get(ctx)),
		Status:       comm.Must(BindingStatus.Get(ctx)),
		ClaimID:      comm.Must(AcceptedClaimID.Get(ctx)),
	}}, nil
}

// ClaimRepository binds an unbound repository to the claiming Setup under a new generation; the same claim
// repeated finds its binding, another claim of a bound one is ALREADY_EXISTS, a released one FAILED_PRECONDITION.
func (f *RepositoryBindingFlow) ClaimRepository(ctx dex.Context, in repositoryClaim) (*dex.RPCResult[repositoryResult], error) {
	b := f.readBinding(ctx)
	if b.Status == StatusReleased {
		return &dex.RPCResult[repositoryResult]{Output: repositoryResult{Binding: b, Rejection: errBindingEnded()}}, nil
	}
	if b.Status == StatusBound {
		result := repositoryResult{Binding: b}
		if b.ProjectID != in.ProjectID || b.ClaimID != in.ClaimID {
			result.Rejection = dexflow.Reject(dexflow.RejectionAlreadyExists, "repository is already bound")
		}
		return &dex.RPCResult[repositoryResult]{Output: result}, nil
	}
	comm.MustDo(BoundProjectID.Set(ctx, in.ProjectID))
	comm.MustDo(AcceptedClaimID.Set(ctx, in.ClaimID))
	comm.MustDo(BindingGeneration.Set(ctx, b.Generation+1))
	comm.MustDo(BindingStatus.Set(ctx, StatusBound))
	touchRepository(ctx)
	return &dex.RPCResult[repositoryResult]{Output: repositoryResult{Binding: f.readBinding(ctx)}}, nil
}

// ReleaseRepository ends the binding of an archived repository for the claimed generation of the releasing
// Project, or an UNBOUND binding at its generation, and completes the Flow; anything else is ABORTED.
func (f *RepositoryBindingFlow) ReleaseRepository(ctx dex.Context, in repositoryRelease) (*dex.RPCResult[repositoryResult], error) {
	b := f.readBinding(ctx)
	if !b.releasableBy(in) {
		return &dex.RPCResult[repositoryResult]{Output: repositoryResult{Binding: b, Rejection: errReleaseMismatch()}}, nil
	}
	if b.Status == StatusReleased {
		return &dex.RPCResult[repositoryResult]{Output: repositoryResult{Binding: b}}, nil
	}
	comm.MustDo(BoundProjectID.Set(ctx, in.ProjectID))
	comm.MustDo(BindingStatus.Set(ctx, StatusReleased))
	touchRepository(ctx)
	return &dex.RPCResult[repositoryResult]{Output: repositoryResult{Binding: f.readBinding(ctx)},
		NextSteps: []dex.StepMovement{dex.MovementOf(CompleteRepositoryBinding{flow: f}, nil)}}, nil
}

// dex:group group-id:binding group-label:"Repository binding"
// dex:explanation text:"Complete the binding that ReleaseRepository ended; the retained execution keeps it readable."
type CompleteRepositoryBinding struct {
	dex.StepDefaultsNoWaitFor[dex.None]
	flow *RepositoryBindingFlow
}

func (CompleteRepositoryBinding) GetStepOptions() *dex.StepOptions { return dexflow.StateStepOptions() }

func (s CompleteRepositoryBinding) Execute(ctx dex.Context, _ dex.None) (*dex.StepDecision, error) {
	return dex.GracefulComplete(s.flow.readBinding(ctx)), nil
}

// dex:field attribute-key:BoundProjectID value-type:string editable:false description:"BoundProjectID"
func (*RepositoryBindingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"BoundProjectID": comm.Must(BoundProjectID.Get(ctx))}}, nil
}

// dex:field attribute-key:BindingStatus value-type:string editable:false description:"Repository binding state"
func (*RepositoryBindingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"BindingStatus": comm.Must(BindingStatus.Get(ctx))}}, nil
}

// readBinding reads the snapshot of the invoking execution.
func (f *RepositoryBindingFlow) readBinding(ctx dex.Context) RepositoryBinding {
	return comm.Must(f.GetRepositoryBinding(ctx, nil)).Output
}

// releasableBy reports whether in may release b: its generation, and its Project unless b is UNBOUND.
func (b RepositoryBinding) releasableBy(in repositoryRelease) bool {
	return in.Generation == b.Generation && (b.Status == StatusUnbound || b.ProjectID == in.ProjectID)
}

func errBindingEnded() *dexflow.Rejection {
	return dexflow.Reject(dexflow.RejectionFailedPrecondition, "repository binding ended")
}

func errReleaseMismatch() *dexflow.Rejection {
	return dexflow.Reject(dexflow.RejectionAborted, "repository binding generation changed")
}

func touchRepository(ctx dex.Context) {
	comm.MustDo(RepositoryRevision.Set(ctx, comm.Must(RepositoryRevision.Get(ctx))+1))
	comm.MustDo(RepositoryUpdatedAt.Set(ctx, time.Now().UTC()))
}
