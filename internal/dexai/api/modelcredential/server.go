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
	"github.com/superdurable/superversev2/internal/comm/modelendpoint"
	"github.com/superdurable/superversev2/internal/dexai/agents"
	"github.com/superdurable/superversev2/internal/dexai/agents/modelcredential"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/conversation"
	"github.com/superdurable/superversev2/internal/dexai/agents/session/modelprovider"
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
	current, err := s.sessions.Agent.ReadModelCredentialInfo(ctx, caller.FlowID, input.Provider)
	if err != nil {
		return nil, credentialError(err)
	}
	replay := current.OperationID == input.OperationID
	if !replay && current.Revision != input.ExpectedRevision {
		return nil, credentialError(modelcredential.ErrRevision)
	}
	if replay {
		input.Endpoint = current.Endpoint
	}
	if !replay {
		if request.GetBaseUrl() == "" && (request.GetContextWindow() != 0 || request.GetMaxOutputTokens() != 0) {
			return nil, status.Error(codes.InvalidArgument, "custom token budgets require a custom endpoint")
		}
		if request.GetBaseUrl() != "" {
			input.Endpoint, err = modelprovider.NormalizeCustomEndpointBudget(conversation.EndpointSelection{ContextWindow: request.GetContextWindow(), MaxOutputTokens: request.GetMaxOutputTokens()})
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
		}
	}
	if key != "" {
		input.Last4 = key[len(key)-4:]
		if !replay && request.GetBaseUrl() != "" {
			budget := input.Endpoint
			input.Endpoint, err = s.sessions.ModelCredentials.Compatible.ResolveCustom(ctx, input.Provider.Catalog(), request.GetBaseUrl(), request.GetCustomModelId(), key)
			if err != nil {
				return nil, status.Error(codes.FailedPrecondition, "custom endpoint validation failed; select or enter a valid model ID")
			}
			input.Endpoint.ContextWindow, input.Endpoint.MaxOutputTokens = budget.ContextWindow, budget.MaxOutputTokens
		} else if !replay && request.GetCustomModelId() != "" {
			return nil, status.Error(codes.InvalidArgument, "a custom endpoint is required for a custom model ID")
		}
	}
	info, rateLimited, err := s.sessions.ModelCredentials.MutateWithNotice(ctx, caller.FlowID, input, key)
	key = ""
	if err != nil {
		return nil, credentialError(err)
	}
	response := &pb.SetModelCredentialResponse{Credential: credentialDocument(info, credentialProvider(request.GetProvider()))}
	if rateLimited {
		response.Notice = pb.ModelCredentialSaveNotice_MODEL_CREDENTIAL_SAVE_NOTICE_RATE_LIMITED_UNVERIFIED
		response.NoticeMessage = "The endpoint rate-limited the test request; the key is saved but unverified and will be checked on the first model call."
	}
	return response, nil
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
		Provider: boundary.Enum[pb.ModelProvider](provider.Catalog().String()), BaseUrl: info.Endpoint.BaseURL,
		CustomModelId: info.Endpoint.CustomModelID, EffectiveModelId: info.Endpoint.EffectiveModelID, CustomProfileVersion: info.Endpoint.CustomProfileVersion}
	if info.Endpoint.BaseURL != "" {
		endpoint := comm.Must(modelprovider.NormalizeCustomEndpointBudget(info.Endpoint))
		value.ContextWindow, value.MaxOutputTokens = endpoint.ContextWindow, endpoint.MaxOutputTokens
	}
	if !info.SetAt.IsZero() {
		value.SetAt = info.SetAt.Format(time.RFC3339Nano)
	}
	if info.Status == modelcredential.StatusPending {
		value.PendingOperation, value.PendingRevision, value.PendingDelete = info.OperationID, info.OperationRevision, info.Delete
	}
	return value
}

