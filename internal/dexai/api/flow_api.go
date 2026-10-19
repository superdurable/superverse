// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	pb "github.com/superdurable/superversev2/gen/superverse/v2"
	"github.com/superdurable/superversev2/internal/dexai/api/boundary"
	"github.com/superdurable/superversev2/internal/dexai/delivery/deployments"
	projectconfig "github.com/superdurable/superversev2/internal/dexai/projects/config"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi"
	"github.com/superdurable/superversev2/internal/dexai/studio/flowapi/fdg"
	"github.com/superdurable/superversev2/internal/dexai/studio/runs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// The Flow API is the API of the embedded Dex Flow UI: Runs, diagnostics,
// connectors and the application environment of one served application.
// Studio reaches its four services only through /dex/..., with CSRF and exact
// target binding. Every RPC fixes its operation, authenticates its caller
// freshly and answers with a FlowApiDocument, the JSON and HTTP status the Flow
// UI expects.

// flowAPIRequest is a Flow API request: the target context the page observed
// and the caller.
type flowAPIRequest interface {
	proto.Message
	GetContext() *pb.FlowApiContext
	GetCaller() *pb.CallerContext
}

// flowAPIDocument carries a Flow API document and its HTTP status, which is
// always a three-digit code.
func flowAPIDocument(document flowapi.Document) *pb.FlowApiDocument {
	if document.StatusCode < 100 || document.StatusCode > 999 {
		panic("Flow API document has an invalid HTTP status")
	}
	return &pb.FlowApiDocument{Json: document.JSON, HttpStatus: uint32(document.StatusCode), RetryAfterSeconds: document.RetryAfterSeconds}
}

// documentResponse returns response, or only err when serving its document
// failed. Every Flow API response only wraps its document, so building it
// whether or not serving failed is harmless.
func documentResponse[R proto.Message](response R, err error) (R, error) {
	if err != nil {
		var none R
		return none, err
	}
	return response, nil
}

// flowJSON is the JSON of an optional bytes field, or fallback when it is empty.
func flowJSON(value []byte, fallback string) json.RawMessage {
	if len(value) == 0 {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(value)
}

// flowRuns is what FlowRunService and FlowDiagnosticsService share: the trust
// boundary and Runs, which serves the fixed operations below on the target's
// project Dex.
type flowRuns struct {
	*boundary.Boundary
	runs *runs.Runs
}

// flowRunsRequestLimit bounds a Runs request, whose fields reach the project
// Dex and a Flow's input.
const flowRunsRequestLimit = 1 << 20

// serveRuns authenticates the caller of a Runs RPC and maps the result
// of run, which serves it for the caller's account. Runs rejects a missing or
// malformed target itself.
func (s flowRuns) serveRuns(ctx context.Context, in flowAPIRequest, run func(actor string) (flowapi.Document, error)) (*pb.FlowApiDocument, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	if proto.Size(in) > flowRunsRequestLimit {
		return nil, status.Error(codes.InvalidArgument, "Flow API request exceeds its limit")
	}
	document, err := run(account.AccountID)
	if err != nil {
		return nil, boundary.RPCStatus(err)
	}
	return flowAPIDocument(document), nil
}

// serveFlows serves one Flow API operation of a Runs RPC.
func (s flowRuns) serveFlows(ctx context.Context, in flowAPIRequest, operation runs.Operation, serve runs.Serve) (*pb.FlowApiDocument, error) {
	return s.serveRuns(ctx, in, func(actor string) (flowapi.Document, error) {
		return s.runs.Invoke(ctx, actor, in.GetContext(), operation, serve)
	})
}

// flowRunServer serves FlowRunService: the serving definitions, and listing,
// displaying, changing and starting Flows.
type flowRunServer struct {
	pb.UnimplementedFlowRunServiceServer
	flowRuns
}

func (s *flowRunServer) GetFlowDefinitions(ctx context.Context, in *pb.GetFlowDefinitionsRequest) (*pb.GetFlowDefinitionsResponse, error) {
	document, err := s.serveRuns(ctx, in, func(actor string) (flowapi.Document, error) {
		return s.runs.Definitions(ctx, actor, in.GetContext())
	})
	return documentResponse(&pb.GetFlowDefinitionsResponse{Document: document}, err)
}

func (s *flowRunServer) GetFlowCatalog(ctx context.Context, in *pb.GetFlowCatalogRequest) (*pb.GetFlowCatalogResponse, error) {
	document, err := s.serveFlows(ctx, in, runs.Read, func(_ context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.Catalog()
	})
	return documentResponse(&pb.GetFlowCatalogResponse{Document: document}, err)
}

func (s *flowRunServer) SearchFlows(ctx context.Context, in *pb.SearchFlowsRequest) (*pb.SearchFlowsResponse, error) {
	request := flowapi.SearchRequest{FlowType: in.GetFlowType(), WorkQueueOnly: in.GetWorkQueueOnly(), PageSize: in.GetPageSize(), NextPageToken: in.GetPageToken()}
	for _, filter := range in.GetFilters() {
		request.Filters = append(request.Filters, fdg.SearchFilter{Field: filter.GetField(), Operator: filter.GetOperator(), Values: flowJSON(filter.GetValuesJson(), "[]")})
	}
	document, err := s.serveFlows(ctx, in, runs.PinnedRead, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.Search(ctx, request)
	})
	return documentResponse(&pb.SearchFlowsResponse{Document: document}, err)
}

