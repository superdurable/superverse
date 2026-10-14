// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { AddTeamSpaceMemberRequest as _superverse_v2_AddTeamSpaceMemberRequest, AddTeamSpaceMemberRequest__Output as _superverse_v2_AddTeamSpaceMemberRequest__Output } from '../../superverse/v2/AddTeamSpaceMemberRequest';
import type { AddTeamSpaceMemberResponse as _superverse_v2_AddTeamSpaceMemberResponse, AddTeamSpaceMemberResponse__Output as _superverse_v2_AddTeamSpaceMemberResponse__Output } from '../../superverse/v2/AddTeamSpaceMemberResponse';
import type { ListTeamSpaceMembersRequest as _superverse_v2_ListTeamSpaceMembersRequest, ListTeamSpaceMembersRequest__Output as _superverse_v2_ListTeamSpaceMembersRequest__Output } from '../../superverse/v2/ListTeamSpaceMembersRequest';
import type { ListTeamSpaceMembersResponse as _superverse_v2_ListTeamSpaceMembersResponse, ListTeamSpaceMembersResponse__Output as _superverse_v2_ListTeamSpaceMembersResponse__Output } from '../../superverse/v2/ListTeamSpaceMembersResponse';
import type { RemoveTeamSpaceMemberRequest as _superverse_v2_RemoveTeamSpaceMemberRequest, RemoveTeamSpaceMemberRequest__Output as _superverse_v2_RemoveTeamSpaceMemberRequest__Output } from '../../superverse/v2/RemoveTeamSpaceMemberRequest';
import type { RemoveTeamSpaceMemberResponse as _superverse_v2_RemoveTeamSpaceMemberResponse, RemoveTeamSpaceMemberResponse__Output as _superverse_v2_RemoveTeamSpaceMemberResponse__Output } from '../../superverse/v2/RemoveTeamSpaceMemberResponse';
import type { SetTeamSpaceMemberRoleRequest as _superverse_v2_SetTeamSpaceMemberRoleRequest, SetTeamSpaceMemberRoleRequest__Output as _superverse_v2_SetTeamSpaceMemberRoleRequest__Output } from '../../superverse/v2/SetTeamSpaceMemberRoleRequest';
import type { SetTeamSpaceMemberRoleResponse as _superverse_v2_SetTeamSpaceMemberRoleResponse, SetTeamSpaceMemberRoleResponse__Output as _superverse_v2_SetTeamSpaceMemberRoleResponse__Output } from '../../superverse/v2/SetTeamSpaceMemberRoleResponse';

