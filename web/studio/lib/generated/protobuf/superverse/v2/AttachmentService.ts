// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { CompleteAttachmentUploadRequest as _superverse_v2_CompleteAttachmentUploadRequest, CompleteAttachmentUploadRequest__Output as _superverse_v2_CompleteAttachmentUploadRequest__Output } from '../../superverse/v2/CompleteAttachmentUploadRequest';
import type { CompleteAttachmentUploadResponse as _superverse_v2_CompleteAttachmentUploadResponse, CompleteAttachmentUploadResponse__Output as _superverse_v2_CompleteAttachmentUploadResponse__Output } from '../../superverse/v2/CompleteAttachmentUploadResponse';
import type { CreateAttachmentUploadRequest as _superverse_v2_CreateAttachmentUploadRequest, CreateAttachmentUploadRequest__Output as _superverse_v2_CreateAttachmentUploadRequest__Output } from '../../superverse/v2/CreateAttachmentUploadRequest';
import type { CreateAttachmentUploadResponse as _superverse_v2_CreateAttachmentUploadResponse, CreateAttachmentUploadResponse__Output as _superverse_v2_CreateAttachmentUploadResponse__Output } from '../../superverse/v2/CreateAttachmentUploadResponse';
import type { DeleteAttachmentRequest as _superverse_v2_DeleteAttachmentRequest, DeleteAttachmentRequest__Output as _superverse_v2_DeleteAttachmentRequest__Output } from '../../superverse/v2/DeleteAttachmentRequest';
import type { DeleteAttachmentResponse as _superverse_v2_DeleteAttachmentResponse, DeleteAttachmentResponse__Output as _superverse_v2_DeleteAttachmentResponse__Output } from '../../superverse/v2/DeleteAttachmentResponse';
import type { GetAttachmentDownloadsRequest as _superverse_v2_GetAttachmentDownloadsRequest, GetAttachmentDownloadsRequest__Output as _superverse_v2_GetAttachmentDownloadsRequest__Output } from '../../superverse/v2/GetAttachmentDownloadsRequest';
import type { GetAttachmentDownloadsResponse as _superverse_v2_GetAttachmentDownloadsResponse, GetAttachmentDownloadsResponse__Output as _superverse_v2_GetAttachmentDownloadsResponse__Output } from '../../superverse/v2/GetAttachmentDownloadsResponse';