func (s *flowRunServer) GetFlowDisplay(ctx context.Context, in *pb.GetFlowDisplayRequest) (*pb.GetFlowDisplayResponse, error) {
	flow := flowapi.FlowRef{FlowType: in.GetFlowType(), FlowID: in.GetFlowId()}
	document, err := s.serveFlows(ctx, in, runs.PinnedRead, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.Display(ctx, flow)
	})
	return documentResponse(&pb.GetFlowDisplayResponse{Document: document}, err)
}

func (s *flowRunServer) UpdateFlowDisplayField(ctx context.Context, in *pb.UpdateFlowDisplayFieldRequest) (*pb.UpdateFlowDisplayFieldResponse, error) {
	request := flowapi.EditRequest{FlowRef: flowapi.FlowRef{FlowType: in.GetFlowType(), FlowID: in.GetFlowId()}, AttributeKey: in.GetAttributeKey(), Value: flowJSON(in.GetValueJson(), "null")}
	document, err := s.serveFlows(ctx, in, runs.PinnedWrite, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.EditDisplay(ctx, request)
	})
	return documentResponse(&pb.UpdateFlowDisplayFieldResponse{Document: document}, err)
}

func (s *flowRunServer) InvokeFlowAction(ctx context.Context, in *pb.InvokeFlowActionRequest) (*pb.InvokeFlowActionResponse, error) {
	request := flowapi.ActionRequest{FlowRef: flowapi.FlowRef{FlowType: in.GetFlowType(), FlowID: in.GetFlowId()}, RPCName: in.GetRpcName(), Input: flowJSON(in.GetInputJson(), "{}"), AttributeSnapshot: flowJSON(in.GetAttributeSnapshotJson(), "{}")}
	document, err := s.serveFlows(ctx, in, runs.PinnedWrite, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.InvokeAction(ctx, request)
	})
	return documentResponse(&pb.InvokeFlowActionResponse{Document: document}, err)
}

func (s *flowRunServer) StartFlow(ctx context.Context, in *pb.StartFlowRequest) (*pb.StartFlowResponse, error) {
	request := flowapi.StartRequest{FlowType: in.GetFlowType(), FlowID: in.GetFlowId(), RequestID: in.GetRequestId(), TargetRevision: in.GetTargetRevision(), Input: flowJSON(in.GetInputJson(), "{}")}
	document, err := s.serveFlows(ctx, in, runs.PinnedWrite, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.Start(ctx, request)
	})
	return documentResponse(&pb.StartFlowResponse{Document: document}, err)
}

