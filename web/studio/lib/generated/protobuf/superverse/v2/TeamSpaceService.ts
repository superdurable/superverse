// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { CreateTeamSpaceRequest as _superverse_v2_CreateTeamSpaceRequest, CreateTeamSpaceRequest__Output as _superverse_v2_CreateTeamSpaceRequest__Output } from '../../superverse/v2/CreateTeamSpaceRequest';
import type { CreateTeamSpaceResponse as _superverse_v2_CreateTeamSpaceResponse, CreateTeamSpaceResponse__Output as _superverse_v2_CreateTeamSpaceResponse__Output } from '../../superverse/v2/CreateTeamSpaceResponse';
import type { DeleteTeamSpaceRequest as _superverse_v2_DeleteTeamSpaceRequest, DeleteTeamSpaceRequest__Output as _superverse_v2_DeleteTeamSpaceRequest__Output } from '../../superverse/v2/DeleteTeamSpaceRequest';
import type { DeleteTeamSpaceResponse as _superverse_v2_DeleteTeamSpaceResponse, DeleteTeamSpaceResponse__Output as _superverse_v2_DeleteTeamSpaceResponse__Output } from '../../superverse/v2/DeleteTeamSpaceResponse';
import type { GetTeamSpaceOverviewRequest as _superverse_v2_GetTeamSpaceOverviewRequest, GetTeamSpaceOverviewRequest__Output as _superverse_v2_GetTeamSpaceOverviewRequest__Output } from '../../superverse/v2/GetTeamSpaceOverviewRequest';
import type { GetTeamSpaceOverviewResponse as _superverse_v2_GetTeamSpaceOverviewResponse, GetTeamSpaceOverviewResponse__Output as _superverse_v2_GetTeamSpaceOverviewResponse__Output } from '../../superverse/v2/GetTeamSpaceOverviewResponse';
import type { GetTeamSpaceRequest as _superverse_v2_GetTeamSpaceRequest, GetTeamSpaceRequest__Output as _superverse_v2_GetTeamSpaceRequest__Output } from '../../superverse/v2/GetTeamSpaceRequest';
import type { GetTeamSpaceResponse as _superverse_v2_GetTeamSpaceResponse, GetTeamSpaceResponse__Output as _superverse_v2_GetTeamSpaceResponse__Output } from '../../superverse/v2/GetTeamSpaceResponse';
import type { ListTeamSpacesRequest as _superverse_v2_ListTeamSpacesRequest, ListTeamSpacesRequest__Output as _superverse_v2_ListTeamSpacesRequest__Output } from '../../superverse/v2/ListTeamSpacesRequest';
import type { ListTeamSpacesResponse as _superverse_v2_ListTeamSpacesResponse, ListTeamSpacesResponse__Output as _superverse_v2_ListTeamSpacesResponse__Output } from '../../superverse/v2/ListTeamSpacesResponse';
import type { RenameTeamSpaceRequest as _superverse_v2_RenameTeamSpaceRequest, RenameTeamSpaceRequest__Output as _superverse_v2_RenameTeamSpaceRequest__Output } from '../../superverse/v2/RenameTeamSpaceRequest';
import type { RenameTeamSpaceResponse as _superverse_v2_RenameTeamSpaceResponse, RenameTeamSpaceResponse__Output as _superverse_v2_RenameTeamSpaceResponse__Output } from '../../superverse/v2/RenameTeamSpaceResponse';

