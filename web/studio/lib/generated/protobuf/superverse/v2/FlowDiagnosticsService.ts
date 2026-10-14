// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { DeleteFlowChannelMessageRequest as _superverse_v2_DeleteFlowChannelMessageRequest, DeleteFlowChannelMessageRequest__Output as _superverse_v2_DeleteFlowChannelMessageRequest__Output } from '../../superverse/v2/DeleteFlowChannelMessageRequest';
import type { DeleteFlowChannelMessageResponse as _superverse_v2_DeleteFlowChannelMessageResponse, DeleteFlowChannelMessageResponse__Output as _superverse_v2_DeleteFlowChannelMessageResponse__Output } from '../../superverse/v2/DeleteFlowChannelMessageResponse';
import type { GetFlowHistoryRequest as _superverse_v2_GetFlowHistoryRequest, GetFlowHistoryRequest__Output as _superverse_v2_GetFlowHistoryRequest__Output } from '../../superverse/v2/GetFlowHistoryRequest';
import type { GetFlowHistoryResponse as _superverse_v2_GetFlowHistoryResponse, GetFlowHistoryResponse__Output as _superverse_v2_GetFlowHistoryResponse__Output } from '../../superverse/v2/GetFlowHistoryResponse';
import type { GetFlowStateRequest as _superverse_v2_GetFlowStateRequest, GetFlowStateRequest__Output as _superverse_v2_GetFlowStateRequest__Output } from '../../superverse/v2/GetFlowStateRequest';
import type { GetFlowStateResponse as _superverse_v2_GetFlowStateResponse, GetFlowStateResponse__Output as _superverse_v2_GetFlowStateResponse__Output } from '../../superverse/v2/GetFlowStateResponse';
import type { GetFlowSummaryRequest as _superverse_v2_GetFlowSummaryRequest, GetFlowSummaryRequest__Output as _superverse_v2_GetFlowSummaryRequest__Output } from '../../superverse/v2/GetFlowSummaryRequest';
import type { GetFlowSummaryResponse as _superverse_v2_GetFlowSummaryResponse, GetFlowSummaryResponse__Output as _superverse_v2_GetFlowSummaryResponse__Output } from '../../superverse/v2/GetFlowSummaryResponse';
import type { LoadFlowBlobsRequest as _superverse_v2_LoadFlowBlobsRequest, LoadFlowBlobsRequest__Output as _superverse_v2_LoadFlowBlobsRequest__Output } from '../../superverse/v2/LoadFlowBlobsRequest';
import type { LoadFlowBlobsResponse as _superverse_v2_LoadFlowBlobsResponse, LoadFlowBlobsResponse__Output as _superverse_v2_LoadFlowBlobsResponse__Output } from '../../superverse/v2/LoadFlowBlobsResponse';
import type { ReadFlowStreamRequest as _superverse_v2_ReadFlowStreamRequest, ReadFlowStreamRequest__Output as _superverse_v2_ReadFlowStreamRequest__Output } from '../../superverse/v2/ReadFlowStreamRequest';
import type { ReadFlowStreamResponse as _superverse_v2_ReadFlowStreamResponse, ReadFlowStreamResponse__Output as _superverse_v2_ReadFlowStreamResponse__Output } from '../../superverse/v2/ReadFlowStreamResponse';
import type { StopFlowRequest as _superverse_v2_StopFlowRequest, StopFlowRequest__Output as _superverse_v2_StopFlowRequest__Output } from '../../superverse/v2/StopFlowRequest';
import type { StopFlowResponse as _superverse_v2_StopFlowResponse, StopFlowResponse__Output as _superverse_v2_StopFlowResponse__Output } from '../../superverse/v2/StopFlowResponse';
import type { TimeTravelFlowRequest as _superverse_v2_TimeTravelFlowRequest, TimeTravelFlowRequest__Output as _superverse_v2_TimeTravelFlowRequest__Output } from '../../superverse/v2/TimeTravelFlowRequest';
import type { TimeTravelFlowResponse as _superverse_v2_TimeTravelFlowResponse, TimeTravelFlowResponse__Output as _superverse_v2_TimeTravelFlowResponse__Output } from '../../superverse/v2/TimeTravelFlowResponse';
import type { WaitFlowHistoryRequest as _superverse_v2_WaitFlowHistoryRequest, WaitFlowHistoryRequest__Output as _superverse_v2_WaitFlowHistoryRequest__Output } from '../../superverse/v2/WaitFlowHistoryRequest';
import type { WaitFlowHistoryResponse as _superverse_v2_WaitFlowHistoryResponse, WaitFlowHistoryResponse__Output as _superverse_v2_WaitFlowHistoryResponse__Output } from '../../superverse/v2/WaitFlowHistoryResponse';