// RecoverFlowStart queries an uncertain StartFlow by its original identity.
func (s *flowRunServer) RecoverFlowStart(ctx context.Context, in *pb.RecoverFlowStartRequest) (*pb.RecoverFlowStartResponse, error) {
	request := flowapi.StartRequest{FlowType: in.GetFlowType(), FlowID: in.GetFlowId(), RequestID: in.GetRequestId(), TargetRevision: in.GetTargetRevision(), Input: flowJSON(in.GetInputJson(), "{}")}
	document, err := s.serveFlows(ctx, in, runs.PinnedRead, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.RecoverStart(ctx, request)
	})
	return documentResponse(&pb.RecoverFlowStartResponse{Document: document}, err)
}

// flowDiagnosticsServer serves FlowDiagnosticsService: engine state, history,
// Streams and blobs, and the diagnostic interventions. Run IDs select engine
// runs only, never a business resource.
type flowDiagnosticsServer struct {
	pb.UnimplementedFlowDiagnosticsServiceServer
	flowRuns
}

func (s *flowDiagnosticsServer) GetFlowSummary(ctx context.Context, in *pb.GetFlowSummaryRequest) (*pb.GetFlowSummaryResponse, error) {
	run := flowapi.FlowRun{FlowID: in.GetFlowId(), RunID: in.GetRunId()}
	document, err := s.serveFlows(ctx, in, runs.Read, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.Summary(ctx, run)
	})
	return documentResponse(&pb.GetFlowSummaryResponse{Document: document}, err)
}

func (s *flowDiagnosticsServer) GetFlowState(ctx context.Context, in *pb.GetFlowStateRequest) (*pb.GetFlowStateResponse, error) {
	run := flowapi.FlowRun{FlowID: in.GetFlowId(), RunID: in.GetRunId()}
	document, err := s.serveFlows(ctx, in, runs.Read, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.State(ctx, run)
	})
	return documentResponse(&pb.GetFlowStateResponse{Document: document}, err)
}

func (s *flowDiagnosticsServer) GetFlowHistory(ctx context.Context, in *pb.GetFlowHistoryRequest) (*pb.GetFlowHistoryResponse, error) {
	request := flowapi.HistoryRequest{FlowRun: flowapi.FlowRun{FlowID: in.GetFlowId(), RunID: in.GetRunId()}, AfterEventID: in.GetAfterEventId(), PageSize: in.GetPageSize(), NextPageToken: in.GetPageToken()}
	document, err := s.serveFlows(ctx, in, runs.Read, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.History(ctx, request)
	})
	return documentResponse(&pb.GetFlowHistoryResponse{Document: document}, err)
}

// WaitFlowHistory long-polls for history after after_event_id.
func (s *flowDiagnosticsServer) WaitFlowHistory(ctx context.Context, in *pb.WaitFlowHistoryRequest) (*pb.WaitFlowHistoryResponse, error) {
	request := flowapi.HistoryRequest{FlowRun: flowapi.FlowRun{FlowID: in.GetFlowId(), RunID: in.GetRunId()}, AfterEventID: in.GetAfterEventId()}
	document, err := s.serveFlows(ctx, in, runs.Read, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.WaitHistory(ctx, request)
	})
	return documentResponse(&pb.WaitFlowHistoryResponse{Document: document}, err)
}

func (s *flowDiagnosticsServer) ReadFlowStream(ctx context.Context, in *pb.ReadFlowStreamRequest) (*pb.ReadFlowStreamResponse, error) {
	request := flowapi.StreamRequest{FlowID: in.GetFlowId(), FlowType: in.GetFlowType(), StreamName: in.GetStreamName(), ResumeToken: in.GetResumeToken()}
	document, err := s.serveFlows(ctx, in, runs.Read, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.ReadStream(ctx, request)
	})
	return documentResponse(&pb.ReadFlowStreamResponse{Document: document}, err)
}

