// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { GetFlowCatalogRequest as _superverse_v2_GetFlowCatalogRequest, GetFlowCatalogRequest__Output as _superverse_v2_GetFlowCatalogRequest__Output } from '../../superverse/v2/GetFlowCatalogRequest';
import type { GetFlowCatalogResponse as _superverse_v2_GetFlowCatalogResponse, GetFlowCatalogResponse__Output as _superverse_v2_GetFlowCatalogResponse__Output } from '../../superverse/v2/GetFlowCatalogResponse';
import type { GetFlowDefinitionsRequest as _superverse_v2_GetFlowDefinitionsRequest, GetFlowDefinitionsRequest__Output as _superverse_v2_GetFlowDefinitionsRequest__Output } from '../../superverse/v2/GetFlowDefinitionsRequest';
import type { GetFlowDefinitionsResponse as _superverse_v2_GetFlowDefinitionsResponse, GetFlowDefinitionsResponse__Output as _superverse_v2_GetFlowDefinitionsResponse__Output } from '../../superverse/v2/GetFlowDefinitionsResponse';
import type { GetFlowDisplayRequest as _superverse_v2_GetFlowDisplayRequest, GetFlowDisplayRequest__Output as _superverse_v2_GetFlowDisplayRequest__Output } from '../../superverse/v2/GetFlowDisplayRequest';
import type { GetFlowDisplayResponse as _superverse_v2_GetFlowDisplayResponse, GetFlowDisplayResponse__Output as _superverse_v2_GetFlowDisplayResponse__Output } from '../../superverse/v2/GetFlowDisplayResponse';
import type { InvokeFlowActionRequest as _superverse_v2_InvokeFlowActionRequest, InvokeFlowActionRequest__Output as _superverse_v2_InvokeFlowActionRequest__Output } from '../../superverse/v2/InvokeFlowActionRequest';
import type { InvokeFlowActionResponse as _superverse_v2_InvokeFlowActionResponse, InvokeFlowActionResponse__Output as _superverse_v2_InvokeFlowActionResponse__Output } from '../../superverse/v2/InvokeFlowActionResponse';
import type { RecoverFlowStartRequest as _superverse_v2_RecoverFlowStartRequest, RecoverFlowStartRequest__Output as _superverse_v2_RecoverFlowStartRequest__Output } from '../../superverse/v2/RecoverFlowStartRequest';
import type { RecoverFlowStartResponse as _superverse_v2_RecoverFlowStartResponse, RecoverFlowStartResponse__Output as _superverse_v2_RecoverFlowStartResponse__Output } from '../../superverse/v2/RecoverFlowStartResponse';
import type { SearchFlowsRequest as _superverse_v2_SearchFlowsRequest, SearchFlowsRequest__Output as _superverse_v2_SearchFlowsRequest__Output } from '../../superverse/v2/SearchFlowsRequest';
import type { SearchFlowsResponse as _superverse_v2_SearchFlowsResponse, SearchFlowsResponse__Output as _superverse_v2_SearchFlowsResponse__Output } from '../../superverse/v2/SearchFlowsResponse';
import type { StartFlowRequest as _superverse_v2_StartFlowRequest, StartFlowRequest__Output as _superverse_v2_StartFlowRequest__Output } from '../../superverse/v2/StartFlowRequest';
import type { StartFlowResponse as _superverse_v2_StartFlowResponse, StartFlowResponse__Output as _superverse_v2_StartFlowResponse__Output } from '../../superverse/v2/StartFlowResponse';
import type { UpdateFlowDisplayFieldRequest as _superverse_v2_UpdateFlowDisplayFieldRequest, UpdateFlowDisplayFieldRequest__Output as _superverse_v2_UpdateFlowDisplayFieldRequest__Output } from '../../superverse/v2/UpdateFlowDisplayFieldRequest';
import type { UpdateFlowDisplayFieldResponse as _superverse_v2_UpdateFlowDisplayFieldResponse, UpdateFlowDisplayFieldResponse__Output as _superverse_v2_UpdateFlowDisplayFieldResponse__Output } from '../../superverse/v2/UpdateFlowDisplayFieldResponse';

