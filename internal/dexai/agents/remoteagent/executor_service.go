// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package remoteagent

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	"golang.org/x/mod/semver"

	"github.com/superdurable/superversev2/internal/comm/platform/postgres"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagentdao"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/dexai/tenancy/accounts"
)

// ErrExecutorChanged is a failed browser basis-epoch CAS; it carries no state.
var ErrExecutorChanged = errors.New("executor changed")

// ErrBridgeOutdated refuses resumption by a build below the configured minimum.
var ErrBridgeOutdated = errors.New("update dexai and pair again before resuming")

// activate uses only the SQL pairing's immutable basis and authenticated owner.
func (s *Connections) activate(ctx context.Context, access remoteagentdao.Access, request bridgeapi.ActivateRequest) (bridgeapi.ActivateResponse, error) {
	current, err := s.Sessions.Agent.ReadExecutor(ctx, access.ProjectID)
	if err != nil {
		return bridgeapi.ActivateResponse{}, err
	}
	if current.Mode == remoteexec.ModePlatform && current.Suspended != nil && current.Suspended.ConnectionID == access.ConnectionID {
		return bridgeapi.ActivateResponse{Idle: true, Epoch: current.Epoch, Generation: current.Generation}, nil
	}
	selection := remoteexec.SetRequest{ProjectID: access.ProjectID, BasisEpoch: uint64(access.BasisEpoch), Mode: remoteexec.ModeRemote,
		ConnectionID: access.ConnectionID, Owner: accessActor(access), Binding: remoteexec.Binding{BridgeVersion: request.BridgeVersion, CodexVersion: request.Binding.CodexVersion, ProviderThreadID: request.Binding.ProviderThreadID, HostLabel: request.Binding.HostLabel}}
	if access.BasisEpoch < 0 || selection.Validate() != nil {
		return bridgeapi.ActivateResponse{}, refusal(bridgeapi.ErrorMalformed)
	}
	result, err := s.Sessions.Agent.SetExecutor(ctx, selection)
	if err != nil {
		return bridgeapi.ActivateResponse{}, err
	}
	if !result.Accepted {
		return bridgeapi.ActivateResponse{}, executorRefusal(result.Executor)
	}
	// Close removal-before-activation even when both remover reads preceded this
	// commit. A dependency failure leaves an explicit offline residual, not success.
	fresh, err := s.DAO.Authenticate(ctx, access.TokenHash.V, s.PlatformAdminEmails, accountdao.StatusActive)
	if errors.Is(err, postgres.ErrNotFound) {
		_, switchErr := s.Sessions.Agent.SetExecutor(ctx, remoteexec.SetRequest{ProjectID: access.ProjectID, BasisEpoch: result.Executor.Epoch,
			Mode: remoteexec.ModePlatform, Owner: accessActor(access), Reason: remoteexec.HandoffConnectionRemoved})
		if switchErr != nil {
			return bridgeapi.ActivateResponse{}, switchErr
		}
		return bridgeapi.ActivateResponse{}, refusal(bridgeapi.ErrorUnauthorized)
	}
	if err != nil {
		return bridgeapi.ActivateResponse{}, err
	}
	if err = authorizeAccess(fresh); err != nil {
		return bridgeapi.ActivateResponse{}, err
	}
	last := result.Executor.Remote.ActivationSequence
	if last == nil {
		// A remote epoch committed by an older Worker has no recorded head.
		legacyHead, err := s.Sessions.Agent.ReadRemoteHead(ctx, access.ProjectID, access.ConnectionID, result.Executor.Epoch)
		if err != nil {
			return bridgeapi.ActivateResponse{}, err
		}
		last = &legacyHead
	}
	response := bridgeapi.ActivateResponse{Epoch: result.Executor.Epoch, Generation: result.Executor.Generation,
		Reported: result.Executor.Remote.Reported, LastSequence: uint64(*last), ToolSetDigest: agentloop.RemoteToolDigest()}
	for _, definition := range agentloop.RemoteTools() {
		response.Tools = append(response.Tools, bridgeapi.Tool{Name: string(definition.Name), Description: definition.Description,
			InputSchema: json.RawMessage(definition.InputSchema)})
	}
	return response, nil
}

func accessActor(access remoteagentdao.Access) conversation.Actor {
	return conversation.Actor{AccountID: access.AccountID, Email: access.CanonicalEmail}
}

func executorRefusal(executor remoteexec.Executor) error {
	return &remoteexec.ReportRefusal{Code: remoteexec.RefusalExecutorChanged, Message: "executor changed", Epoch: executor.Epoch, Generation: executor.Generation}
}

