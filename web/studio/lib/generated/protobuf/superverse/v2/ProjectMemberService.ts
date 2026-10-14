// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { AddProjectMemberRequest as _superverse_v2_AddProjectMemberRequest, AddProjectMemberRequest__Output as _superverse_v2_AddProjectMemberRequest__Output } from '../../superverse/v2/AddProjectMemberRequest';
import type { AddProjectMemberResponse as _superverse_v2_AddProjectMemberResponse, AddProjectMemberResponse__Output as _superverse_v2_AddProjectMemberResponse__Output } from '../../superverse/v2/AddProjectMemberResponse';
import type { ListProjectMembersRequest as _superverse_v2_ListProjectMembersRequest, ListProjectMembersRequest__Output as _superverse_v2_ListProjectMembersRequest__Output } from '../../superverse/v2/ListProjectMembersRequest';
import type { ListProjectMembersResponse as _superverse_v2_ListProjectMembersResponse, ListProjectMembersResponse__Output as _superverse_v2_ListProjectMembersResponse__Output } from '../../superverse/v2/ListProjectMembersResponse';
import type { RemoveProjectMemberRequest as _superverse_v2_RemoveProjectMemberRequest, RemoveProjectMemberRequest__Output as _superverse_v2_RemoveProjectMemberRequest__Output } from '../../superverse/v2/RemoveProjectMemberRequest';
import type { RemoveProjectMemberResponse as _superverse_v2_RemoveProjectMemberResponse, RemoveProjectMemberResponse__Output as _superverse_v2_RemoveProjectMemberResponse__Output } from '../../superverse/v2/RemoveProjectMemberResponse';
import type { SetProjectMemberRoleRequest as _superverse_v2_SetProjectMemberRoleRequest, SetProjectMemberRoleRequest__Output as _superverse_v2_SetProjectMemberRoleRequest__Output } from '../../superverse/v2/SetProjectMemberRoleRequest';
import type { SetProjectMemberRoleResponse as _superverse_v2_SetProjectMemberRoleResponse, SetProjectMemberRoleResponse__Output as _superverse_v2_SetProjectMemberRoleResponse__Output } from '../../superverse/v2/SetProjectMemberRoleResponse';