export interface FlowRunServiceClient extends grpc.Client {
  GetFlowCatalog(argument: _superverse_v2_GetFlowCatalogRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowCatalogResponse__Output>): grpc.ClientUnaryCall;
  GetFlowCatalog(argument: _superverse_v2_GetFlowCatalogRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowCatalogResponse__Output>): grpc.ClientUnaryCall;
  GetFlowCatalog(argument: _superverse_v2_GetFlowCatalogRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowCatalogResponse__Output>): grpc.ClientUnaryCall;
  GetFlowCatalog(argument: _superverse_v2_GetFlowCatalogRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowCatalogResponse__Output>): grpc.ClientUnaryCall;
  getFlowCatalog(argument: _superverse_v2_GetFlowCatalogRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowCatalogResponse__Output>): grpc.ClientUnaryCall;
  getFlowCatalog(argument: _superverse_v2_GetFlowCatalogRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowCatalogResponse__Output>): grpc.ClientUnaryCall;
  getFlowCatalog(argument: _superverse_v2_GetFlowCatalogRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowCatalogResponse__Output>): grpc.ClientUnaryCall;
  getFlowCatalog(argument: _superverse_v2_GetFlowCatalogRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowCatalogResponse__Output>): grpc.ClientUnaryCall;

  GetFlowDefinitions(argument: _superverse_v2_GetFlowDefinitionsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  GetFlowDefinitions(argument: _superverse_v2_GetFlowDefinitionsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  GetFlowDefinitions(argument: _superverse_v2_GetFlowDefinitionsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  GetFlowDefinitions(argument: _superverse_v2_GetFlowDefinitionsRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  getFlowDefinitions(argument: _superverse_v2_GetFlowDefinitionsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  getFlowDefinitions(argument: _superverse_v2_GetFlowDefinitionsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  getFlowDefinitions(argument: _superverse_v2_GetFlowDefinitionsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  getFlowDefinitions(argument: _superverse_v2_GetFlowDefinitionsRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowDefinitionsResponse__Output>): grpc.ClientUnaryCall;

  GetFlowDisplay(argument: _superverse_v2_GetFlowDisplayRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDisplayResponse__Output>): grpc.ClientUnaryCall;
  GetFlowDisplay(argument: _superverse_v2_GetFlowDisplayRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowDisplayResponse__Output>): grpc.ClientUnaryCall;
  GetFlowDisplay(argument: _superverse_v2_GetFlowDisplayRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDisplayResponse__Output>): grpc.ClientUnaryCall;
  GetFlowDisplay(argument: _superverse_v2_GetFlowDisplayRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowDisplayResponse__Output>): grpc.ClientUnaryCall;
  getFlowDisplay(argument: _superverse_v2_GetFlowDisplayRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDisplayResponse__Output>): grpc.ClientUnaryCall;
  getFlowDisplay(argument: _superverse_v2_GetFlowDisplayRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowDisplayResponse__Output>): grpc.ClientUnaryCall;
  getFlowDisplay(argument: _superverse_v2_GetFlowDisplayRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowDisplayResponse__Output>): grpc.ClientUnaryCall;
  getFlowDisplay(argument: _superverse_v2_GetFlowDisplayRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowDisplayResponse__Output>): grpc.ClientUnaryCall;

  InvokeFlowAction(argument: _superverse_v2_InvokeFlowActionRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InvokeFlowActionResponse__Output>): grpc.ClientUnaryCall;
  InvokeFlowAction(argument: _superverse_v2_InvokeFlowActionRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_InvokeFlowActionResponse__Output>): grpc.ClientUnaryCall;
  InvokeFlowAction(argument: _superverse_v2_InvokeFlowActionRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InvokeFlowActionResponse__Output>): grpc.ClientUnaryCall;
  InvokeFlowAction(argument: _superverse_v2_InvokeFlowActionRequest, callback: grpc.requestCallback<_superverse_v2_InvokeFlowActionResponse__Output>): grpc.ClientUnaryCall;
  invokeFlowAction(argument: _superverse_v2_InvokeFlowActionRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InvokeFlowActionResponse__Output>): grpc.ClientUnaryCall;
  invokeFlowAction(argument: _superverse_v2_InvokeFlowActionRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_InvokeFlowActionResponse__Output>): grpc.ClientUnaryCall;
  invokeFlowAction(argument: _superverse_v2_InvokeFlowActionRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InvokeFlowActionResponse__Output>): grpc.ClientUnaryCall;
  invokeFlowAction(argument: _superverse_v2_InvokeFlowActionRequest, callback: grpc.requestCallback<_superverse_v2_InvokeFlowActionResponse__Output>): grpc.ClientUnaryCall;

  RecoverFlowStart(argument: _superverse_v2_RecoverFlowStartRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RecoverFlowStartResponse__Output>): grpc.ClientUnaryCall;
  RecoverFlowStart(argument: _superverse_v2_RecoverFlowStartRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RecoverFlowStartResponse__Output>): grpc.ClientUnaryCall;
  RecoverFlowStart(argument: _superverse_v2_RecoverFlowStartRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RecoverFlowStartResponse__Output>): grpc.ClientUnaryCall;
  RecoverFlowStart(argument: _superverse_v2_RecoverFlowStartRequest, callback: grpc.requestCallback<_superverse_v2_RecoverFlowStartResponse__Output>): grpc.ClientUnaryCall;
  recoverFlowStart(argument: _superverse_v2_RecoverFlowStartRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RecoverFlowStartResponse__Output>): grpc.ClientUnaryCall;
  recoverFlowStart(argument: _superverse_v2_RecoverFlowStartRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RecoverFlowStartResponse__Output>): grpc.ClientUnaryCall;
  recoverFlowStart(argument: _superverse_v2_RecoverFlowStartRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RecoverFlowStartResponse__Output>): grpc.ClientUnaryCall;
  recoverFlowStart(argument: _superverse_v2_RecoverFlowStartRequest, callback: grpc.requestCallback<_superverse_v2_RecoverFlowStartResponse__Output>): grpc.ClientUnaryCall;

  SearchFlows(argument: _superverse_v2_SearchFlowsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SearchFlowsResponse__Output>): grpc.ClientUnaryCall;
  SearchFlows(argument: _superverse_v2_SearchFlowsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SearchFlowsResponse__Output>): grpc.ClientUnaryCall;
  SearchFlows(argument: _superverse_v2_SearchFlowsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SearchFlowsResponse__Output>): grpc.ClientUnaryCall;
  SearchFlows(argument: _superverse_v2_SearchFlowsRequest, callback: grpc.requestCallback<_superverse_v2_SearchFlowsResponse__Output>): grpc.ClientUnaryCall;
  searchFlows(argument: _superverse_v2_SearchFlowsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SearchFlowsResponse__Output>): grpc.ClientUnaryCall;
  searchFlows(argument: _superverse_v2_SearchFlowsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SearchFlowsResponse__Output>): grpc.ClientUnaryCall;
  searchFlows(argument: _superverse_v2_SearchFlowsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SearchFlowsResponse__Output>): grpc.ClientUnaryCall;
  searchFlows(argument: _superverse_v2_SearchFlowsRequest, callback: grpc.requestCallback<_superverse_v2_SearchFlowsResponse__Output>): grpc.ClientUnaryCall;

  StartFlow(argument: _superverse_v2_StartFlowRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartFlowResponse__Output>): grpc.ClientUnaryCall;
  StartFlow(argument: _superverse_v2_StartFlowRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartFlowResponse__Output>): grpc.ClientUnaryCall;
  StartFlow(argument: _superverse_v2_StartFlowRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartFlowResponse__Output>): grpc.ClientUnaryCall;
  StartFlow(argument: _superverse_v2_StartFlowRequest, callback: grpc.requestCallback<_superverse_v2_StartFlowResponse__Output>): grpc.ClientUnaryCall;
  startFlow(argument: _superverse_v2_StartFlowRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartFlowResponse__Output>): grpc.ClientUnaryCall;
  startFlow(argument: _superverse_v2_StartFlowRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartFlowResponse__Output>): grpc.ClientUnaryCall;
  startFlow(argument: _superverse_v2_StartFlowRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartFlowResponse__Output>): grpc.ClientUnaryCall;
  startFlow(argument: _superverse_v2_StartFlowRequest, callback: grpc.requestCallback<_superverse_v2_StartFlowResponse__Output>): grpc.ClientUnaryCall;

  UpdateFlowDisplayField(argument: _superverse_v2_UpdateFlowDisplayFieldRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdateFlowDisplayFieldResponse__Output>): grpc.ClientUnaryCall;
  UpdateFlowDisplayField(argument: _superverse_v2_UpdateFlowDisplayFieldRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_UpdateFlowDisplayFieldResponse__Output>): grpc.ClientUnaryCall;
  UpdateFlowDisplayField(argument: _superverse_v2_UpdateFlowDisplayFieldRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdateFlowDisplayFieldResponse__Output>): grpc.ClientUnaryCall;
  UpdateFlowDisplayField(argument: _superverse_v2_UpdateFlowDisplayFieldRequest, callback: grpc.requestCallback<_superverse_v2_UpdateFlowDisplayFieldResponse__Output>): grpc.ClientUnaryCall;
  updateFlowDisplayField(argument: _superverse_v2_UpdateFlowDisplayFieldRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdateFlowDisplayFieldResponse__Output>): grpc.ClientUnaryCall;
  updateFlowDisplayField(argument: _superverse_v2_UpdateFlowDisplayFieldRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_UpdateFlowDisplayFieldResponse__Output>): grpc.ClientUnaryCall;
  updateFlowDisplayField(argument: _superverse_v2_UpdateFlowDisplayFieldRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdateFlowDisplayFieldResponse__Output>): grpc.ClientUnaryCall;
  updateFlowDisplayField(argument: _superverse_v2_UpdateFlowDisplayFieldRequest, callback: grpc.requestCallback<_superverse_v2_UpdateFlowDisplayFieldResponse__Output>): grpc.ClientUnaryCall;

}

export interface FlowRunServiceHandlers extends grpc.UntypedServiceImplementation {
  GetFlowCatalog: grpc.handleUnaryCall<_superverse_v2_GetFlowCatalogRequest__Output, _superverse_v2_GetFlowCatalogResponse>;

  GetFlowDefinitions: grpc.handleUnaryCall<_superverse_v2_GetFlowDefinitionsRequest__Output, _superverse_v2_GetFlowDefinitionsResponse>;

  GetFlowDisplay: grpc.handleUnaryCall<_superverse_v2_GetFlowDisplayRequest__Output, _superverse_v2_GetFlowDisplayResponse>;

  InvokeFlowAction: grpc.handleUnaryCall<_superverse_v2_InvokeFlowActionRequest__Output, _superverse_v2_InvokeFlowActionResponse>;

  RecoverFlowStart: grpc.handleUnaryCall<_superverse_v2_RecoverFlowStartRequest__Output, _superverse_v2_RecoverFlowStartResponse>;

  SearchFlows: grpc.handleUnaryCall<_superverse_v2_SearchFlowsRequest__Output, _superverse_v2_SearchFlowsResponse>;

  StartFlow: grpc.handleUnaryCall<_superverse_v2_StartFlowRequest__Output, _superverse_v2_StartFlowResponse>;

  UpdateFlowDisplayField: grpc.handleUnaryCall<_superverse_v2_UpdateFlowDisplayFieldRequest__Output, _superverse_v2_UpdateFlowDisplayFieldResponse>;

}

export interface FlowRunServiceDefinition extends grpc.ServiceDefinition {
  GetFlowCatalog: MethodDefinition<_superverse_v2_GetFlowCatalogRequest, _superverse_v2_GetFlowCatalogResponse, _superverse_v2_GetFlowCatalogRequest__Output, _superverse_v2_GetFlowCatalogResponse__Output>
  GetFlowDefinitions: MethodDefinition<_superverse_v2_GetFlowDefinitionsRequest, _superverse_v2_GetFlowDefinitionsResponse, _superverse_v2_GetFlowDefinitionsRequest__Output, _superverse_v2_GetFlowDefinitionsResponse__Output>
  GetFlowDisplay: MethodDefinition<_superverse_v2_GetFlowDisplayRequest, _superverse_v2_GetFlowDisplayResponse, _superverse_v2_GetFlowDisplayRequest__Output, _superverse_v2_GetFlowDisplayResponse__Output>
  InvokeFlowAction: MethodDefinition<_superverse_v2_InvokeFlowActionRequest, _superverse_v2_InvokeFlowActionResponse, _superverse_v2_InvokeFlowActionRequest__Output, _superverse_v2_InvokeFlowActionResponse__Output>
  RecoverFlowStart: MethodDefinition<_superverse_v2_RecoverFlowStartRequest, _superverse_v2_RecoverFlowStartResponse, _superverse_v2_RecoverFlowStartRequest__Output, _superverse_v2_RecoverFlowStartResponse__Output>
  SearchFlows: MethodDefinition<_superverse_v2_SearchFlowsRequest, _superverse_v2_SearchFlowsResponse, _superverse_v2_SearchFlowsRequest__Output, _superverse_v2_SearchFlowsResponse__Output>
  StartFlow: MethodDefinition<_superverse_v2_StartFlowRequest, _superverse_v2_StartFlowResponse, _superverse_v2_StartFlowRequest__Output, _superverse_v2_StartFlowResponse__Output>
  UpdateFlowDisplayField: MethodDefinition<_superverse_v2_UpdateFlowDisplayFieldRequest, _superverse_v2_UpdateFlowDisplayFieldResponse, _superverse_v2_UpdateFlowDisplayFieldRequest__Output, _superverse_v2_UpdateFlowDisplayFieldResponse__Output>
}