func credentialError(err error) error {
	if probe, ok := errors.AsType[*openaicompat.CredentialProbeError](err); ok {
		if probe.Reason == "provider_request_canceled" {
			return status.Error(codes.Canceled, "endpoint test canceled; the key was not saved")
		}
		if probe.Reason == "provider_transport_timeout" {
			return status.Error(codes.FailedPrecondition, "The endpoint test timed out; the key was not saved. Check the endpoint and try again.")
		}
		return status.Error(codes.FailedPrecondition, "The endpoint could not be reached or is temporarily unavailable; the key was not saved. Check the endpoint and try again.")
	}
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
	if rejected, ok := errors.AsType[*conversation.ModelRejectedError](err); ok {
		if rejected.Status != 0 {
			return status.Errorf(codes.FailedPrecondition, "the provider rejected this credential (HTTP %d): %s", rejected.Status, rejected.Hint)
		}
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

// ListModelCredentialModels never writes a key, provider response or URL to Dex.
func (s *server) ListModelCredentialModels(ctx context.Context, request *pb.ListModelCredentialModelsRequest) (*pb.ListModelCredentialModelsResponse, error) {
	caller, err := s.authorize(ctx, request.GetCaller(), request.GetProjectId(), request.GetProvider())
	if err != nil {
		return nil, err
	}
	provider := credentialProvider(request.GetProvider())
	key := strings.TrimSpace(request.GetApiKey())
	request.ApiKey = ""
	runtime := s.sessions.ModelCredentials.Compatible
	base, err := runtime.CanonicalEndpoint(request.GetBaseUrl())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "model endpoint is not permitted")
	}
	if key == "" {
		if request.ExpectedRevision == nil {
			return nil, status.Error(codes.InvalidArgument, "the observed credential revision is required")
		}
		info, err := s.sessions.Agent.ReadModelCredentialInfo(ctx, caller.FlowID, provider)
		if err != nil {
			return nil, credentialError(err)
		}
		if !info.Active() || info.Revision != request.GetExpectedRevision() || info.Endpoint.BaseURL != base {
			return nil, status.Error(codes.FailedPrecondition, "the current credential does not match this endpoint and revision")
		}
		envelope, err := s.sessions.Agent.ReadModelCredential(ctx, caller.FlowID, provider)
		if err != nil {
			return nil, credentialError(err)
		}
		if envelope == nil || envelope.Revision != info.Revision {
			return nil, status.Error(codes.FailedPrecondition, "the current credential is unavailable")
		}
		latest, readErr := s.sessions.Agent.ReadModelCredentialInfo(ctx, caller.FlowID, provider)
		if readErr != nil {
			return nil, credentialError(readErr)
		}
		if !latest.Active() || latest.Revision != info.Revision || latest.Endpoint != info.Endpoint {
			return nil, status.Error(codes.Aborted, "model credential revision changed")
		}
		key = envelope.Key
	} else {
		if len(key) < 4 || len(key) > 4096 {
			return nil, status.Error(codes.InvalidArgument, "the API key must contain 4 to 4096 printable ASCII characters")
		}
		for _, c := range key {
			if c < 0x21 || c > 0x7e {
				return nil, status.Error(codes.InvalidArgument, "the API key must contain printable ASCII characters without spaces")
			}
		}
	}
	models, result, err := runtime.DiscoverModels(ctx, provider.Catalog(), base, key)
	key = ""
	if err != nil {
		if errors.Is(err, modelendpoint.ErrDiscoveryBounds) {
			return nil, status.Error(codes.ResourceExhausted, "model discovery exceeded its limits")
		}
		return nil, status.Error(codes.Unavailable, "model discovery did not complete")
	}
	response := &pb.ListModelCredentialModelsResponse{Result: boundary.Enum[pb.ModelDiscoveryResult](result.String())}
	for _, model := range models {
		response.ModelIds = append(response.ModelIds, model.ID)
		response.Models = append(response.Models, &pb.ModelCredentialModel{ModelId: model.ID, ContextWindow: model.ContextWindow})
	}
	return response, nil
}