export interface ProjectMemberServiceClient extends grpc.Client {
  AddProjectMember(argument: _superverse_v2_AddProjectMemberRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AddProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  AddProjectMember(argument: _superverse_v2_AddProjectMemberRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_AddProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  AddProjectMember(argument: _superverse_v2_AddProjectMemberRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AddProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  AddProjectMember(argument: _superverse_v2_AddProjectMemberRequest, callback: grpc.requestCallback<_superverse_v2_AddProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  addProjectMember(argument: _superverse_v2_AddProjectMemberRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AddProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  addProjectMember(argument: _superverse_v2_AddProjectMemberRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_AddProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  addProjectMember(argument: _superverse_v2_AddProjectMemberRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_AddProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  addProjectMember(argument: _superverse_v2_AddProjectMemberRequest, callback: grpc.requestCallback<_superverse_v2_AddProjectMemberResponse__Output>): grpc.ClientUnaryCall;

  ListProjectMembers(argument: _superverse_v2_ListProjectMembersRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListProjectMembersResponse__Output>): grpc.ClientUnaryCall;
  ListProjectMembers(argument: _superverse_v2_ListProjectMembersRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListProjectMembersResponse__Output>): grpc.ClientUnaryCall;
  ListProjectMembers(argument: _superverse_v2_ListProjectMembersRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListProjectMembersResponse__Output>): grpc.ClientUnaryCall;
  ListProjectMembers(argument: _superverse_v2_ListProjectMembersRequest, callback: grpc.requestCallback<_superverse_v2_ListProjectMembersResponse__Output>): grpc.ClientUnaryCall;
  listProjectMembers(argument: _superverse_v2_ListProjectMembersRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListProjectMembersResponse__Output>): grpc.ClientUnaryCall;
  listProjectMembers(argument: _superverse_v2_ListProjectMembersRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListProjectMembersResponse__Output>): grpc.ClientUnaryCall;
  listProjectMembers(argument: _superverse_v2_ListProjectMembersRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListProjectMembersResponse__Output>): grpc.ClientUnaryCall;
  listProjectMembers(argument: _superverse_v2_ListProjectMembersRequest, callback: grpc.requestCallback<_superverse_v2_ListProjectMembersResponse__Output>): grpc.ClientUnaryCall;

  RemoveProjectMember(argument: _superverse_v2_RemoveProjectMemberRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  RemoveProjectMember(argument: _superverse_v2_RemoveProjectMemberRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RemoveProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  RemoveProjectMember(argument: _superverse_v2_RemoveProjectMemberRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  RemoveProjectMember(argument: _superverse_v2_RemoveProjectMemberRequest, callback: grpc.requestCallback<_superverse_v2_RemoveProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  removeProjectMember(argument: _superverse_v2_RemoveProjectMemberRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  removeProjectMember(argument: _superverse_v2_RemoveProjectMemberRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RemoveProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  removeProjectMember(argument: _superverse_v2_RemoveProjectMemberRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveProjectMemberResponse__Output>): grpc.ClientUnaryCall;
  removeProjectMember(argument: _superverse_v2_RemoveProjectMemberRequest, callback: grpc.requestCallback<_superverse_v2_RemoveProjectMemberResponse__Output>): grpc.ClientUnaryCall;

  SetProjectMemberRole(argument: _superverse_v2_SetProjectMemberRoleRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetProjectMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  SetProjectMemberRole(argument: _superverse_v2_SetProjectMemberRoleRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SetProjectMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  SetProjectMemberRole(argument: _superverse_v2_SetProjectMemberRoleRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetProjectMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  SetProjectMemberRole(argument: _superverse_v2_SetProjectMemberRoleRequest, callback: grpc.requestCallback<_superverse_v2_SetProjectMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  setProjectMemberRole(argument: _superverse_v2_SetProjectMemberRoleRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetProjectMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  setProjectMemberRole(argument: _superverse_v2_SetProjectMemberRoleRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SetProjectMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  setProjectMemberRole(argument: _superverse_v2_SetProjectMemberRoleRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SetProjectMemberRoleResponse__Output>): grpc.ClientUnaryCall;
  setProjectMemberRole(argument: _superverse_v2_SetProjectMemberRoleRequest, callback: grpc.requestCallback<_superverse_v2_SetProjectMemberRoleResponse__Output>): grpc.ClientUnaryCall;

}

export interface ProjectMemberServiceHandlers extends grpc.UntypedServiceImplementation {
  AddProjectMember: grpc.handleUnaryCall<_superverse_v2_AddProjectMemberRequest__Output, _superverse_v2_AddProjectMemberResponse>;

  ListProjectMembers: grpc.handleUnaryCall<_superverse_v2_ListProjectMembersRequest__Output, _superverse_v2_ListProjectMembersResponse>;

  RemoveProjectMember: grpc.handleUnaryCall<_superverse_v2_RemoveProjectMemberRequest__Output, _superverse_v2_RemoveProjectMemberResponse>;

  SetProjectMemberRole: grpc.handleUnaryCall<_superverse_v2_SetProjectMemberRoleRequest__Output, _superverse_v2_SetProjectMemberRoleResponse>;

}

export interface ProjectMemberServiceDefinition extends grpc.ServiceDefinition {
  AddProjectMember: MethodDefinition<_superverse_v2_AddProjectMemberRequest, _superverse_v2_AddProjectMemberResponse, _superverse_v2_AddProjectMemberRequest__Output, _superverse_v2_AddProjectMemberResponse__Output>
  ListProjectMembers: MethodDefinition<_superverse_v2_ListProjectMembersRequest, _superverse_v2_ListProjectMembersResponse, _superverse_v2_ListProjectMembersRequest__Output, _superverse_v2_ListProjectMembersResponse__Output>
  RemoveProjectMember: MethodDefinition<_superverse_v2_RemoveProjectMemberRequest, _superverse_v2_RemoveProjectMemberResponse, _superverse_v2_RemoveProjectMemberRequest__Output, _superverse_v2_RemoveProjectMemberResponse__Output>
  SetProjectMemberRole: MethodDefinition<_superverse_v2_SetProjectMemberRoleRequest, _superverse_v2_SetProjectMemberRoleResponse, _superverse_v2_SetProjectMemberRoleRequest__Output, _superverse_v2_SetProjectMemberRoleResponse__Output>
}