export interface TeamSpaceServiceClient extends grpc.Client {
  CreateTeamSpace(argument: _superverse_v2_CreateTeamSpaceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  CreateTeamSpace(argument: _superverse_v2_CreateTeamSpaceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CreateTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  CreateTeamSpace(argument: _superverse_v2_CreateTeamSpaceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  CreateTeamSpace(argument: _superverse_v2_CreateTeamSpaceRequest, callback: grpc.requestCallback<_superverse_v2_CreateTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  createTeamSpace(argument: _superverse_v2_CreateTeamSpaceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  createTeamSpace(argument: _superverse_v2_CreateTeamSpaceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CreateTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  createTeamSpace(argument: _superverse_v2_CreateTeamSpaceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  createTeamSpace(argument: _superverse_v2_CreateTeamSpaceRequest, callback: grpc.requestCallback<_superverse_v2_CreateTeamSpaceResponse__Output>): grpc.ClientUnaryCall;

  DeleteTeamSpace(argument: _superverse_v2_DeleteTeamSpaceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  DeleteTeamSpace(argument: _superverse_v2_DeleteTeamSpaceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  DeleteTeamSpace(argument: _superverse_v2_DeleteTeamSpaceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  DeleteTeamSpace(argument: _superverse_v2_DeleteTeamSpaceRequest, callback: grpc.requestCallback<_superverse_v2_DeleteTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  deleteTeamSpace(argument: _superverse_v2_DeleteTeamSpaceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  deleteTeamSpace(argument: _superverse_v2_DeleteTeamSpaceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  deleteTeamSpace(argument: _superverse_v2_DeleteTeamSpaceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  deleteTeamSpace(argument: _superverse_v2_DeleteTeamSpaceRequest, callback: grpc.requestCallback<_superverse_v2_DeleteTeamSpaceResponse__Output>): grpc.ClientUnaryCall;

  GetTeamSpace(argument: _superverse_v2_GetTeamSpaceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  GetTeamSpace(argument: _superverse_v2_GetTeamSpaceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  GetTeamSpace(argument: _superverse_v2_GetTeamSpaceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  GetTeamSpace(argument: _superverse_v2_GetTeamSpaceRequest, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  getTeamSpace(argument: _superverse_v2_GetTeamSpaceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  getTeamSpace(argument: _superverse_v2_GetTeamSpaceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  getTeamSpace(argument: _superverse_v2_GetTeamSpaceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  getTeamSpace(argument: _superverse_v2_GetTeamSpaceRequest, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceResponse__Output>): grpc.ClientUnaryCall;

  GetTeamSpaceOverview(argument: _superverse_v2_GetTeamSpaceOverviewRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceOverviewResponse__Output>): grpc.ClientUnaryCall;
  GetTeamSpaceOverview(argument: _superverse_v2_GetTeamSpaceOverviewRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceOverviewResponse__Output>): grpc.ClientUnaryCall;
  GetTeamSpaceOverview(argument: _superverse_v2_GetTeamSpaceOverviewRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceOverviewResponse__Output>): grpc.ClientUnaryCall;
  GetTeamSpaceOverview(argument: _superverse_v2_GetTeamSpaceOverviewRequest, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceOverviewResponse__Output>): grpc.ClientUnaryCall;
  getTeamSpaceOverview(argument: _superverse_v2_GetTeamSpaceOverviewRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceOverviewResponse__Output>): grpc.ClientUnaryCall;
  getTeamSpaceOverview(argument: _superverse_v2_GetTeamSpaceOverviewRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceOverviewResponse__Output>): grpc.ClientUnaryCall;
  getTeamSpaceOverview(argument: _superverse_v2_GetTeamSpaceOverviewRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceOverviewResponse__Output>): grpc.ClientUnaryCall;
  getTeamSpaceOverview(argument: _superverse_v2_GetTeamSpaceOverviewRequest, callback: grpc.requestCallback<_superverse_v2_GetTeamSpaceOverviewResponse__Output>): grpc.ClientUnaryCall;

  ListTeamSpaces(argument: _superverse_v2_ListTeamSpacesRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListTeamSpacesResponse__Output>): grpc.ClientUnaryCall;
  ListTeamSpaces(argument: _superverse_v2_ListTeamSpacesRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListTeamSpacesResponse__Output>): grpc.ClientUnaryCall;
  ListTeamSpaces(argument: _superverse_v2_ListTeamSpacesRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListTeamSpacesResponse__Output>): grpc.ClientUnaryCall;
  ListTeamSpaces(argument: _superverse_v2_ListTeamSpacesRequest, callback: grpc.requestCallback<_superverse_v2_ListTeamSpacesResponse__Output>): grpc.ClientUnaryCall;
  listTeamSpaces(argument: _superverse_v2_ListTeamSpacesRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListTeamSpacesResponse__Output>): grpc.ClientUnaryCall;
  listTeamSpaces(argument: _superverse_v2_ListTeamSpacesRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListTeamSpacesResponse__Output>): grpc.ClientUnaryCall;
  listTeamSpaces(argument: _superverse_v2_ListTeamSpacesRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListTeamSpacesResponse__Output>): grpc.ClientUnaryCall;
  listTeamSpaces(argument: _superverse_v2_ListTeamSpacesRequest, callback: grpc.requestCallback<_superverse_v2_ListTeamSpacesResponse__Output>): grpc.ClientUnaryCall;

  RenameTeamSpace(argument: _superverse_v2_RenameTeamSpaceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RenameTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  RenameTeamSpace(argument: _superverse_v2_RenameTeamSpaceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RenameTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  RenameTeamSpace(argument: _superverse_v2_RenameTeamSpaceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RenameTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  RenameTeamSpace(argument: _superverse_v2_RenameTeamSpaceRequest, callback: grpc.requestCallback<_superverse_v2_RenameTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  renameTeamSpace(argument: _superverse_v2_RenameTeamSpaceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RenameTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  renameTeamSpace(argument: _superverse_v2_RenameTeamSpaceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RenameTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  renameTeamSpace(argument: _superverse_v2_RenameTeamSpaceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RenameTeamSpaceResponse__Output>): grpc.ClientUnaryCall;
  renameTeamSpace(argument: _superverse_v2_RenameTeamSpaceRequest, callback: grpc.requestCallback<_superverse_v2_RenameTeamSpaceResponse__Output>): grpc.ClientUnaryCall;

}

export interface TeamSpaceServiceHandlers extends grpc.UntypedServiceImplementation {
  CreateTeamSpace: grpc.handleUnaryCall<_superverse_v2_CreateTeamSpaceRequest__Output, _superverse_v2_CreateTeamSpaceResponse>;

  DeleteTeamSpace: grpc.handleUnaryCall<_superverse_v2_DeleteTeamSpaceRequest__Output, _superverse_v2_DeleteTeamSpaceResponse>;

  GetTeamSpace: grpc.handleUnaryCall<_superverse_v2_GetTeamSpaceRequest__Output, _superverse_v2_GetTeamSpaceResponse>;

  GetTeamSpaceOverview: grpc.handleUnaryCall<_superverse_v2_GetTeamSpaceOverviewRequest__Output, _superverse_v2_GetTeamSpaceOverviewResponse>;

  ListTeamSpaces: grpc.handleUnaryCall<_superverse_v2_ListTeamSpacesRequest__Output, _superverse_v2_ListTeamSpacesResponse>;

  RenameTeamSpace: grpc.handleUnaryCall<_superverse_v2_RenameTeamSpaceRequest__Output, _superverse_v2_RenameTeamSpaceResponse>;

}

export interface TeamSpaceServiceDefinition extends grpc.ServiceDefinition {
  CreateTeamSpace: MethodDefinition<_superverse_v2_CreateTeamSpaceRequest, _superverse_v2_CreateTeamSpaceResponse, _superverse_v2_CreateTeamSpaceRequest__Output, _superverse_v2_CreateTeamSpaceResponse__Output>
  DeleteTeamSpace: MethodDefinition<_superverse_v2_DeleteTeamSpaceRequest, _superverse_v2_DeleteTeamSpaceResponse, _superverse_v2_DeleteTeamSpaceRequest__Output, _superverse_v2_DeleteTeamSpaceResponse__Output>
  GetTeamSpace: MethodDefinition<_superverse_v2_GetTeamSpaceRequest, _superverse_v2_GetTeamSpaceResponse, _superverse_v2_GetTeamSpaceRequest__Output, _superverse_v2_GetTeamSpaceResponse__Output>
  GetTeamSpaceOverview: MethodDefinition<_superverse_v2_GetTeamSpaceOverviewRequest, _superverse_v2_GetTeamSpaceOverviewResponse, _superverse_v2_GetTeamSpaceOverviewRequest__Output, _superverse_v2_GetTeamSpaceOverviewResponse__Output>
  ListTeamSpaces: MethodDefinition<_superverse_v2_ListTeamSpacesRequest, _superverse_v2_ListTeamSpacesResponse, _superverse_v2_ListTeamSpacesRequest__Output, _superverse_v2_ListTeamSpacesResponse__Output>
  RenameTeamSpace: MethodDefinition<_superverse_v2_RenameTeamSpaceRequest, _superverse_v2_RenameTeamSpaceResponse, _superverse_v2_RenameTeamSpaceRequest__Output, _superverse_v2_RenameTeamSpaceResponse__Output>
}
