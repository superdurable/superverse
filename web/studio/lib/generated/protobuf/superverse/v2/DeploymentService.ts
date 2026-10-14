// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { CancelDeploymentRequest as _superverse_v2_CancelDeploymentRequest, CancelDeploymentRequest__Output as _superverse_v2_CancelDeploymentRequest__Output } from '../../superverse/v2/CancelDeploymentRequest';
import type { CancelDeploymentResponse as _superverse_v2_CancelDeploymentResponse, CancelDeploymentResponse__Output as _superverse_v2_CancelDeploymentResponse__Output } from '../../superverse/v2/CancelDeploymentResponse';
import type { GetDeploymentRequest as _superverse_v2_GetDeploymentRequest, GetDeploymentRequest__Output as _superverse_v2_GetDeploymentRequest__Output } from '../../superverse/v2/GetDeploymentRequest';
import type { GetDeploymentResponse as _superverse_v2_GetDeploymentResponse, GetDeploymentResponse__Output as _superverse_v2_GetDeploymentResponse__Output } from '../../superverse/v2/GetDeploymentResponse';
import type { ListDeploymentsRequest as _superverse_v2_ListDeploymentsRequest, ListDeploymentsRequest__Output as _superverse_v2_ListDeploymentsRequest__Output } from '../../superverse/v2/ListDeploymentsRequest';
import type { ListDeploymentsResponse as _superverse_v2_ListDeploymentsResponse, ListDeploymentsResponse__Output as _superverse_v2_ListDeploymentsResponse__Output } from '../../superverse/v2/ListDeploymentsResponse';
import type { StartDeploymentRequest as _superverse_v2_StartDeploymentRequest, StartDeploymentRequest__Output as _superverse_v2_StartDeploymentRequest__Output } from '../../superverse/v2/StartDeploymentRequest';
import type { StartDeploymentResponse as _superverse_v2_StartDeploymentResponse, StartDeploymentResponse__Output as _superverse_v2_StartDeploymentResponse__Output } from '../../superverse/v2/StartDeploymentResponse';
import type { WatchDeploymentRequest as _superverse_v2_WatchDeploymentRequest, WatchDeploymentRequest__Output as _superverse_v2_WatchDeploymentRequest__Output } from '../../superverse/v2/WatchDeploymentRequest';
import type { WatchDeploymentResponse as _superverse_v2_WatchDeploymentResponse, WatchDeploymentResponse__Output as _superverse_v2_WatchDeploymentResponse__Output } from '../../superverse/v2/WatchDeploymentResponse';

export interface DeploymentServiceClient extends grpc.Client {
  CancelDeployment(argument: _superverse_v2_CancelDeploymentRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CancelDeploymentResponse__Output>): grpc.ClientUnaryCall;
  CancelDeployment(argument: _superverse_v2_CancelDeploymentRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CancelDeploymentResponse__Output>): grpc.ClientUnaryCall;
  CancelDeployment(argument: _superverse_v2_CancelDeploymentRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CancelDeploymentResponse__Output>): grpc.ClientUnaryCall;
  CancelDeployment(argument: _superverse_v2_CancelDeploymentRequest, callback: grpc.requestCallback<_superverse_v2_CancelDeploymentResponse__Output>): grpc.ClientUnaryCall;
  cancelDeployment(argument: _superverse_v2_CancelDeploymentRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CancelDeploymentResponse__Output>): grpc.ClientUnaryCall;
  cancelDeployment(argument: _superverse_v2_CancelDeploymentRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CancelDeploymentResponse__Output>): grpc.ClientUnaryCall;
  cancelDeployment(argument: _superverse_v2_CancelDeploymentRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CancelDeploymentResponse__Output>): grpc.ClientUnaryCall;
  cancelDeployment(argument: _superverse_v2_CancelDeploymentRequest, callback: grpc.requestCallback<_superverse_v2_CancelDeploymentResponse__Output>): grpc.ClientUnaryCall;

  GetDeployment(argument: _superverse_v2_GetDeploymentRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetDeploymentResponse__Output>): grpc.ClientUnaryCall;
  GetDeployment(argument: _superverse_v2_GetDeploymentRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetDeploymentResponse__Output>): grpc.ClientUnaryCall;
  GetDeployment(argument: _superverse_v2_GetDeploymentRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetDeploymentResponse__Output>): grpc.ClientUnaryCall;
  GetDeployment(argument: _superverse_v2_GetDeploymentRequest, callback: grpc.requestCallback<_superverse_v2_GetDeploymentResponse__Output>): grpc.ClientUnaryCall;
  getDeployment(argument: _superverse_v2_GetDeploymentRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetDeploymentResponse__Output>): grpc.ClientUnaryCall;
  getDeployment(argument: _superverse_v2_GetDeploymentRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetDeploymentResponse__Output>): grpc.ClientUnaryCall;
  getDeployment(argument: _superverse_v2_GetDeploymentRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetDeploymentResponse__Output>): grpc.ClientUnaryCall;
  getDeployment(argument: _superverse_v2_GetDeploymentRequest, callback: grpc.requestCallback<_superverse_v2_GetDeploymentResponse__Output>): grpc.ClientUnaryCall;