func (s *flowDiagnosticsServer) LoadFlowBlobs(ctx context.Context, in *pb.LoadFlowBlobsRequest) (*pb.LoadFlowBlobsResponse, error) {
	request := flowapi.BlobsRequest{FlowID: in.GetFlowId()}
	for _, value := range in.GetValues() {
		request.Values = append(request.Values, flowapi.BlobReference{ID: value.GetId(), Kind: value.GetKind()})
	}
	document, err := s.serveFlows(ctx, in, runs.Read, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.LoadBlobs(ctx, request)
	})
	return documentResponse(&pb.LoadFlowBlobsResponse{Document: document}, err)
}

func (s *flowDiagnosticsServer) StopFlow(ctx context.Context, in *pb.StopFlowRequest) (*pb.StopFlowResponse, error) {
	request := flowapi.StopRequest{FlowRun: flowapi.FlowRun{FlowID: in.GetFlowId(), RunID: in.GetRunId()}, StopType: in.GetStopType(), Reason: in.GetReason()}
	document, err := s.serveFlows(ctx, in, runs.Write, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.Stop(ctx, request)
	})
	return documentResponse(&pb.StopFlowResponse{Document: document}, err)
}

func (s *flowDiagnosticsServer) DeleteFlowChannelMessage(ctx context.Context, in *pb.DeleteFlowChannelMessageRequest) (*pb.DeleteFlowChannelMessageResponse, error) {
	message := flowapi.ChannelMessageRef{FlowRun: flowapi.FlowRun{FlowID: in.GetFlowId(), RunID: in.GetRunId()}, ChannelName: in.GetChannelName(), MessageID: in.GetMessageId()}
	document, err := s.serveFlows(ctx, in, runs.Write, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.DeleteChannelMessage(ctx, message)
	})
	return documentResponse(&pb.DeleteFlowChannelMessageResponse{Document: document}, err)
}

// TimeTravelFlow is the Dex ResetFlow call.
func (s *flowDiagnosticsServer) TimeTravelFlow(ctx context.Context, in *pb.TimeTravelFlowRequest) (*pb.TimeTravelFlowResponse, error) {
	request := flowapi.TimeTravelRequest{
		FlowRun: flowapi.FlowRun{FlowID: in.GetFlowId(), RunID: in.GetRunId()}, TimeTravelType: in.GetTimeTravelType(), Reason: in.GetReason(),
		StepType: in.GetStepType(), StepExecutionID: in.GetStepExecutionId(), HistoryEventTime: in.GetHistoryEventTime(), StepMethod: in.GetStepMethod(),
	}
	document, err := s.serveFlows(ctx, in, runs.Write, func(ctx context.Context, flows *flowapi.FlowAPI) flowapi.Document {
		return flows.TimeTravel(ctx, request)
	})
	return documentResponse(&pb.TimeTravelFlowResponse{Document: document}, err)
}

// flowConfiguration is what ConnectorService and ApplicationEnvironmentService
// share: the trust boundary and the configuration of the target's scope, which
// serves each operation on a fixed configuration host route.
type flowConfiguration struct {
	*boundary.Boundary
	configuration *projectconfig.Configuration
}

// invokeConfiguration authenticates the caller and invokes one fixed
// configuration host route for the caller's account. The configuration
// rejects a missing or malformed target itself.
func (s flowConfiguration) invokeConfiguration(ctx context.Context, in flowAPIRequest, access projectconfig.Access, method, path string, input any) (*pb.FlowApiDocument, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	var payload []byte
	if input != nil {
		payload, err = json.Marshal(input)
		if err != nil || len(payload) > 2<<20 {
			return nil, status.Error(codes.InvalidArgument, "Flow API input JSON is invalid or exceeds the limit")
		}
	}
	result, err := s.configuration.InvokeForAccount(ctx, account.AccountID, in.GetContext(), access, method, path, payload)
	if err != nil {
		return nil, boundary.ConfigurationRPCStatus(err)
	}
	return flowAPIDocument(result), nil
}

