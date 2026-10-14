// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package teamspaces

import (
	"context"
	"errors"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
	"github.com/superdurable/superversev2/internal/comm/dexflow"
	"github.com/superdurable/superversev2/internal/comm/flowid"
	"github.com/superdurable/superversev2/internal/platform/postgres"
)

// lockWait bounds a write, including its wait for another write's Revision
// lock and its Attribute Store sync, which a loaded lane slows to several
// seconds.
const lockWait = 30 * time.Second

// existenceReadTimeout bounds the read of TeamSpaceFlow that tells a TeamSpace
// whose row Dex has not written yet from one that does not exist.
const existenceReadTimeout = 10 * time.Second

// AwaitRow reads a row of the TeamSpace's read model with read, as
// postgres.AwaitRow does: when the row is missing, one read of TeamSpaceFlow
// tells a TeamSpace created a moment ago, whose row is read again, from one
// that does not exist or is DELETED, which is postgres.ErrNotFound at once.
func AwaitRow[T any](ctx context.Context, c *TeamSpaceFlowClient, teamSpaceID string, read func(context.Context) (T, error)) (T, error) {
	return postgres.AwaitRow(ctx, read, func(ctx context.Context) (bool, error) { return c.pending(ctx, teamSpaceID) })
}

// pending reports whether the TeamSpace has a TeamSpaceFlow that is not
// DELETED: a bounded read, which a just-created TeamSpace answers.
func (c *TeamSpaceFlowClient) pending(ctx context.Context, teamSpaceID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, existenceReadTimeout)
	defer cancel()
	var display map[string]any
	err := dexflow.InvokeRetainedRead(ctx, c.Dex, flowid.TeamSpaceFlowID(teamSpaceID), c.Flow.GetDexDisplay, nil, &display)
	if _, missing := errors.AsType[*dex.FlowNotFoundError](err); missing {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return display["TeamSpaceStatus"] != StatusDeleted, nil
}

// TeamSpaceFlowClient fronts TeamSpaceFlow: its start and its writes, each of
// which the Flow admits under its Revision lock, waiting for a concurrent one. Reads of TeamSpaces and their
// members come from the read model, not from the Flow.
type TeamSpaceFlowClient struct {
	Dex  *dex.Client
	Flow *TeamSpaceFlow
}

// Create starts the TeamSpaceFlow of an accepted creation. A retry of the same
// request joins the execution it started; another request for the same ID is
// a typed FlowAlreadyStartedError.
func (c *TeamSpaceFlowClient) Create(ctx context.Context, teamSpaceID string, in Start) error {
	_, err := c.Dex.StartFlow(ctx, c.Flow, flowid.TeamSpaceFlowID(teamSpaceID), nil, dex.StartFlowOptions{
		IDReusePolicy: dex.IDReuseDisallow, RequestID: &in.Identity.RequestID,
		AlreadyStarted: &dex.AlreadyStartedOptions{IgnoreError: true}, Attributes: c.Flow.InitialAttributes(in),
		ConfigOverride: &dex.FlowConfig{AttributeStoreNames: postgres.TeamSpaceStore.Names()},
	})
	return err
}

// AddMember, SetMemberRole and RemoveMember change the members as the actor,
// and return the TeamSpace and its members after the change.
func (c *TeamSpaceFlowClient) AddMember(ctx context.Context, teamSpaceID string, in MemberChange) (Update, error) {
	return c.invoke(ctx, teamSpaceID, c.Flow.AddTeamSpaceMember, in, dex.RPCInvokeOptions{})
}

func (c *TeamSpaceFlowClient) SetMemberRole(ctx context.Context, teamSpaceID string, in MemberChange) (Update, error) {
	return c.invoke(ctx, teamSpaceID, c.Flow.SetTeamSpaceMemberRole, in, dex.RPCInvokeOptions{})
}

