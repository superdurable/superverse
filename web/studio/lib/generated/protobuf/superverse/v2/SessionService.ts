// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { GetSessionRequest as _superverse_v2_GetSessionRequest, GetSessionRequest__Output as _superverse_v2_GetSessionRequest__Output } from '../../superverse/v2/GetSessionRequest';
import type { GetSessionResponse as _superverse_v2_GetSessionResponse, GetSessionResponse__Output as _superverse_v2_GetSessionResponse__Output } from '../../superverse/v2/GetSessionResponse';
import type { ListArchivedMessagesRequest as _superverse_v2_ListArchivedMessagesRequest, ListArchivedMessagesRequest__Output as _superverse_v2_ListArchivedMessagesRequest__Output } from '../../superverse/v2/ListArchivedMessagesRequest';
import type { ListArchivedMessagesResponse as _superverse_v2_ListArchivedMessagesResponse, ListArchivedMessagesResponse__Output as _superverse_v2_ListArchivedMessagesResponse__Output } from '../../superverse/v2/ListArchivedMessagesResponse';
import type { RenewWorkspaceRequest as _superverse_v2_RenewWorkspaceRequest, RenewWorkspaceRequest__Output as _superverse_v2_RenewWorkspaceRequest__Output } from '../../superverse/v2/RenewWorkspaceRequest';
import type { RenewWorkspaceResponse as _superverse_v2_RenewWorkspaceResponse, RenewWorkspaceResponse__Output as _superverse_v2_RenewWorkspaceResponse__Output } from '../../superverse/v2/RenewWorkspaceResponse';
import type { WatchSessionRequest as _superverse_v2_WatchSessionRequest, WatchSessionRequest__Output as _superverse_v2_WatchSessionRequest__Output } from '../../superverse/v2/WatchSessionRequest';
import type { WatchSessionResponse as _superverse_v2_WatchSessionResponse, WatchSessionResponse__Output as _superverse_v2_WatchSessionResponse__Output } from '../../superverse/v2/WatchSessionResponse';

export interface SessionServiceClient extends grpc.Client {
  GetSession(argument: _superverse_v2_GetSessionRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetSessionResponse__Output>): grpc.ClientUnaryCall;
  GetSession(argument: _superverse_v2_GetSessionRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetSessionResponse__Output>): grpc.ClientUnaryCall;
  GetSession(argument: _superverse_v2_GetSessionRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetSessionResponse__Output>): grpc.ClientUnaryCall;
  GetSession(argument: _superverse_v2_GetSessionRequest, callback: grpc.requestCallback<_superverse_v2_GetSessionResponse__Output>): grpc.ClientUnaryCall;
  getSession(argument: _superverse_v2_GetSessionRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetSessionResponse__Output>): grpc.ClientUnaryCall;
  getSession(argument: _superverse_v2_GetSessionRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetSessionResponse__Output>): grpc.ClientUnaryCall;
  getSession(argument: _superverse_v2_GetSessionRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetSessionResponse__Output>): grpc.ClientUnaryCall;
  getSession(argument: _superverse_v2_GetSessionRequest, callback: grpc.requestCallback<_superverse_v2_GetSessionResponse__Output>): grpc.ClientUnaryCall;

  ListArchivedMessages(argument: _superverse_v2_ListArchivedMessagesRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListArchivedMessagesResponse__Output>): grpc.ClientUnaryCall;
  ListArchivedMessages(argument: _superverse_v2_ListArchivedMessagesRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListArchivedMessagesResponse__Output>): grpc.ClientUnaryCall;
  ListArchivedMessages(argument: _superverse_v2_ListArchivedMessagesRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListArchivedMessagesResponse__Output>): grpc.ClientUnaryCall;
  ListArchivedMessages(argument: _superverse_v2_ListArchivedMessagesRequest, callback: grpc.requestCallback<_superverse_v2_ListArchivedMessagesResponse__Output>): grpc.ClientUnaryCall;
  listArchivedMessages(argument: _superverse_v2_ListArchivedMessagesRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListArchivedMessagesResponse__Output>): grpc.ClientUnaryCall;
  listArchivedMessages(argument: _superverse_v2_ListArchivedMessagesRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListArchivedMessagesResponse__Output>): grpc.ClientUnaryCall;
  listArchivedMessages(argument: _superverse_v2_ListArchivedMessagesRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListArchivedMessagesResponse__Output>): grpc.ClientUnaryCall;
  listArchivedMessages(argument: _superverse_v2_ListArchivedMessagesRequest, callback: grpc.requestCallback<_superverse_v2_ListArchivedMessagesResponse__Output>): grpc.ClientUnaryCall;

