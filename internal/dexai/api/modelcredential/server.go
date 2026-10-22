// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package modelcredential

import (
	"context"
	"errors"
	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/comm"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider/openaicompat"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math"
	"strings"
	"time"
)

type server struct {
	pb.UnimplementedModelCredentialServiceServer
	*boundary.Boundary
	sessions *agents.SessionFlowService
}

// Register exposes the authenticated Project Builder credential boundary.
func Register(registrar grpc.ServiceRegistrar, b *boundary.Boundary, sessions *agents.SessionFlowService) {
	pb.RegisterModelCredentialServiceServer(registrar, &server{Boundary: b, sessions: sessions})
}

func (s *server) authorize(ctx context.Context, caller *pb.CallerContext, project string, provider pb.ModelProvider) (agents.SessionCaller, error) {
	account, err := s.Authenticate(ctx, caller)
	if err != nil {
		return agents.SessionCaller{}, err
	}
	scope, err := s.sessions.AuthorizeSession(ctx, account, project)
	if err != nil {
		return scope, boundary.RPCStatus(err)
	}
	value, err := boundary.DomainValue(provider)
	var selected modelcredential.Provider
	if err != nil || selected.UnmarshalText([]byte(strings.ToLower(value))) != nil {
		return scope, status.Error(codes.InvalidArgument, "unsupported model credential provider")
	}
	return scope, nil
}

func (s *server) GetModelCredentialInfo(ctx context.Context, request *pb.GetModelCredentialInfoRequest) (*pb.GetModelCredentialInfoResponse, error) {
	caller, err := s.authorize(ctx, request.GetCaller(), request.GetProjectId(), request.GetProvider())
	if err != nil {
		return nil, err
	}
	info, err := s.sessions.Agent.ReadModelCredentialInfo(ctx, caller.FlowID, credentialProvider(request.GetProvider()))
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return &pb.GetModelCredentialInfoResponse{Credential: credentialDocument(info, credentialProvider(request.GetProvider()))}, nil
}

func (s *server) SetModelCredential(ctx context.Context, request *pb.SetModelCredentialRequest) (*pb.SetModelCredentialResponse, error) {
	caller, err := s.authorize(ctx, request.GetCaller(), request.GetProjectId(), request.GetProvider())
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(request.GetApiKey())
	request.ApiKey = ""
	if key == "" {
		info, readErr := s.sessions.Agent.ReadModelCredentialInfo(ctx, caller.FlowID, credentialProvider(request.GetProvider()))
		if readErr != nil {
			return nil, credentialError(readErr)
		}
		if request.ExpectedRevision == nil || info.OperationID != request.GetRequestId() || info.OperationRevision != request.GetExpectedRevision() || info.Delete {
			return nil, status.Error(codes.InvalidArgument, "an API key is required for a new operation")
		}
	} else if len(key) < 4 || len(key) > 4096 {
		return nil, status.Error(codes.InvalidArgument, "the API key must contain 4 to 4096 printable ASCII characters")
	}
	for _, c := range key {
		if c < 0x21 || c > 0x7e {
			return nil, status.Error(codes.InvalidArgument, "the API key must contain printable ASCII characters without spaces")
		}
	}
	input, err := mutation(request.GetRequestId(), request.ExpectedRevision, false, caller.Actor, credentialProvider(request.GetProvider()))
	if err != nil {
		return nil, err
	}
	if key != "" {
		input.Last4 = key[len(key)-4:]
	}
	info, err := s.sessions.ModelCredentials.Mutate(ctx, caller.FlowID, input, key)
	key = ""
	if err != nil {
		return nil, credentialError(err)
	}
	return &pb.SetModelCredentialResponse{Credential: credentialDocument(info, credentialProvider(request.GetProvider()))}, nil
}

func (s *server) DeleteModelCredential(ctx context.Context, request *pb.DeleteModelCredentialRequest) (*pb.DeleteModelCredentialResponse, error) {
	caller, err := s.authorize(ctx, request.GetCaller(), request.GetProjectId(), request.GetProvider())
	if err != nil {
		return nil, err
	}
	input, err := mutation(request.GetRequestId(), request.ExpectedRevision, true, caller.Actor, credentialProvider(request.GetProvider()))
	if err != nil {
		return nil, err
	}
	info, err := s.sessions.ModelCredentials.Mutate(ctx, caller.FlowID, input, "")
	if err != nil {
		return nil, credentialError(err)
	}
	return &pb.DeleteModelCredentialResponse{Credential: credentialDocument(info, credentialProvider(request.GetProvider()))}, nil
}

func mutation(operation string, revision *uint64, remove bool, actor conversation.Actor, provider modelcredential.Provider) (modelcredential.Mutation, error) {
	if operation == "" || len(operation) > 128 || revision == nil || *revision == math.MaxUint64 {
		return modelcredential.Mutation{}, status.Error(codes.InvalidArgument, "request ID and observed credential revision are required")
	}
	return modelcredential.Mutation{Provider: provider, OperationID: operation, ExpectedRevision: *revision, Delete: remove, Actor: actor}, nil
}

// credentialDocument has a deliberate metadata allowlist; no secret or writer internals reach Studio.
func credentialDocument(info modelcredential.Info, provider modelcredential.Provider) *pb.ModelCredentialInfo {
	value := &pb.ModelCredentialInfo{Revision: info.Revision, Status: boundary.Enum[pb.ModelCredentialStatus](info.Status.String()),
		Last4: info.Last4, SetBy: &pb.Actor{AccountId: info.SetBy.AccountID, Email: info.SetBy.Email},
		Provider: boundary.Enum[pb.ModelProvider](provider.Catalog().String())}
	if !info.SetAt.IsZero() {
		value.SetAt = info.SetAt.Format(time.RFC3339Nano)
	}
	if info.Status == modelcredential.StatusPending {
		value.PendingOperation, value.PendingRevision, value.PendingDelete = info.OperationID, info.OperationRevision, info.Delete
	}
	return value
}

func credentialError(err error) error {
	if errors.Is(err, openaicompat.ErrModelCapacity) {
		return status.Error(codes.ResourceExhausted, "model credential validation is busy; try again shortly")
	}
	if errors.Is(err, modelcredential.ErrCanceled) {
		return status.Error(codes.FailedPrecondition, modelcredential.ErrCanceled.Error())
	}
	if errors.Is(err, modelcredential.ErrRevision) {
		return status.Error(codes.Aborted, "model credential revision changed; refresh and try again")
	}
	if errors.Is(err, modelcredential.ErrPending) {
		return status.Error(codes.Unavailable, "model credential operation is pending reconciliation")
	}
	if _, ok := errors.AsType[*conversation.ModelRejectedError](err); ok {
		return status.Error(codes.FailedPrecondition, "the provider rejected this credential")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "model credential operation timed out; refresh its status")
	}
	return status.Error(codes.Unavailable, "model credential operation did not complete; refresh its status")
}

func credentialProvider(value pb.ModelProvider) modelcredential.Provider {
	var provider modelcredential.Provider
	domain := comm.Must(boundary.DomainValue(value))
	comm.MustDo(provider.UnmarshalText([]byte(strings.ToLower(domain))))
	return provider
}