func (c *TeamSpaceFlowClient) RemoveMember(ctx context.Context, teamSpaceID string, in MemberChange) (Update, error) {
	return c.invoke(ctx, teamSpaceID, c.Flow.RemoveTeamSpaceMember, in, dex.RPCInvokeOptions{})
}

// Rename renames the TeamSpace at the revision the actor observed.
func (c *TeamSpaceFlowClient) Rename(ctx context.Context, teamSpaceID string, in Renaming) (TeamSpace, error) {
	result, err := c.invoke(ctx, teamSpaceID, c.Flow.RenameTeamSpace, in,
		dex.RPCInvokeOptions{LoadAttributeMapInstances: []dex.AttributeMapLoad{TeamSpaceMembers.Load(in.Actor.AccountID)}})
	return result.TeamSpace, err
}

// RequestDeletion admits the deletion command of the request.
func (c *TeamSpaceFlowClient) RequestDeletion(ctx context.Context, teamSpaceID string, in DeletionRequest) (TeamSpace, error) {
	result, err := c.invoke(ctx, teamSpaceID, c.Flow.RequestTeamSpaceDeletion, in, dex.RPCInvokeOptions{})
	return result.TeamSpace, err
}

// RegisterProject registers a Project its creator starts, before any of the
// Project's effects. A refusal is a *dexflow.Rejection; a TeamSpace that never
// existed or that completed its deletion refuses it like one that is deleting.
func (c *TeamSpaceFlowClient) RegisterProject(ctx context.Context, teamSpaceID, projectID string, creator Actor) error {
	in := registration{Actor: creator, ProjectID: projectID}
	err := c.invokeRegistration(ctx, teamSpaceID, c.Flow.RegisterProject, in, dex.RPCInvokeOptions{LoadAttributeMapInstances: []dex.AttributeMapLoad{
		TeamSpaceMembers.Load(creator.AccountID), TeamSpaceProjects.Load(projectID),
	}})
	var missing *dex.FlowNotFoundError
	var inactive *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &missing) || errors.As(err, &inactive) {
		return dexflow.Reject(dexflow.RejectionNotFound, "the TeamSpace does not exist or is deleted")
	}
	return err
}

// UnregisterProject removes a deleted Project from the registry. A TeamSpace
// that is gone holds no registry, so there is nothing to remove.
func (c *TeamSpaceFlowClient) UnregisterProject(ctx context.Context, teamSpaceID, projectID string) error {
	err := c.invokeRegistration(ctx, teamSpaceID, c.Flow.UnregisterProject, registration{ProjectID: projectID},
		dex.RPCInvokeOptions{LoadAttributeMapInstances: []dex.AttributeMapLoad{TeamSpaceProjects.Load(projectID)}})
	var missing *dex.FlowNotFoundError
	var inactive *dex.FlowNotActiveOrNotFoundError
	if errors.As(err, &missing) || errors.As(err, &inactive) {
		return nil
	}
	return err
}

// invoke calls a TeamSpaceFlow RPC that answers with a Update. It
// returns the result, with the call's rejection as the error.
func (c *TeamSpaceFlowClient) invoke(ctx context.Context, teamSpaceID string, rpc, in any, options dex.RPCInvokeOptions) (Update, error) {
	result, err := dexflow.InvokeLockedWith[Update](ctx, c.Dex, lockWait, flowid.TeamSpaceFlowID(teamSpaceID), rpc, in, options)
	if err != nil {
		return Update{}, err
	}
	return result, result.Rejection.Err()
}

// invokeRegistration calls a Project registry RPC, with its rejection as the
// error.
func (c *TeamSpaceFlowClient) invokeRegistration(ctx context.Context, teamSpaceID string, rpc any, in registration, options dex.RPCInvokeOptions) error {
	result, err := dexflow.InvokeLockedWith[registrationResult](ctx, c.Dex, lockWait, flowid.TeamSpaceFlowID(teamSpaceID), rpc, in, options)
	if err != nil {
		return err
	}
	return result.Rejection.Err()
}