export interface AttachmentServiceClient extends grpc.Client {
  CompleteAttachmentUpload(argument: _superverse_v2_CompleteAttachmentUploadRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CompleteAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  CompleteAttachmentUpload(argument: _superverse_v2_CompleteAttachmentUploadRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CompleteAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  CompleteAttachmentUpload(argument: _superverse_v2_CompleteAttachmentUploadRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CompleteAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  CompleteAttachmentUpload(argument: _superverse_v2_CompleteAttachmentUploadRequest, callback: grpc.requestCallback<_superverse_v2_CompleteAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  completeAttachmentUpload(argument: _superverse_v2_CompleteAttachmentUploadRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CompleteAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  completeAttachmentUpload(argument: _superverse_v2_CompleteAttachmentUploadRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CompleteAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  completeAttachmentUpload(argument: _superverse_v2_CompleteAttachmentUploadRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CompleteAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  completeAttachmentUpload(argument: _superverse_v2_CompleteAttachmentUploadRequest, callback: grpc.requestCallback<_superverse_v2_CompleteAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;

  CreateAttachmentUpload(argument: _superverse_v2_CreateAttachmentUploadRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  CreateAttachmentUpload(argument: _superverse_v2_CreateAttachmentUploadRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CreateAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  CreateAttachmentUpload(argument: _superverse_v2_CreateAttachmentUploadRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  CreateAttachmentUpload(argument: _superverse_v2_CreateAttachmentUploadRequest, callback: grpc.requestCallback<_superverse_v2_CreateAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  createAttachmentUpload(argument: _superverse_v2_CreateAttachmentUploadRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  createAttachmentUpload(argument: _superverse_v2_CreateAttachmentUploadRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CreateAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  createAttachmentUpload(argument: _superverse_v2_CreateAttachmentUploadRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;
  createAttachmentUpload(argument: _superverse_v2_CreateAttachmentUploadRequest, callback: grpc.requestCallback<_superverse_v2_CreateAttachmentUploadResponse__Output>): grpc.ClientUnaryCall;

  DeleteAttachment(argument: _superverse_v2_DeleteAttachmentRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteAttachmentResponse__Output>): grpc.ClientUnaryCall;
  DeleteAttachment(argument: _superverse_v2_DeleteAttachmentRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteAttachmentResponse__Output>): grpc.ClientUnaryCall;
  DeleteAttachment(argument: _superverse_v2_DeleteAttachmentRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteAttachmentResponse__Output>): grpc.ClientUnaryCall;
  DeleteAttachment(argument: _superverse_v2_DeleteAttachmentRequest, callback: grpc.requestCallback<_superverse_v2_DeleteAttachmentResponse__Output>): grpc.ClientUnaryCall;
  deleteAttachment(argument: _superverse_v2_DeleteAttachmentRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteAttachmentResponse__Output>): grpc.ClientUnaryCall;
  deleteAttachment(argument: _superverse_v2_DeleteAttachmentRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteAttachmentResponse__Output>): grpc.ClientUnaryCall;
  deleteAttachment(argument: _superverse_v2_DeleteAttachmentRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteAttachmentResponse__Output>): grpc.ClientUnaryCall;
  deleteAttachment(argument: _superverse_v2_DeleteAttachmentRequest, callback: grpc.requestCallback<_superverse_v2_DeleteAttachmentResponse__Output>): grpc.ClientUnaryCall;

  GetAttachmentDownloads(argument: _superverse_v2_GetAttachmentDownloadsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetAttachmentDownloadsResponse__Output>): grpc.ClientUnaryCall;
  GetAttachmentDownloads(argument: _superverse_v2_GetAttachmentDownloadsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetAttachmentDownloadsResponse__Output>): grpc.ClientUnaryCall;
  GetAttachmentDownloads(argument: _superverse_v2_GetAttachmentDownloadsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetAttachmentDownloadsResponse__Output>): grpc.ClientUnaryCall;
  GetAttachmentDownloads(argument: _superverse_v2_GetAttachmentDownloadsRequest, callback: grpc.requestCallback<_superverse_v2_GetAttachmentDownloadsResponse__Output>): grpc.ClientUnaryCall;
  getAttachmentDownloads(argument: _superverse_v2_GetAttachmentDownloadsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetAttachmentDownloadsResponse__Output>): grpc.ClientUnaryCall;
  getAttachmentDownloads(argument: _superverse_v2_GetAttachmentDownloadsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetAttachmentDownloadsResponse__Output>): grpc.ClientUnaryCall;
  getAttachmentDownloads(argument: _superverse_v2_GetAttachmentDownloadsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetAttachmentDownloadsResponse__Output>): grpc.ClientUnaryCall;
  getAttachmentDownloads(argument: _superverse_v2_GetAttachmentDownloadsRequest, callback: grpc.requestCallback<_superverse_v2_GetAttachmentDownloadsResponse__Output>): grpc.ClientUnaryCall;

}

export interface AttachmentServiceHandlers extends grpc.UntypedServiceImplementation {
  CompleteAttachmentUpload: grpc.handleUnaryCall<_superverse_v2_CompleteAttachmentUploadRequest__Output, _superverse_v2_CompleteAttachmentUploadResponse>;

  CreateAttachmentUpload: grpc.handleUnaryCall<_superverse_v2_CreateAttachmentUploadRequest__Output, _superverse_v2_CreateAttachmentUploadResponse>;

  DeleteAttachment: grpc.handleUnaryCall<_superverse_v2_DeleteAttachmentRequest__Output, _superverse_v2_DeleteAttachmentResponse>;

  GetAttachmentDownloads: grpc.handleUnaryCall<_superverse_v2_GetAttachmentDownloadsRequest__Output, _superverse_v2_GetAttachmentDownloadsResponse>;

}

export interface AttachmentServiceDefinition extends grpc.ServiceDefinition {
  CompleteAttachmentUpload: MethodDefinition<_superverse_v2_CompleteAttachmentUploadRequest, _superverse_v2_CompleteAttachmentUploadResponse, _superverse_v2_CompleteAttachmentUploadRequest__Output, _superverse_v2_CompleteAttachmentUploadResponse__Output>
  CreateAttachmentUpload: MethodDefinition<_superverse_v2_CreateAttachmentUploadRequest, _superverse_v2_CreateAttachmentUploadResponse, _superverse_v2_CreateAttachmentUploadRequest__Output, _superverse_v2_CreateAttachmentUploadResponse__Output>
  DeleteAttachment: MethodDefinition<_superverse_v2_DeleteAttachmentRequest, _superverse_v2_DeleteAttachmentResponse, _superverse_v2_DeleteAttachmentRequest__Output, _superverse_v2_DeleteAttachmentResponse__Output>
  GetAttachmentDownloads: MethodDefinition<_superverse_v2_GetAttachmentDownloadsRequest, _superverse_v2_GetAttachmentDownloadsResponse, _superverse_v2_GetAttachmentDownloadsRequest__Output, _superverse_v2_GetAttachmentDownloadsResponse__Output>
}