export interface FlowDiagnosticsServiceClient extends grpc.Client {
  DeleteFlowChannelMessage(argument: _superverse_v2_DeleteFlowChannelMessageRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteFlowChannelMessageResponse__Output>): grpc.ClientUnaryCall;
  DeleteFlowChannelMessage(argument: _superverse_v2_DeleteFlowChannelMessageRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteFlowChannelMessageResponse__Output>): grpc.ClientUnaryCall;
  DeleteFlowChannelMessage(argument: _superverse_v2_DeleteFlowChannelMessageRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteFlowChannelMessageResponse__Output>): grpc.ClientUnaryCall;
  DeleteFlowChannelMessage(argument: _superverse_v2_DeleteFlowChannelMessageRequest, callback: grpc.requestCallback<_superverse_v2_DeleteFlowChannelMessageResponse__Output>): grpc.ClientUnaryCall;
  deleteFlowChannelMessage(argument: _superverse_v2_DeleteFlowChannelMessageRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteFlowChannelMessageResponse__Output>): grpc.ClientUnaryCall;
  deleteFlowChannelMessage(argument: _superverse_v2_DeleteFlowChannelMessageRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteFlowChannelMessageResponse__Output>): grpc.ClientUnaryCall;
  deleteFlowChannelMessage(argument: _superverse_v2_DeleteFlowChannelMessageRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteFlowChannelMessageResponse__Output>): grpc.ClientUnaryCall;
  deleteFlowChannelMessage(argument: _superverse_v2_DeleteFlowChannelMessageRequest, callback: grpc.requestCallback<_superverse_v2_DeleteFlowChannelMessageResponse__Output>): grpc.ClientUnaryCall;