export interface TeamSpaceMemberServiceClient extends grpc.Client {
  AddTeamSpaceMember(argument: _superverse_v2_AddTeamSpaceMemberRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AddTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  AddTeamSpaceMember(argument: _superverse_v2_AddTeamSpaceMemberRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_AddTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  AddTeamSpaceMember(argument: _superverse_v2_AddTeamSpaceMemberRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AddTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  AddTeamSpaceMember(argument: _superverse_v2_AddTeamSpaceMemberRequest, callback: grpc.requestCallback<_superverse_v2_AddTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  addTeamSpaceMember(argument: _superverse_v2_AddTeamSpaceMemberRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AddTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  addTeamSpaceMember(argument: _superverse_v2_AddTeamSpaceMemberRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_AddTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  addTeamSpaceMember(argument: _superverse_v2_AddTeamSpaceMemberRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AddTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  addTeamSpaceMember(argument: _superverse_v2_AddTeamSpaceMemberRequest, callback: grpc.requestCallback<_superverse_v2_AddTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;

  ListTeamSpaceMembers(argument: _superverse_v2_ListTeamSpaceMembersRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListTeamSpaceMembersResponse__Output>): grpc.ClientUnaryCall;
  ListTeamSpaceMembers(argument: _superverse_v2_ListTeamSpaceMembersRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListTeamSpaceMembersResponse__Output>): grpc.ClientUnaryCall;
  ListTeamSpaceMembers(argument: _superverse_v2_ListTeamSpaceMembersRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListTeamSpaceMembersResponse__Output>): grpc.ClientUnaryCall;
  ListTeamSpaceMembers(argument: _superverse_v2_ListTeamSpaceMembersRequest, callback: grpc.requestCallback<_superverse_v2_ListTeamSpaceMembersResponse__Output>): grpc.ClientUnaryCall;
  listTeamSpaceMembers(argument: _superverse_v2_ListTeamSpaceMembersRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListTeamSpaceMembersResponse__Output>): grpc.ClientUnaryCall;
  listTeamSpaceMembers(argument: _superverse_v2_ListTeamSpaceMembersRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListTeamSpaceMembersResponse__Output>): grpc.ClientUnaryCall;
  listTeamSpaceMembers(argument: _superverse_v2_ListTeamSpaceMembersRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListTeamSpaceMembersResponse__Output>): grpc.ClientUnaryCall;
  listTeamSpaceMembers(argument: _superverse_v2_ListTeamSpaceMembersRequest, callback: grpc.requestCallback<_superverse_v2_ListTeamSpaceMembersResponse__Output>): grpc.ClientUnaryCall;

  RemoveTeamSpaceMember(argument: _superverse_v2_RemoveTeamSpaceMemberRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  RemoveTeamSpaceMember(argument: _superverse_v2_RemoveTeamSpaceMemberRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RemoveTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  RemoveTeamSpaceMember(argument: _superverse_v2_RemoveTeamSpaceMemberRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  RemoveTeamSpaceMember(argument: _superverse_v2_RemoveTeamSpaceMemberRequest, callback: grpc.requestCallback<_superverse_v2_RemoveTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  removeTeamSpaceMember(argument: _superverse_v2_RemoveTeamSpaceMemberRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  removeTeamSpaceMember(argument: _superverse_v2_RemoveTeamSpaceMemberRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RemoveTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  removeTeamSpaceMember(argument: _superverse_v2_RemoveTeamSpaceMemberRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;
  removeTeamSpaceMember(argument: _superverse_v2_RemoveTeamSpaceMemberRequest, callback: grpc.requestCallback<_superverse_v2_RemoveTeamSpaceMemberResponse__Output>): grpc.ClientUnaryCall;

  SetTeamSpaceMemberRole(argument: _superverse_v2_SetTeamSpaceMemberRoleRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetTeamSpaceMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  SetTeamSpaceMemberRole(argument: _superverse_v2_SetTeamSpaceMemberRoleRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SetTeamSpaceMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  SetTeamSpaceMemberRole(argument: _superverse_v2_SetTeamSpaceMemberRoleRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetTeamSpaceMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  SetTeamSpaceMemberRole(argument: _superverse_v2_SetTeamSpaceMemberRoleRequest, callback: grpc.requestCallback<_superverse_v2_SetTeamSpaceMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  setTeamSpaceMemberRole(argument: _superverse_v2_SetTeamSpaceMemberRoleRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetTeamSpaceMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  setTeamSpaceMemberRole(argument: _superverse_v2_SetTeamSpaceMemberRoleRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SetTeamSpaceMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  setTeamSpaceMemberRole(argument: _superverse_v2_SetTeamSpaceMemberRoleRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetTeamSpaceMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  setTeamSpaceMemberRole(argument: _superverse_v2_SetTeamSpaceMemberRoleRequest, callback: grpc.requestCallback<_superverse_v2_SetTeamSpaceMemberRoleResponse__Output>): grpc.ClientUnaryCall;

}

export interface TeamSpaceMemberServiceHandlers extends grpc.UntypedServiceImplementation {
  AddTeamSpaceMember: grpc.handleUnaryCall<_superverse_v2_AddTeamSpaceMemberRequest__Output, _superverse_v2_AddTeamSpaceMemberResponse>;

  ListTeamSpaceMembers: grpc.handleUnaryCall<_superverse_v2_ListTeamSpaceMembersRequest__Output, _superverse_v2_ListTeamSpaceMembersResponse>;

  RemoveTeamSpaceMember: grpc.handleUnaryCall<_superverse_v2_RemoveTeamSpaceMemberRequest__Output, _superverse_v2_RemoveTeamSpaceMemberResponse>;

  SetTeamSpaceMemberRole: grpc.handleUnaryCall<_superverse_v2_SetTeamSpaceMemberRoleRequest__Output, _superverse_v2_SetTeamSpaceMemberRoleResponse>;

}

export interface TeamSpaceMemberServiceDefinition extends grpc.ServiceDefinition {
  AddTeamSpaceMember: MethodDefinition<_superverse_v2_AddTeamSpaceMemberRequest, _superverse_v2_AddTeamSpaceMemberResponse, _superverse_v2_AddTeamSpaceMemberRequest__Output, _superverse_v2_AddTeamSpaceMemberResponse__Output>
  ListTeamSpaceMembers: MethodDefinition<_superverse_v2_ListTeamSpaceMembersRequest, _superverse_v2_ListTeamSpaceMembersResponse, _superverse_v2_ListTeamSpaceMembersRequest__Output, _superverse_v2_ListTeamSpaceMembersResponse__Output>
  RemoveTeamSpaceMember: MethodDefinition<_superverse_v2_RemoveTeamSpaceMemberRequest, _superverse_v2_RemoveTeamSpaceMemberResponse, _superverse_v2_RemoveTeamSpaceMemberRequest__Output, _superverse_v2_RemoveTeamSpaceMemberResponse__Output>
  SetTeamSpaceMemberRole: MethodDefinition<_superverse_v2_SetTeamSpaceMemberRoleRequest, _superverse_v2_SetTeamSpaceMemberRoleResponse, _superverse_v2_SetTeamSpaceMemberRoleRequest__Output, _superverse_v2_SetTeamSpaceMemberRoleResponse__Output>
}
