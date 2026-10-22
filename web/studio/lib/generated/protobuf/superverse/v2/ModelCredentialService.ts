// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { DeleteModelCredentialRequest as _superverse_v2_DeleteModelCredentialRequest, DeleteModelCredentialRequest__Output as _superverse_v2_DeleteModelCredentialRequest__Output } from '../../superverse/v2/DeleteModelCredentialRequest';
import type { DeleteModelCredentialResponse as _superverse_v2_DeleteModelCredentialResponse, DeleteModelCredentialResponse__Output as _superverse_v2_DeleteModelCredentialResponse__Output } from '../../superverse/v2/DeleteModelCredentialResponse';
import type { GetModelCredentialInfoRequest as _superverse_v2_GetModelCredentialInfoRequest, GetModelCredentialInfoRequest__Output as _superverse_v2_GetModelCredentialInfoRequest__Output } from '../../superverse/v2/GetModelCredentialInfoRequest';
import type { GetModelCredentialInfoResponse as _superverse_v2_GetModelCredentialInfoResponse, GetModelCredentialInfoResponse__Output as _superverse_v2_GetModelCredentialInfoResponse__Output } from '../../superverse/v2/GetModelCredentialInfoResponse';
import type { SetModelCredentialRequest as _superverse_v2_SetModelCredentialRequest, SetModelCredentialRequest__Output as _superverse_v2_SetModelCredentialRequest__Output } from '../../superverse/v2/SetModelCredentialRequest';
import type { SetModelCredentialResponse as _superverse_v2_SetModelCredentialResponse, SetModelCredentialResponse__Output as _superverse_v2_SetModelCredentialResponse__Output } from '../../superverse/v2/SetModelCredentialResponse';

export interface ModelCredentialServiceClient extends grpc.Client {
  DeleteModelCredential(argument: _superverse_v2_DeleteModelCredentialRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  DeleteModelCredential(argument: _superverse_v2_DeleteModelCredentialRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  DeleteModelCredential(argument: _superverse_v2_DeleteModelCredentialRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  DeleteModelCredential(argument: _superverse_v2_DeleteModelCredentialRequest, callback: grpc.requestCallback<_superverse_v2_DeleteModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  deleteModelCredential(argument: _superverse_v2_DeleteModelCredentialRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  deleteModelCredential(argument: _superverse_v2_DeleteModelCredentialRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  deleteModelCredential(argument: _superverse_v2_DeleteModelCredentialRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  deleteModelCredential(argument: _superverse_v2_DeleteModelCredentialRequest, callback: grpc.requestCallback<_superverse_v2_DeleteModelCredentialResponse__Output>): grpc.ClientUnaryCall;

  GetModelCredentialInfo(argument: _superverse_v2_GetModelCredentialInfoRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetModelCredentialInfoResponse__Output>): grpc.ClientUnaryCall;
  GetModelCredentialInfo(argument: _superverse_v2_GetModelCredentialInfoRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetModelCredentialInfoResponse__Output>): grpc.ClientUnaryCall;
  GetModelCredentialInfo(argument: _superverse_v2_GetModelCredentialInfoRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetModelCredentialInfoResponse__Output>): grpc.ClientUnaryCall;
  GetModelCredentialInfo(argument: _superverse_v2_GetModelCredentialInfoRequest, callback: grpc.requestCallback<_superverse_v2_GetModelCredentialInfoResponse__Output>): grpc.ClientUnaryCall;
  getModelCredentialInfo(argument: _superverse_v2_GetModelCredentialInfoRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetModelCredentialInfoResponse__Output>): grpc.ClientUnaryCall;
  getModelCredentialInfo(argument: _superverse_v2_GetModelCredentialInfoRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetModelCredentialInfoResponse__Output>): grpc.ClientUnaryCall;
  getModelCredentialInfo(argument: _superverse_v2_GetModelCredentialInfoRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetModelCredentialInfoResponse__Output>): grpc.ClientUnaryCall;
  getModelCredentialInfo(argument: _superverse_v2_GetModelCredentialInfoRequest, callback: grpc.requestCallback<_superverse_v2_GetModelCredentialInfoResponse__Output>): grpc.ClientUnaryCall;

  SetModelCredential(argument: _superverse_v2_SetModelCredentialRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  SetModelCredential(argument: _superverse_v2_SetModelCredentialRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SetModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  SetModelCredential(argument: _superverse_v2_SetModelCredentialRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  SetModelCredential(argument: _superverse_v2_SetModelCredentialRequest, callback: grpc.requestCallback<_superverse_v2_SetModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  setModelCredential(argument: _superverse_v2_SetModelCredentialRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  setModelCredential(argument: _superverse_v2_SetModelCredentialRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SetModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  setModelCredential(argument: _superverse_v2_SetModelCredentialRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetModelCredentialResponse__Output>): grpc.ClientUnaryCall;
  setModelCredential(argument: _superverse_v2_SetModelCredentialRequest, callback: grpc.requestCallback<_superverse_v2_SetModelCredentialResponse__Output>): grpc.ClientUnaryCall;

}

export interface ModelCredentialServiceHandlers extends grpc.UntypedServiceImplementation {
  DeleteModelCredential: grpc.handleUnaryCall<_superverse_v2_DeleteModelCredentialRequest__Output, _superverse_v2_DeleteModelCredentialResponse>;

  GetModelCredentialInfo: grpc.handleUnaryCall<_superverse_v2_GetModelCredentialInfoRequest__Output, _superverse_v2_GetModelCredentialInfoResponse>;

  SetModelCredential: grpc.handleUnaryCall<_superverse_v2_SetModelCredentialRequest__Output, _superverse_v2_SetModelCredentialResponse>;

}

export interface ModelCredentialServiceDefinition extends grpc.ServiceDefinition {
  DeleteModelCredential: MethodDefinition<_superverse_v2_DeleteModelCredentialRequest, _superverse_v2_DeleteModelCredentialResponse, _superverse_v2_DeleteModelCredentialRequest__Output, _superverse_v2_DeleteModelCredentialResponse__Output>
  GetModelCredentialInfo: MethodDefinition<_superverse_v2_GetModelCredentialInfoRequest, _superverse_v2_GetModelCredentialInfoResponse, _superverse_v2_GetModelCredentialInfoRequest__Output, _superverse_v2_GetModelCredentialInfoResponse__Output>
  SetModelCredential: MethodDefinition<_superverse_v2_SetModelCredentialRequest, _superverse_v2_SetModelCredentialResponse, _superverse_v2_SetModelCredentialRequest__Output, _superverse_v2_SetModelCredentialResponse__Output>
}