// configurationPath escapes the path segments of a configuration resource.
func configurationPath(values ...string) (string, error) {
	encoded := make([]string, len(values))
	for i, value := range values {
		if value == "" || value == "." || value == ".." || len(value) > 256 || strings.ContainsAny(value, "/\\\r\n") {
			return "", status.Error(codes.InvalidArgument, "invalid configuration resource identity")
		}
		encoded[i] = url.PathEscape(value)
	}
	return strings.Join(encoded, "/"), nil
}

// connectorUISessionPath accepts the signed, opaque nonce as one URL segment.
// Its payload can exceed the resource identity limit; the configuration host
// verifies its signature, purpose, expiry and scope.
var connectorUISessionNonce = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)

func connectorUISessionPath(nonce string) (string, error) {
	if len(nonce) > 2048 || !connectorUISessionNonce.MatchString(nonce) {
		return "", status.Error(codes.InvalidArgument, "invalid connector UI session nonce")
	}
	return url.PathEscape(nonce), nil
}

// connectorServer serves ConnectorService: the connection names, their
// TeamSpace connection selections, connector UI sessions, trigger bindings and
// operation configurations of the target's configuration scope.
type connectorServer struct {
	pb.UnimplementedConnectorServiceServer
	flowConfiguration
	publishing *deployments.PublishingFlowService
}

func (s *connectorServer) ListConnections(ctx context.Context, in *pb.ListConnectionsRequest) (*pb.ListConnectionsResponse, error) {
	document, err := s.invokeConfiguration(ctx, in, projectconfig.ReadAccess, http.MethodGet, "/api/v2/connector-connections", nil)
	return documentResponse(&pb.ListConnectionsResponse{Document: document}, err)
}

// configurationRequest is the body of a configuration host request that
// saves a connector configuration: the configuration document, an empty
// object when the request carries none, or on Live's next-publish layer
// following Preview, which carries none.
func configurationRequest(configurationJSON []byte, followPreview bool) map[string]any {
	if followPreview {
		return map[string]any{"followPreview": true}
	}
	return map[string]any{"configuration": flowJSON(configurationJSON, "{}")}
}

func (s *connectorServer) SelectTeamSpaceConnection(ctx context.Context, in *pb.SelectTeamSpaceConnectionRequest) (*pb.SelectTeamSpaceConnectionResponse, error) {
	path, err := configurationPath(in.GetConnectorId(), in.GetConnectionName())
	if err != nil {
		return nil, err
	}
	document, err := s.invokeConfiguration(ctx, in, projectconfig.WriteAccess, http.MethodPut, "/api/v2/connector-selections/"+path,
		map[string]any{"teamSpaceConnectionId": in.GetTeamSpaceConnectionId(), "followPreview": in.GetFollowPreview()})
	return documentResponse(&pb.SelectTeamSpaceConnectionResponse{Document: document}, err)
}

// ReleaseUnusedConnections keeps only the Live usages of the connections the
// current Live version selects and answers the Live target's connections.
func (s *connectorServer) ReleaseUnusedConnections(ctx context.Context, in *pb.ReleaseUnusedConnectionsRequest) (*pb.ReleaseUnusedConnectionsResponse, error) {
	account, err := s.Authenticate(ctx, in.GetCaller())
	if err != nil {
		return nil, err
	}
	target := in.GetContext().GetTarget()
	if target.GetScope() != pb.ProjectScope_PROJECT_SCOPE_LIVE {
		return nil, status.Error(codes.InvalidArgument, "only the Live scope releases unused connections")
	}
	if err = s.publishing.ReleaseUnusedConnections(ctx, account.AccountID, target.GetProjectId()); err != nil {
		return nil, boundary.ConnectionRPCStatus(err)
	}
	document, err := s.invokeConfiguration(ctx, in, projectconfig.ReadAccess, http.MethodGet, "/api/v2/connector-connections", nil)
	return documentResponse(&pb.ReleaseUnusedConnectionsResponse{Document: document}, err)
}