  RenewWorkspace(argument: _superverse_v2_RenewWorkspaceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RenewWorkspaceResponse__Output>): grpc.ClientUnaryCall;
  RenewWorkspace(argument: _superverse_v2_RenewWorkspaceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RenewWorkspaceResponse__Output>): grpc.ClientUnaryCall;
  RenewWorkspace(argument: _superverse_v2_RenewWorkspaceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RenewWorkspaceResponse__Output>): grpc.ClientUnaryCall;
  RenewWorkspace(argument: _superverse_v2_RenewWorkspaceRequest, callback: grpc.requestCallback<_superverse_v2_RenewWorkspaceResponse__Output>): grpc.ClientUnaryCall;
  renewWorkspace(argument: _superverse_v2_RenewWorkspaceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RenewWorkspaceResponse__Output>): grpc.ClientUnaryCall;
  renewWorkspace(argument: _superverse_v2_RenewWorkspaceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RenewWorkspaceResponse__Output>): grpc.ClientUnaryCall;
  renewWorkspace(argument: _superverse_v2_RenewWorkspaceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RenewWorkspaceResponse__Output>): grpc.ClientUnaryCall;
  renewWorkspace(argument: _superverse_v2_RenewWorkspaceRequest, callback: grpc.requestCallback<_superverse_v2_RenewWorkspaceResponse__Output>): grpc.ClientUnaryCall;

  WatchSession(argument: _superverse_v2_WatchSessionRequest, metadata: grpc.Metadata, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchSessionResponse__Output>;
  WatchSession(argument: _superverse_v2_WatchSessionRequest, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchSessionResponse__Output>;
  watchSession(argument: _superverse_v2_WatchSessionRequest, metadata: grpc.Metadata, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchSessionResponse__Output>;
  watchSession(argument: _superverse_v2_WatchSessionRequest, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchSessionResponse__Output>;

}

export interface SessionServiceHandlers extends grpc.UntypedServiceImplementation {
  GetSession: grpc.handleUnaryCall<_superverse_v2_GetSessionRequest__Output, _superverse_v2_GetSessionResponse>;

  ListArchivedMessages: grpc.handleUnaryCall<_superverse_v2_ListArchivedMessagesRequest__Output, _superverse_v2_ListArchivedMessagesResponse>;

  RenewWorkspace: grpc.handleUnaryCall<_superverse_v2_RenewWorkspaceRequest__Output, _superverse_v2_RenewWorkspaceResponse>;

  WatchSession: grpc.handleServerStreamingCall<_superverse_v2_WatchSessionRequest__Output, _superverse_v2_WatchSessionResponse>;

}

export interface SessionServiceDefinition extends grpc.ServiceDefinition {
  GetSession: MethodDefinition<_superverse_v2_GetSessionRequest, _superverse_v2_GetSessionResponse, _superverse_v2_GetSessionRequest__Output, _superverse_v2_GetSessionResponse__Output>
  ListArchivedMessages: MethodDefinition<_superverse_v2_ListArchivedMessagesRequest, _superverse_v2_ListArchivedMessagesResponse, _superverse_v2_ListArchivedMessagesRequest__Output, _superverse_v2_ListArchivedMessagesResponse__Output>
  RenewWorkspace: MethodDefinition<_superverse_v2_RenewWorkspaceRequest, _superverse_v2_RenewWorkspaceResponse, _superverse_v2_RenewWorkspaceRequest__Output, _superverse_v2_RenewWorkspaceResponse__Output>
  WatchSession: MethodDefinition<_superverse_v2_WatchSessionRequest, _superverse_v2_WatchSessionResponse, _superverse_v2_WatchSessionRequest__Output, _superverse_v2_WatchSessionResponse__Output>
}
