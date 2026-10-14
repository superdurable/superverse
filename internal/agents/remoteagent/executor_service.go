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

	"github.com/superdurable/superversev2/internal/agents/remoteagent/bridgeapi"
	"github.com/superdurable/superversev2/internal/agents/remoteagentdao"
	"github.com/superdurable/superversev2/internal/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/agents/session/agentloop"
	"github.com/superdurable/superversev2/internal/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/platform/postgres"
	"github.com/superdurable/superversev2/internal/tenancy/accountdao"
	"github.com/superdurable/superversev2/internal/tenancy/accounts"
)

// ErrExecutorChanged is a failed browser basis-epoch CAS; it carries no state.
var ErrExecutorChanged = errors.New("executor changed")

// activate uses only the SQL pairing's immutable basis and authenticated owner.
func (s *Connections) activate(ctx context.Context, access remoteagentdao.Access, request bridgeapi.ActivateRequest) (bridgeapi.ActivateResponse, error) {
	selection := remoteexec.SetRequest{ProjectID: access.ProjectID, BasisEpoch: uint64(access.BasisEpoch), Mode: remoteexec.ModeRemote,
		ConnectionID: access.ConnectionID, Owner: accessActor(access), Binding: remoteexec.Binding{OrcaVersion: request.Binding.OrcaVersion,
			CodexVersion: request.Binding.CodexVersion, OrcaSessionID: request.Binding.OrcaSessionID, ProviderThreadID: request.Binding.ProviderThreadID, HostLabel: request.Binding.HostLabel}}
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
	fresh, err := s.DAO.Authenticate(ctx, access.TokenHash.V, s.PlatformAdminEmail, accountdao.StatusActive)
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
	last, err := s.Sessions.Agent.ReadRemoteHead(ctx, access.ProjectID, access.ConnectionID, result.Executor.Epoch)
	if err != nil {
		return bridgeapi.ActivateResponse{}, err
	}
	response := bridgeapi.ActivateResponse{Epoch: result.Executor.Epoch, Generation: result.Executor.Generation,
		Reported: result.Executor.Remote.Reported, LastSequence: uint64(last), ToolSetDigest: agentloop.RemoteToolDigest()}
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

// UsePlatform authorizes the actual Builder and uses its observed epoch. Only
// the connection this committed switch replaced is revoked afterwards.
func (s *Connections) UsePlatform(ctx context.Context, account accounts.AccountSummary, projectID string, basis uint64) error {
	caller, err := s.Sessions.AuthorizeSession(ctx, account, projectID)
	if err != nil {
		return err
	}
	current, err := s.Sessions.Agent.ReadExecutor(ctx, projectID)
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
	if current.Mode == remoteexec.ModeRemote && current.Remote != nil && current.Epoch == basis {
		err = s.DAO.Revoke(ctx, projectID, current.Remote.ConnectionID, remoteagentdao.RevokePlatform)
		if errors.Is(err, postgres.ErrNotFound) {
			return nil
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
	if current.Mode != remoteexec.ModeRemote || current.Remote == nil || current.Remote.ConnectionID != connection {
		return nil
	}
	_, err = s.Sessions.Agent.SetExecutor(ctx, remoteexec.SetRequest{ProjectID: project, BasisEpoch: current.Epoch,
		Mode: remoteexec.ModePlatform, Owner: owner, Reason: remoteexec.HandoffConnectionRemoved})
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