  ListDeployments(argument: _superverse_v2_ListDeploymentsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListDeploymentsResponse__Output>): grpc.ClientUnaryCall;
  ListDeployments(argument: _superverse_v2_ListDeploymentsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListDeploymentsResponse__Output>): grpc.ClientUnaryCall;
  ListDeployments(argument: _superverse_v2_ListDeploymentsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListDeploymentsResponse__Output>): grpc.ClientUnaryCall;
  ListDeployments(argument: _superverse_v2_ListDeploymentsRequest, callback: grpc.requestCallback<_superverse_v2_ListDeploymentsResponse__Output>): grpc.ClientUnaryCall;
  listDeployments(argument: _superverse_v2_ListDeploymentsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListDeploymentsResponse__Output>): grpc.ClientUnaryCall;
  listDeployments(argument: _superverse_v2_ListDeploymentsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListDeploymentsResponse__Output>): grpc.ClientUnaryCall;
  listDeployments(argument: _superverse_v2_ListDeploymentsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListDeploymentsResponse__Output>): grpc.ClientUnaryCall;
  listDeployments(argument: _superverse_v2_ListDeploymentsRequest, callback: grpc.requestCallback<_superverse_v2_ListDeploymentsResponse__Output>): grpc.ClientUnaryCall;

  StartDeployment(argument: _superverse_v2_StartDeploymentRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartDeploymentResponse__Output>): grpc.ClientUnaryCall;
  StartDeployment(argument: _superverse_v2_StartDeploymentRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartDeploymentResponse__Output>): grpc.ClientUnaryCall;
  StartDeployment(argument: _superverse_v2_StartDeploymentRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartDeploymentResponse__Output>): grpc.ClientUnaryCall;
  StartDeployment(argument: _superverse_v2_StartDeploymentRequest, callback: grpc.requestCallback<_superverse_v2_StartDeploymentResponse__Output>): grpc.ClientUnaryCall;
  startDeployment(argument: _superverse_v2_StartDeploymentRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartDeploymentResponse__Output>): grpc.ClientUnaryCall;
  startDeployment(argument: _superverse_v2_StartDeploymentRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartDeploymentResponse__Output>): grpc.ClientUnaryCall;
  startDeployment(argument: _superverse_v2_StartDeploymentRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartDeploymentResponse__Output>): grpc.ClientUnaryCall;
  startDeployment(argument: _superverse_v2_StartDeploymentRequest, callback: grpc.requestCallback<_superverse_v2_StartDeploymentResponse__Output>): grpc.ClientUnaryCall;

  WatchDeployment(argument: _superverse_v2_WatchDeploymentRequest, metadata: grpc.Metadata, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchDeploymentResponse__Output>;
  WatchDeployment(argument: _superverse_v2_WatchDeploymentRequest, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchDeploymentResponse__Output>;
  watchDeployment(argument: _superverse_v2_WatchDeploymentRequest, metadata: grpc.Metadata, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchDeploymentResponse__Output>;
  watchDeployment(argument: _superverse_v2_WatchDeploymentRequest, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchDeploymentResponse__Output>;

}

export interface DeploymentServiceHandlers extends grpc.UntypedServiceImplementation {
  CancelDeployment: grpc.handleUnaryCall<_superverse_v2_CancelDeploymentRequest__Output, _superverse_v2_CancelDeploymentResponse>;

  GetDeployment: grpc.handleUnaryCall<_superverse_v2_GetDeploymentRequest__Output, _superverse_v2_GetDeploymentResponse>;

  ListDeployments: grpc.handleUnaryCall<_superverse_v2_ListDeploymentsRequest__Output, _superverse_v2_ListDeploymentsResponse>;

  StartDeployment: grpc.handleUnaryCall<_superverse_v2_StartDeploymentRequest__Output, _superverse_v2_StartDeploymentResponse>;

  WatchDeployment: grpc.handleServerStreamingCall<_superverse_v2_WatchDeploymentRequest__Output, _superverse_v2_WatchDeploymentResponse>;

}

export interface DeploymentServiceDefinition extends grpc.ServiceDefinition {
  CancelDeployment: MethodDefinition<_superverse_v2_CancelDeploymentRequest, _superverse_v2_CancelDeploymentResponse, _superverse_v2_CancelDeploymentRequest__Output, _superverse_v2_CancelDeploymentResponse__Output>
  GetDeployment: MethodDefinition<_superverse_v2_GetDeploymentRequest, _superverse_v2_GetDeploymentResponse, _superverse_v2_GetDeploymentRequest__Output, _superverse_v2_GetDeploymentResponse__Output>
  ListDeployments: MethodDefinition<_superverse_v2_ListDeploymentsRequest, _superverse_v2_ListDeploymentsResponse, _superverse_v2_ListDeploymentsRequest__Output, _superverse_v2_ListDeploymentsResponse__Output>
  StartDeployment: MethodDefinition<_superverse_v2_StartDeploymentRequest, _superverse_v2_StartDeploymentResponse, _superverse_v2_StartDeploymentRequest__Output, _superverse_v2_StartDeploymentResponse__Output>
  WatchDeployment: MethodDefinition<_superverse_v2_WatchDeploymentRequest, _superverse_v2_WatchDeploymentResponse, _superverse_v2_WatchDeploymentRequest__Output, _superverse_v2_WatchDeploymentResponse__Output>
}