// UsePlatform changes execution authority without revoking the paired connection.
func (s *Connections) UsePlatform(ctx context.Context, account accounts.AccountSummary, projectID string, basis uint64) error {
	caller, err := s.Sessions.AuthorizeSession(ctx, account, projectID)
	if err != nil {
		return err
	}
	result, err := s.Sessions.Agent.SetExecutor(ctx, remoteexec.SetRequest{ProjectID: projectID, BasisEpoch: basis,
		Mode: remoteexec.ModePlatform, Owner: caller.Actor, Reason: remoteexec.HandoffSwitched})
	if err != nil {
		return err
	}
	if !result.Accepted {
		return ErrExecutorChanged
	}
	return nil
}

// Resume reauthorizes both Builders; only the Session can admit the suspended ID.
func (s *Connections) Resume(ctx context.Context, account accounts.AccountSummary, projectID, connectionID string, basis uint64) error {
	caller, err := s.Sessions.AuthorizeSession(ctx, account, projectID)
	if err != nil {
		return err
	}
	access, err := s.DAO.ReadPairedAccess(ctx, projectID, connectionID, s.PlatformAdminEmails, accountdao.StatusActive)
	if err != nil {
		return err
	}
	if err := authorizeAccess(access); err != nil {
		return err
	}
	current, err := s.Sessions.Agent.ReadExecutor(ctx, projectID)
	if err != nil {
		return err
	}
	target := current.Suspended
	if current.Mode == remoteexec.ModeRemote {
		target = current.Remote
	}
	if target == nil || target.ConnectionID != connectionID {
		return ErrExecutorChanged
	}
	version := target.Binding.BridgeVersion
	if !(s.AcceptDevBridge && version == bridgeapi.DevBridgeVersion) &&
		(!semver.IsValid("v"+version) || semver.Compare("v"+version, "v"+s.MinimumBridgeVersion) < 0) {
		return ErrBridgeOutdated
	}
	result, err := s.Sessions.Agent.SetExecutor(ctx, remoteexec.SetRequest{ProjectID: projectID, BasisEpoch: basis,
		Mode: remoteexec.ModeRemote, ConnectionID: connectionID, Resume: true, RequestedBy: &caller.Actor, Owner: accessActor(access), Binding: target.Binding})
	if err != nil {
		return err
	}
	if !result.Accepted {
		return ErrExecutorChanged
	}
	fresh, err := s.DAO.ReadPairedAccess(ctx, projectID, connectionID, s.PlatformAdminEmails, accountdao.StatusActive)
	if err == nil {
		err = authorizeAccess(fresh)
	}
	if err != nil {
		if compensation := s.compensateRemoval(ctx, projectID, connectionID, accessActor(access)); compensation != nil {
			return compensation
		}
		return err
	}
	return nil
}

func (s *Connections) compensateRemoval(ctx context.Context, project, connection string, owner conversation.Actor) error {
	current, err := s.Sessions.Agent.ReadExecutor(ctx, project)
	if err != nil {
		return err
	}
	if !(current.Mode == remoteexec.ModeRemote && current.Remote != nil && current.Remote.ConnectionID == connection) &&
		!(current.Mode == remoteexec.ModePlatform && current.Suspended != nil && current.Suspended.ConnectionID == connection) {
		return nil
	}
	_, err = s.Sessions.Agent.SetExecutor(ctx, remoteexec.SetRequest{ProjectID: project, BasisEpoch: current.Epoch,
		Mode: remoteexec.ModePlatform, ConnectionID: connection, Owner: owner, Reason: remoteexec.HandoffConnectionRemoved})
	return err // a newer executor's CAS refusal is harmless
}

// currentAccess is additional executor admission, never an SQL auth substitute.
func (s *Connections) currentAccess(ctx context.Context, access remoteagentdao.Access) error {
	current, err := s.Sessions.Agent.ReadExecutor(ctx, access.ProjectID)
	if err != nil {
		return err
	}
	if current.Mode != remoteexec.ModeRemote || current.Remote == nil || current.Remote.ConnectionID != access.ConnectionID {
		return executorRefusal(current)
	}
	return nil
}

func (s *Connections) report(ctx context.Context, access remoteagentdao.Access, request bridgeapi.Report) (bridgeapi.ReportResponse, error) {
	report, err := remoteReport(access.ConnectionID, request)
	if err != nil {
		return bridgeapi.ReportResponse{}, refusal(bridgeapi.ErrorMalformed)
	}
	if err = s.currentAccess(ctx, access); err != nil {
		return bridgeapi.ReportResponse{}, err
	}
	answer, err := s.Sessions.Agent.ReportRemote(ctx, remoteexec.ReportRequest{ProjectID: access.ProjectID, Report: report})
	return reportResponse(answer), err
}

func validateSync(request bridgeapi.SyncRequest) error {
	if request.After > math.MaxInt64 || len(request.StateDigest) > 64 {
		return refusal(bridgeapi.ErrorMalformed)
	}
	return nil
}