  GetFlowHistory(argument: _superverse_v2_GetFlowHistoryRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  GetFlowHistory(argument: _superverse_v2_GetFlowHistoryRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  GetFlowHistory(argument: _superverse_v2_GetFlowHistoryRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  GetFlowHistory(argument: _superverse_v2_GetFlowHistoryRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  getFlowHistory(argument: _superverse_v2_GetFlowHistoryRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  getFlowHistory(argument: _superverse_v2_GetFlowHistoryRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  getFlowHistory(argument: _superverse_v2_GetFlowHistoryRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  getFlowHistory(argument: _superverse_v2_GetFlowHistoryRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowHistoryResponse__Output>): grpc.ClientUnaryCall;

  GetFlowState(argument: _superverse_v2_GetFlowStateRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowStateResponse__Output>): grpc.ClientUnaryCall;
  GetFlowState(argument: _superverse_v2_GetFlowStateRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowStateResponse__Output>): grpc.ClientUnaryCall;
  GetFlowState(argument: _superverse_v2_GetFlowStateRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowStateResponse__Output>): grpc.ClientUnaryCall;
  GetFlowState(argument: _superverse_v2_GetFlowStateRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowStateResponse__Output>): grpc.ClientUnaryCall;
  getFlowState(argument: _superverse_v2_GetFlowStateRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowStateResponse__Output>): grpc.ClientUnaryCall;
  getFlowState(argument: _superverse_v2_GetFlowStateRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowStateResponse__Output>): grpc.ClientUnaryCall;
  getFlowState(argument: _superverse_v2_GetFlowStateRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowStateResponse__Output>): grpc.ClientUnaryCall;
  getFlowState(argument: _superverse_v2_GetFlowStateRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowStateResponse__Output>): grpc.ClientUnaryCall;

  GetFlowSummary(argument: _superverse_v2_GetFlowSummaryRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowSummaryResponse__Output>): grpc.ClientUnaryCall;
  GetFlowSummary(argument: _superverse_v2_GetFlowSummaryRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowSummaryResponse__Output>): grpc.ClientUnaryCall;
  GetFlowSummary(argument: _superverse_v2_GetFlowSummaryRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowSummaryResponse__Output>): grpc.ClientUnaryCall;
  GetFlowSummary(argument: _superverse_v2_GetFlowSummaryRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowSummaryResponse__Output>): grpc.ClientUnaryCall;
  getFlowSummary(argument: _superverse_v2_GetFlowSummaryRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowSummaryResponse__Output>): grpc.ClientUnaryCall;
  getFlowSummary(argument: _superverse_v2_GetFlowSummaryRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetFlowSummaryResponse__Output>): grpc.ClientUnaryCall;
  getFlowSummary(argument: _superverse_v2_GetFlowSummaryRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetFlowSummaryResponse__Output>): grpc.ClientUnaryCall;
  getFlowSummary(argument: _superverse_v2_GetFlowSummaryRequest, callback: grpc.requestCallback<_superverse_v2_GetFlowSummaryResponse__Output>): grpc.ClientUnaryCall;

  LoadFlowBlobs(argument: _superverse_v2_LoadFlowBlobsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LoadFlowBlobsResponse__Output>): grpc.ClientUnaryCall;
  LoadFlowBlobs(argument: _superverse_v2_LoadFlowBlobsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_LoadFlowBlobsResponse__Output>): grpc.ClientUnaryCall;
  LoadFlowBlobs(argument: _superverse_v2_LoadFlowBlobsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LoadFlowBlobsResponse__Output>): grpc.ClientUnaryCall;
  LoadFlowBlobs(argument: _superverse_v2_LoadFlowBlobsRequest, callback: grpc.requestCallback<_superverse_v2_LoadFlowBlobsResponse__Output>): grpc.ClientUnaryCall;
  loadFlowBlobs(argument: _superverse_v2_LoadFlowBlobsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LoadFlowBlobsResponse__Output>): grpc.ClientUnaryCall;
  loadFlowBlobs(argument: _superverse_v2_LoadFlowBlobsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_LoadFlowBlobsResponse__Output>): grpc.ClientUnaryCall;
  loadFlowBlobs(argument: _superverse_v2_LoadFlowBlobsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LoadFlowBlobsResponse__Output>): grpc.ClientUnaryCall;
  loadFlowBlobs(argument: _superverse_v2_LoadFlowBlobsRequest, callback: grpc.requestCallback<_superverse_v2_LoadFlowBlobsResponse__Output>): grpc.ClientUnaryCall;

  ReadFlowStream(argument: _superverse_v2_ReadFlowStreamRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ReadFlowStreamResponse__Output>): grpc.ClientUnaryCall;
  ReadFlowStream(argument: _superverse_v2_ReadFlowStreamRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ReadFlowStreamResponse__Output>): grpc.ClientUnaryCall;
  ReadFlowStream(argument: _superverse_v2_ReadFlowStreamRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ReadFlowStreamResponse__Output>): grpc.ClientUnaryCall;
  ReadFlowStream(argument: _superverse_v2_ReadFlowStreamRequest, callback: grpc.requestCallback<_superverse_v2_ReadFlowStreamResponse__Output>): grpc.ClientUnaryCall;
  readFlowStream(argument: _superverse_v2_ReadFlowStreamRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ReadFlowStreamResponse__Output>): grpc.ClientUnaryCall;
  readFlowStream(argument: _superverse_v2_ReadFlowStreamRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ReadFlowStreamResponse__Output>): grpc.ClientUnaryCall;
  readFlowStream(argument: _superverse_v2_ReadFlowStreamRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ReadFlowStreamResponse__Output>): grpc.ClientUnaryCall;
  readFlowStream(argument: _superverse_v2_ReadFlowStreamRequest, callback: grpc.requestCallback<_superverse_v2_ReadFlowStreamResponse__Output>): grpc.ClientUnaryCall;

  StopFlow(argument: _superverse_v2_StopFlowRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StopFlowResponse__Output>): grpc.ClientUnaryCall;
  StopFlow(argument: _superverse_v2_StopFlowRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StopFlowResponse__Output>): grpc.ClientUnaryCall;
  StopFlow(argument: _superverse_v2_StopFlowRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StopFlowResponse__Output>): grpc.ClientUnaryCall;
  StopFlow(argument: _superverse_v2_StopFlowRequest, callback: grpc.requestCallback<_superverse_v2_StopFlowResponse__Output>): grpc.ClientUnaryCall;
  stopFlow(argument: _superverse_v2_StopFlowRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StopFlowResponse__Output>): grpc.ClientUnaryCall;
  stopFlow(argument: _superverse_v2_StopFlowRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StopFlowResponse__Output>): grpc.ClientUnaryCall;
  stopFlow(argument: _superverse_v2_StopFlowRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StopFlowResponse__Output>): grpc.ClientUnaryCall;
  stopFlow(argument: _superverse_v2_StopFlowRequest, callback: grpc.requestCallback<_superverse_v2_StopFlowResponse__Output>): grpc.ClientUnaryCall;

  TimeTravelFlow(argument: _superverse_v2_TimeTravelFlowRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_TimeTravelFlowResponse__Output>): grpc.ClientUnaryCall;
  TimeTravelFlow(argument: _superverse_v2_TimeTravelFlowRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_TimeTravelFlowResponse__Output>): grpc.ClientUnaryCall;
  TimeTravelFlow(argument: _superverse_v2_TimeTravelFlowRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_TimeTravelFlowResponse__Output>): grpc.ClientUnaryCall;
  TimeTravelFlow(argument: _superverse_v2_TimeTravelFlowRequest, callback: grpc.requestCallback<_superverse_v2_TimeTravelFlowResponse__Output>): grpc.ClientUnaryCall;
  timeTravelFlow(argument: _superverse_v2_TimeTravelFlowRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_TimeTravelFlowResponse__Output>): grpc.ClientUnaryCall;
  timeTravelFlow(argument: _superverse_v2_TimeTravelFlowRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_TimeTravelFlowResponse__Output>): grpc.ClientUnaryCall;
  timeTravelFlow(argument: _superverse_v2_TimeTravelFlowRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_TimeTravelFlowResponse__Output>): grpc.ClientUnaryCall;
  timeTravelFlow(argument: _superverse_v2_TimeTravelFlowRequest, callback: grpc.requestCallback<_superverse_v2_TimeTravelFlowResponse__Output>): grpc.ClientUnaryCall;

  WaitFlowHistory(argument: _superverse_v2_WaitFlowHistoryRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_WaitFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  WaitFlowHistory(argument: _superverse_v2_WaitFlowHistoryRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_WaitFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  WaitFlowHistory(argument: _superverse_v2_WaitFlowHistoryRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_WaitFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  WaitFlowHistory(argument: _superverse_v2_WaitFlowHistoryRequest, callback: grpc.requestCallback<_superverse_v2_WaitFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  waitFlowHistory(argument: _superverse_v2_WaitFlowHistoryRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_WaitFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  waitFlowHistory(argument: _superverse_v2_WaitFlowHistoryRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_WaitFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  waitFlowHistory(argument: _superverse_v2_WaitFlowHistoryRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_WaitFlowHistoryResponse__Output>): grpc.ClientUnaryCall;
  waitFlowHistory(argument: _superverse_v2_WaitFlowHistoryRequest, callback: grpc.requestCallback<_superverse_v2_WaitFlowHistoryResponse__Output>): grpc.ClientUnaryCall;

}

export interface FlowDiagnosticsServiceHandlers extends grpc.UntypedServiceImplementation {
  DeleteFlowChannelMessage: grpc.handleUnaryCall<_superverse_v2_DeleteFlowChannelMessageRequest__Output, _superverse_v2_DeleteFlowChannelMessageResponse>;

  GetFlowHistory: grpc.handleUnaryCall<_superverse_v2_GetFlowHistoryRequest__Output, _superverse_v2_GetFlowHistoryResponse>;

  GetFlowState: grpc.handleUnaryCall<_superverse_v2_GetFlowStateRequest__Output, _superverse_v2_GetFlowStateResponse>;

  GetFlowSummary: grpc.handleUnaryCall<_superverse_v2_GetFlowSummaryRequest__Output, _superverse_v2_GetFlowSummaryResponse>;

  LoadFlowBlobs: grpc.handleUnaryCall<_superverse_v2_LoadFlowBlobsRequest__Output, _superverse_v2_LoadFlowBlobsResponse>;

  ReadFlowStream: grpc.handleUnaryCall<_superverse_v2_ReadFlowStreamRequest__Output, _superverse_v2_ReadFlowStreamResponse>;

  StopFlow: grpc.handleUnaryCall<_superverse_v2_StopFlowRequest__Output, _superverse_v2_StopFlowResponse>;

  TimeTravelFlow: grpc.handleUnaryCall<_superverse_v2_TimeTravelFlowRequest__Output, _superverse_v2_TimeTravelFlowResponse>;

  WaitFlowHistory: grpc.handleUnaryCall<_superverse_v2_WaitFlowHistoryRequest__Output, _superverse_v2_WaitFlowHistoryResponse>;

}

export interface FlowDiagnosticsServiceDefinition extends grpc.ServiceDefinition {
  DeleteFlowChannelMessage: MethodDefinition<_superverse_v2_DeleteFlowChannelMessageRequest, _superverse_v2_DeleteFlowChannelMessageResponse, _superverse_v2_DeleteFlowChannelMessageRequest__Output, _superverse_v2_DeleteFlowChannelMessageResponse__Output>
  GetFlowHistory: MethodDefinition<_superverse_v2_GetFlowHistoryRequest, _superverse_v2_GetFlowHistoryResponse, _superverse_v2_GetFlowHistoryRequest__Output, _superverse_v2_GetFlowHistoryResponse__Output>
  GetFlowState: MethodDefinition<_superverse_v2_GetFlowStateRequest, _superverse_v2_GetFlowStateResponse, _superverse_v2_GetFlowStateRequest__Output, _superverse_v2_GetFlowStateResponse__Output>
  GetFlowSummary: MethodDefinition<_superverse_v2_GetFlowSummaryRequest, _superverse_v2_GetFlowSummaryResponse, _superverse_v2_GetFlowSummaryRequest__Output, _superverse_v2_GetFlowSummaryResponse__Output>
  LoadFlowBlobs: MethodDefinition<_superverse_v2_LoadFlowBlobsRequest, _superverse_v2_LoadFlowBlobsResponse, _superverse_v2_LoadFlowBlobsRequest__Output, _superverse_v2_LoadFlowBlobsResponse__Output>
  ReadFlowStream: MethodDefinition<_superverse_v2_ReadFlowStreamRequest, _superverse_v2_ReadFlowStreamResponse, _superverse_v2_ReadFlowStreamRequest__Output, _superverse_v2_ReadFlowStreamResponse__Output>
  StopFlow: MethodDefinition<_superverse_v2_StopFlowRequest, _superverse_v2_StopFlowResponse, _superverse_v2_StopFlowRequest__Output, _superverse_v2_StopFlowResponse__Output>
  TimeTravelFlow: MethodDefinition<_superverse_v2_TimeTravelFlowRequest, _superverse_v2_TimeTravelFlowResponse, _superverse_v2_TimeTravelFlowRequest__Output, _superverse_v2_TimeTravelFlowResponse__Output>
  WaitFlowHistory: MethodDefinition<_superverse_v2_WaitFlowHistoryRequest, _superverse_v2_WaitFlowHistoryResponse, _superverse_v2_WaitFlowHistoryRequest__Output, _superverse_v2_WaitFlowHistoryResponse__Output>
}