func (s *connectorServer) StartConnectorUiSession(ctx context.Context, in *pb.StartConnectorUiSessionRequest) (*pb.StartConnectorUiSessionResponse, error) {
	document, err := s.invokeConfiguration(ctx, in, projectconfig.WriteAccess, http.MethodPost, "/api/v2/connector-native-sessions", map[string]any{"connectorId": in.GetConnectorId(), "connectionName": in.GetConnectionName()})
	return documentResponse(&pb.StartConnectorUiSessionResponse{Document: document}, err)
}

func (s *connectorServer) InvokeProviderCommand(ctx context.Context, in *pb.InvokeProviderCommandRequest) (*pb.InvokeProviderCommandResponse, error) {
	path, err := connectorUISessionPath(in.GetSessionNonce())
	if err != nil {
		return nil, err
	}
	command, err := configurationPath(in.GetCommandId())
	if err != nil {
		return nil, err
	}
	document, err := s.invokeConfiguration(ctx, in, projectconfig.WriteAccess, http.MethodPost, "/api/v2/connector-ui-sessions/"+path+"/commands/"+command, map[string]any{"parameters": in.GetParameters()})
	return documentResponse(&pb.InvokeProviderCommandResponse{Document: document}, err)
}

func (s *connectorServer) SaveTriggerBinding(ctx context.Context, in *pb.SaveTriggerBindingRequest) (*pb.SaveTriggerBindingResponse, error) {
	path, err := configurationPath(in.GetConnectorId(), in.GetConnectionName(), in.GetTriggerName(), in.GetBindingName())
	if err != nil {
		return nil, err
	}
	document, err := s.invokeConfiguration(ctx, in, projectconfig.WriteAccess, http.MethodPut, "/api/v2/connector-trigger-bindings/"+path,
		configurationRequest(in.GetConfigurationJson(), in.GetFollowPreview()))
	return documentResponse(&pb.SaveTriggerBindingResponse{Document: document}, err)
}

func (s *connectorServer) SaveOperationConfiguration(ctx context.Context, in *pb.SaveOperationConfigurationRequest) (*pb.SaveOperationConfigurationResponse, error) {
	path, err := configurationPath(in.GetConnectorId(), in.GetConnectionName(), in.GetOperationId(), in.GetFlowType(), in.GetStepType())
	if err != nil {
		return nil, err
	}
	document, err := s.invokeConfiguration(ctx, in, projectconfig.WriteAccess, http.MethodPut, "/api/v2/connector-use-configurations/"+path,
		configurationRequest(in.GetConfigurationJson(), in.GetFollowPreview()))
	return documentResponse(&pb.SaveOperationConfigurationResponse{Document: document}, err)
}

// applicationEnvironmentServer serves ApplicationEnvironmentService: the
// application environment values and secrets of the target's configuration
// scope.
type applicationEnvironmentServer struct {
	pb.UnimplementedApplicationEnvironmentServiceServer
	flowConfiguration
}

func (s *applicationEnvironmentServer) GetApplicationEnvironment(ctx context.Context, in *pb.GetApplicationEnvironmentRequest) (*pb.GetApplicationEnvironmentResponse, error) {
	document, err := s.invokeConfiguration(ctx, in, projectconfig.ReadAccess, http.MethodGet, "/api/v2/application-environment", nil)
	return documentResponse(&pb.GetApplicationEnvironmentResponse{Document: document}, err)
}

func (s *applicationEnvironmentServer) SaveApplicationEnvironment(ctx context.Context, in *pb.SaveApplicationEnvironmentRequest) (*pb.SaveApplicationEnvironmentResponse, error) {
	document, err := s.invokeConfiguration(ctx, in, projectconfig.WriteAccess, http.MethodPut, "/api/v2/application-environment",
		map[string]any{"values": in.GetValues(), "secrets": in.GetSecrets(), "remove": in.GetRemove(), "followPreview": in.GetFollowPreview()})
	return documentResponse(&pb.SaveApplicationEnvironmentResponse{Document: document}, err)
}
