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

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	connections "github.com/superdurable/superversev2/internal/dexai/agents/remoteagent"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteagentdao"
	"github.com/superdurable/superversev2/internal/dexai/agents/remoteexec"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type server struct {
	pb.UnimplementedRemoteAgentServiceServer
	*boundary.Boundary
	connections *connections.Connections
}

// Register binds Builder connection and executor operations behind the BFF boundary.
func Register(registrar grpc.ServiceRegistrar, b *boundary.Boundary, service *connections.Connections) {
	pb.RegisterRemoteAgentServiceServer(registrar, &server{Boundary: b, connections: service})
}

func (s *server) CreateRemoteAgentPairing(ctx context.Context, request *pb.CreateRemoteAgentPairingRequest) (*pb.CreateRemoteAgentPairingResponse, error) {
	caller := request.GetCaller()
	if caller == nil {
		return nil, status.Error(codes.Unauthenticated, "explicit caller identity is required")
	}
	account, loginID, err := s.Accounts.AuthenticateLogin(ctx, caller.GetAccountId(), caller.GetCanonicalEmail(), caller.GetSessionToken())
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	pairing, err := s.connections.CreatePairing(ctx, account, loginID, request.GetProjectId())
	if err != nil {
		return nil, rpcStatus(err)
	}
	currentID, owner := current(pairing.Executor)
	return &pb.CreateRemoteAgentPairingResponse{ConnectionId: pairing.ConnectionID, Code: pairing.Code,
		ExpiresAt: timestamppb.New(pairing.ExpiresAt), Endpoint: pairing.Endpoint, BasisEpoch: pairing.Executor.Epoch,
		CurrentConnectionId: currentID, CurrentOwner: owner}, nil
}

func (s *server) ListRemoteAgentConnections(ctx context.Context, request *pb.ListRemoteAgentConnectionsRequest) (*pb.ListRemoteAgentConnectionsResponse, error) {
	account, err := s.Authenticate(ctx, request.GetCaller())
	if err != nil {
		return nil, err
	}
	rows, executor, err := s.connections.List(ctx, account, request.GetProjectId())
	if err != nil {
		return nil, rpcStatus(err)
	}
	currentID, owner := current(executor)
	response := &pb.ListRemoteAgentConnectionsResponse{Epoch: executor.Epoch, CurrentConnectionId: currentID, CurrentOwner: owner, Endpoint: s.connections.Endpoint}
	for _, row := range rows {
		item := &pb.RemoteAgentConnection{ConnectionId: row.ConnectionID,
			Owner:     actor(conversation.Actor{AccountID: row.AccountID, Email: row.CanonicalEmail}),
			CreatedAt: timestamppb.New(row.CreatedAt)}
		switch row.Status {
		case remoteagentdao.StatusPairing:
			item.Status = pb.RemoteAgentConnectionStatus_REMOTE_AGENT_CONNECTION_STATUS_PAIRING
		case remoteagentdao.StatusPaired:
			item.Status = pb.RemoteAgentConnectionStatus_REMOTE_AGENT_CONNECTION_STATUS_PAIRED
		case remoteagentdao.StatusRevoked:
			item.Status = pb.RemoteAgentConnectionStatus_REMOTE_AGENT_CONNECTION_STATUS_REVOKED
		default:
			return nil, status.Error(codes.Unavailable, "unknown connection credential status")
		}
		if row.LastSeenAt.Valid {
			item.LastSeenAt = timestamppb.New(row.LastSeenAt.V)
		}
		if len(row.Binding) != 0 {
			var metadata remoteexec.Binding
			if err = json.Unmarshal(row.Binding, &metadata); err != nil {
				return nil, boundary.RPCStatus(err)
			}
			item.Binding = binding(metadata)
		}
		if executor.Remote != nil && row.ConnectionID == currentID {
			item.Binding = binding(executor.Remote.Binding)
		}
		response.Connections = append(response.Connections, item)
	}
	return response, nil
}

func (s *server) RevokeRemoteAgentConnection(ctx context.Context, request *pb.RevokeRemoteAgentConnectionRequest) (*pb.RevokeRemoteAgentConnectionResponse, error) {
	account, err := s.Authenticate(ctx, request.GetCaller())
	if err != nil {
		return nil, err
	}
	if request.GetConnectionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "connection identity is required")
	}
	if err = s.connections.Revoke(ctx, account, request.GetProjectId(), request.GetConnectionId()); err != nil {
		return nil, rpcStatus(err)
	}
	return &pb.RevokeRemoteAgentConnectionResponse{}, nil
}

func current(executor remoteexec.Executor) (string, *pb.Actor) {
	if executor.Mode != remoteexec.ModeRemote || executor.Remote == nil {
		return "", nil
	}
	return executor.Remote.ConnectionID, actor(executor.Remote.Owner)
}

func actor(owner conversation.Actor) *pb.Actor {
	if owner.AccountID == "" {
		return nil
	}
	return &pb.Actor{AccountId: owner.AccountID, Email: owner.Email}
}

func binding(value remoteexec.Binding) *pb.RemoteAgentBinding {
	return &pb.RemoteAgentBinding{CodexVersion: value.CodexVersion,
		ProviderThreadId: value.ProviderThreadID, HostLabel: value.HostLabel}
}

func (s *server) SetSessionExecutor(ctx context.Context, request *pb.SetSessionExecutorRequest) (*pb.SetSessionExecutorResponse, error) {
	account, err := s.Authenticate(ctx, request.GetCaller())
	if err != nil {
		return nil, err
	}
	if err = s.connections.UsePlatform(ctx, account, request.GetProjectId(), request.GetBasisEpoch()); err != nil {
		return nil, rpcStatus(err)
	}
	return &pb.SetSessionExecutorResponse{}, nil
}
