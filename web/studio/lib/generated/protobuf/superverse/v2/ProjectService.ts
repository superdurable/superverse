// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { CreateProjectRequest as _superverse_v2_CreateProjectRequest, CreateProjectRequest__Output as _superverse_v2_CreateProjectRequest__Output } from '../../superverse/v2/CreateProjectRequest';
import type { CreateProjectResponse as _superverse_v2_CreateProjectResponse, CreateProjectResponse__Output as _superverse_v2_CreateProjectResponse__Output } from '../../superverse/v2/CreateProjectResponse';
import type { DeleteProjectRequest as _superverse_v2_DeleteProjectRequest, DeleteProjectRequest__Output as _superverse_v2_DeleteProjectRequest__Output } from '../../superverse/v2/DeleteProjectRequest';
import type { DeleteProjectResponse as _superverse_v2_DeleteProjectResponse, DeleteProjectResponse__Output as _superverse_v2_DeleteProjectResponse__Output } from '../../superverse/v2/DeleteProjectResponse';
import type { GetProjectConfigurationRequest as _superverse_v2_GetProjectConfigurationRequest, GetProjectConfigurationRequest__Output as _superverse_v2_GetProjectConfigurationRequest__Output } from '../../superverse/v2/GetProjectConfigurationRequest';
import type { GetProjectConfigurationResponse as _superverse_v2_GetProjectConfigurationResponse, GetProjectConfigurationResponse__Output as _superverse_v2_GetProjectConfigurationResponse__Output } from '../../superverse/v2/GetProjectConfigurationResponse';
import type { GetProjectRequest as _superverse_v2_GetProjectRequest, GetProjectRequest__Output as _superverse_v2_GetProjectRequest__Output } from '../../superverse/v2/GetProjectRequest';
import type { GetProjectResponse as _superverse_v2_GetProjectResponse, GetProjectResponse__Output as _superverse_v2_GetProjectResponse__Output } from '../../superverse/v2/GetProjectResponse';
import type { ListEnvironmentEventsRequest as _superverse_v2_ListEnvironmentEventsRequest, ListEnvironmentEventsRequest__Output as _superverse_v2_ListEnvironmentEventsRequest__Output } from '../../superverse/v2/ListEnvironmentEventsRequest';
import type { ListEnvironmentEventsResponse as _superverse_v2_ListEnvironmentEventsResponse, ListEnvironmentEventsResponse__Output as _superverse_v2_ListEnvironmentEventsResponse__Output } from '../../superverse/v2/ListEnvironmentEventsResponse';
import type { ListProjectsRequest as _superverse_v2_ListProjectsRequest, ListProjectsRequest__Output as _superverse_v2_ListProjectsRequest__Output } from '../../superverse/v2/ListProjectsRequest';
import type { ListProjectsResponse as _superverse_v2_ListProjectsResponse, ListProjectsResponse__Output as _superverse_v2_ListProjectsResponse__Output } from '../../superverse/v2/ListProjectsResponse';
import type { ListRecentTriggerEventsRequest as _superverse_v2_ListRecentTriggerEventsRequest, ListRecentTriggerEventsRequest__Output as _superverse_v2_ListRecentTriggerEventsRequest__Output } from '../../superverse/v2/ListRecentTriggerEventsRequest';
import type { ListRecentTriggerEventsResponse as _superverse_v2_ListRecentTriggerEventsResponse, ListRecentTriggerEventsResponse__Output as _superverse_v2_ListRecentTriggerEventsResponse__Output } from '../../superverse/v2/ListRecentTriggerEventsResponse';
import type { RemoveLiveRequest as _superverse_v2_RemoveLiveRequest, RemoveLiveRequest__Output as _superverse_v2_RemoveLiveRequest__Output } from '../../superverse/v2/RemoveLiveRequest';
import type { RemoveLiveResponse as _superverse_v2_RemoveLiveResponse, RemoveLiveResponse__Output as _superverse_v2_RemoveLiveResponse__Output } from '../../superverse/v2/RemoveLiveResponse';
import type { ValidateProjectConfigurationRequest as _superverse_v2_ValidateProjectConfigurationRequest, ValidateProjectConfigurationRequest__Output as _superverse_v2_ValidateProjectConfigurationRequest__Output } from '../../superverse/v2/ValidateProjectConfigurationRequest';
import type { ValidateProjectConfigurationResponse as _superverse_v2_ValidateProjectConfigurationResponse, ValidateProjectConfigurationResponse__Output as _superverse_v2_ValidateProjectConfigurationResponse__Output } from '../../superverse/v2/ValidateProjectConfigurationResponse';
import type { WatchProjectRequest as _superverse_v2_WatchProjectRequest, WatchProjectRequest__Output as _superverse_v2_WatchProjectRequest__Output } from '../../superverse/v2/WatchProjectRequest';
import type { WatchProjectResponse as _superverse_v2_WatchProjectResponse, WatchProjectResponse__Output as _superverse_v2_WatchProjectResponse__Output } from '../../superverse/v2/WatchProjectResponse';

export interface ProjectServiceClient extends grpc.Client {
  CreateProject(argument: _superverse_v2_CreateProjectRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateProjectResponse__Output>): grpc.ClientUnaryCall;
  CreateProject(argument: _superverse_v2_CreateProjectRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CreateProjectResponse__Output>): grpc.ClientUnaryCall;
  CreateProject(argument: _superverse_v2_CreateProjectRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateProjectResponse__Output>): grpc.ClientUnaryCall;
  CreateProject(argument: _superverse_v2_CreateProjectRequest, callback: grpc.requestCallback<_superverse_v2_CreateProjectResponse__Output>): grpc.ClientUnaryCall;
  createProject(argument: _superverse_v2_CreateProjectRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateProjectResponse__Output>): grpc.ClientUnaryCall;
  createProject(argument: _superverse_v2_CreateProjectRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CreateProjectResponse__Output>): grpc.ClientUnaryCall;
  createProject(argument: _superverse_v2_CreateProjectRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CreateProjectResponse__Output>): grpc.ClientUnaryCall;
  createProject(argument: _superverse_v2_CreateProjectRequest, callback: grpc.requestCallback<_superverse_v2_CreateProjectResponse__Output>): grpc.ClientUnaryCall;

  DeleteProject(argument: _superverse_v2_DeleteProjectRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteProjectResponse__Output>): grpc.ClientUnaryCall;
  DeleteProject(argument: _superverse_v2_DeleteProjectRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteProjectResponse__Output>): grpc.ClientUnaryCall;
  DeleteProject(argument: _superverse_v2_DeleteProjectRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteProjectResponse__Output>): grpc.ClientUnaryCall;
  DeleteProject(argument: _superverse_v2_DeleteProjectRequest, callback: grpc.requestCallback<_superverse_v2_DeleteProjectResponse__Output>): grpc.ClientUnaryCall;
  deleteProject(argument: _superverse_v2_DeleteProjectRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteProjectResponse__Output>): grpc.ClientUnaryCall;
  deleteProject(argument: _superverse_v2_DeleteProjectRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeleteProjectResponse__Output>): grpc.ClientUnaryCall;
  deleteProject(argument: _superverse_v2_DeleteProjectRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeleteProjectResponse__Output>): grpc.ClientUnaryCall;
  deleteProject(argument: _superverse_v2_DeleteProjectRequest, callback: grpc.requestCallback<_superverse_v2_DeleteProjectResponse__Output>): grpc.ClientUnaryCall;

  GetProject(argument: _superverse_v2_GetProjectRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetProjectResponse__Output>): grpc.ClientUnaryCall;
  GetProject(argument: _superverse_v2_GetProjectRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetProjectResponse__Output>): grpc.ClientUnaryCall;
  GetProject(argument: _superverse_v2_GetProjectRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetProjectResponse__Output>): grpc.ClientUnaryCall;
  GetProject(argument: _superverse_v2_GetProjectRequest, callback: grpc.requestCallback<_superverse_v2_GetProjectResponse__Output>): grpc.ClientUnaryCall;
  getProject(argument: _superverse_v2_GetProjectRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetProjectResponse__Output>): grpc.ClientUnaryCall;
  getProject(argument: _superverse_v2_GetProjectRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetProjectResponse__Output>): grpc.ClientUnaryCall;
  getProject(argument: _superverse_v2_GetProjectRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetProjectResponse__Output>): grpc.ClientUnaryCall;
  getProject(argument: _superverse_v2_GetProjectRequest, callback: grpc.requestCallback<_superverse_v2_GetProjectResponse__Output>): grpc.ClientUnaryCall;

  GetProjectConfiguration(argument: _superverse_v2_GetProjectConfigurationRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  GetProjectConfiguration(argument: _superverse_v2_GetProjectConfigurationRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  GetProjectConfiguration(argument: _superverse_v2_GetProjectConfigurationRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  GetProjectConfiguration(argument: _superverse_v2_GetProjectConfigurationRequest, callback: grpc.requestCallback<_superverse_v2_GetProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  getProjectConfiguration(argument: _superverse_v2_GetProjectConfigurationRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  getProjectConfiguration(argument: _superverse_v2_GetProjectConfigurationRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  getProjectConfiguration(argument: _superverse_v2_GetProjectConfigurationRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  getProjectConfiguration(argument: _superverse_v2_GetProjectConfigurationRequest, callback: grpc.requestCallback<_superverse_v2_GetProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;

  ListEnvironmentEvents(argument: _superverse_v2_ListEnvironmentEventsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListEnvironmentEventsResponse__Output>): grpc.ClientUnaryCall;
  ListEnvironmentEvents(argument: _superverse_v2_ListEnvironmentEventsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListEnvironmentEventsResponse__Output>): grpc.ClientUnaryCall;
  ListEnvironmentEvents(argument: _superverse_v2_ListEnvironmentEventsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListEnvironmentEventsResponse__Output>): grpc.ClientUnaryCall;
  ListEnvironmentEvents(argument: _superverse_v2_ListEnvironmentEventsRequest, callback: grpc.requestCallback<_superverse_v2_ListEnvironmentEventsResponse__Output>): grpc.ClientUnaryCall;
  listEnvironmentEvents(argument: _superverse_v2_ListEnvironmentEventsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListEnvironmentEventsResponse__Output>): grpc.ClientUnaryCall;
  listEnvironmentEvents(argument: _superverse_v2_ListEnvironmentEventsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListEnvironmentEventsResponse__Output>): grpc.ClientUnaryCall;
  listEnvironmentEvents(argument: _superverse_v2_ListEnvironmentEventsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListEnvironmentEventsResponse__Output>): grpc.ClientUnaryCall;
  listEnvironmentEvents(argument: _superverse_v2_ListEnvironmentEventsRequest, callback: grpc.requestCallback<_superverse_v2_ListEnvironmentEventsResponse__Output>): grpc.ClientUnaryCall;

  ListProjects(argument: _superverse_v2_ListProjectsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListProjectsResponse__Output>): grpc.ClientUnaryCall;
  ListProjects(argument: _superverse_v2_ListProjectsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListProjectsResponse__Output>): grpc.ClientUnaryCall;
  ListProjects(argument: _superverse_v2_ListProjectsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListProjectsResponse__Output>): grpc.ClientUnaryCall;
  ListProjects(argument: _superverse_v2_ListProjectsRequest, callback: grpc.requestCallback<_superverse_v2_ListProjectsResponse__Output>): grpc.ClientUnaryCall;
  listProjects(argument: _superverse_v2_ListProjectsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListProjectsResponse__Output>): grpc.ClientUnaryCall;
  listProjects(argument: _superverse_v2_ListProjectsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListProjectsResponse__Output>): grpc.ClientUnaryCall;
  listProjects(argument: _superverse_v2_ListProjectsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListProjectsResponse__Output>): grpc.ClientUnaryCall;
  listProjects(argument: _superverse_v2_ListProjectsRequest, callback: grpc.requestCallback<_superverse_v2_ListProjectsResponse__Output>): grpc.ClientUnaryCall;

  ListRecentTriggerEvents(argument: _superverse_v2_ListRecentTriggerEventsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListRecentTriggerEventsResponse__Output>): grpc.ClientUnaryCall;
  ListRecentTriggerEvents(argument: _superverse_v2_ListRecentTriggerEventsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListRecentTriggerEventsResponse__Output>): grpc.ClientUnaryCall;
  ListRecentTriggerEvents(argument: _superverse_v2_ListRecentTriggerEventsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListRecentTriggerEventsResponse__Output>): grpc.ClientUnaryCall;
  ListRecentTriggerEvents(argument: _superverse_v2_ListRecentTriggerEventsRequest, callback: grpc.requestCallback<_superverse_v2_ListRecentTriggerEventsResponse__Output>): grpc.ClientUnaryCall;
  listRecentTriggerEvents(argument: _superverse_v2_ListRecentTriggerEventsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListRecentTriggerEventsResponse__Output>): grpc.ClientUnaryCall;
  listRecentTriggerEvents(argument: _superverse_v2_ListRecentTriggerEventsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListRecentTriggerEventsResponse__Output>): grpc.ClientUnaryCall;
  listRecentTriggerEvents(argument: _superverse_v2_ListRecentTriggerEventsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListRecentTriggerEventsResponse__Output>): grpc.ClientUnaryCall;
  listRecentTriggerEvents(argument: _superverse_v2_ListRecentTriggerEventsRequest, callback: grpc.requestCallback<_superverse_v2_ListRecentTriggerEventsResponse__Output>): grpc.ClientUnaryCall;

  RemoveLive(argument: _superverse_v2_RemoveLiveRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveLiveResponse__Output>): grpc.ClientUnaryCall;
  RemoveLive(argument: _superverse_v2_RemoveLiveRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RemoveLiveResponse__Output>): grpc.ClientUnaryCall;
  RemoveLive(argument: _superverse_v2_RemoveLiveRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveLiveResponse__Output>): grpc.ClientUnaryCall;
  RemoveLive(argument: _superverse_v2_RemoveLiveRequest, callback: grpc.requestCallback<_superverse_v2_RemoveLiveResponse__Output>): grpc.ClientUnaryCall;
  removeLive(argument: _superverse_v2_RemoveLiveRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveLiveResponse__Output>): grpc.ClientUnaryCall;
  removeLive(argument: _superverse_v2_RemoveLiveRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RemoveLiveResponse__Output>): grpc.ClientUnaryCall;
  removeLive(argument: _superverse_v2_RemoveLiveRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RemoveLiveResponse__Output>): grpc.ClientUnaryCall;
  removeLive(argument: _superverse_v2_RemoveLiveRequest, callback: grpc.requestCallback<_superverse_v2_RemoveLiveResponse__Output>): grpc.ClientUnaryCall;

  ValidateProjectConfiguration(argument: _superverse_v2_ValidateProjectConfigurationRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ValidateProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  ValidateProjectConfiguration(argument: _superverse_v2_ValidateProjectConfigurationRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ValidateProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  ValidateProjectConfiguration(argument: _superverse_v2_ValidateProjectConfigurationRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ValidateProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  ValidateProjectConfiguration(argument: _superverse_v2_ValidateProjectConfigurationRequest, callback: grpc.requestCallback<_superverse_v2_ValidateProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  validateProjectConfiguration(argument: _superverse_v2_ValidateProjectConfigurationRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ValidateProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  validateProjectConfiguration(argument: _superverse_v2_ValidateProjectConfigurationRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ValidateProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  validateProjectConfiguration(argument: _superverse_v2_ValidateProjectConfigurationRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ValidateProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;
  validateProjectConfiguration(argument: _superverse_v2_ValidateProjectConfigurationRequest, callback: grpc.requestCallback<_superverse_v2_ValidateProjectConfigurationResponse__Output>): grpc.ClientUnaryCall;

  WatchProject(argument: _superverse_v2_WatchProjectRequest, metadata: grpc.Metadata, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchProjectResponse__Output>;
  WatchProject(argument: _superverse_v2_WatchProjectRequest, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchProjectResponse__Output>;
  watchProject(argument: _superverse_v2_WatchProjectRequest, metadata: grpc.Metadata, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchProjectResponse__Output>;
  watchProject(argument: _superverse_v2_WatchProjectRequest, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchProjectResponse__Output>;

}

export interface ProjectServiceHandlers extends grpc.UntypedServiceImplementation {
  CreateProject: grpc.handleUnaryCall<_superverse_v2_CreateProjectRequest__Output, _superverse_v2_CreateProjectResponse>;

  DeleteProject: grpc.handleUnaryCall<_superverse_v2_DeleteProjectRequest__Output, _superverse_v2_DeleteProjectResponse>;

  GetProject: grpc.handleUnaryCall<_superverse_v2_GetProjectRequest__Output, _superverse_v2_GetProjectResponse>;

  GetProjectConfiguration: grpc.handleUnaryCall<_superverse_v2_GetProjectConfigurationRequest__Output, _superverse_v2_GetProjectConfigurationResponse>;

  ListEnvironmentEvents: grpc.handleUnaryCall<_superverse_v2_ListEnvironmentEventsRequest__Output, _superverse_v2_ListEnvironmentEventsResponse>;

  ListProjects: grpc.handleUnaryCall<_superverse_v2_ListProjectsRequest__Output, _superverse_v2_ListProjectsResponse>;

  ListRecentTriggerEvents: grpc.handleUnaryCall<_superverse_v2_ListRecentTriggerEventsRequest__Output, _superverse_v2_ListRecentTriggerEventsResponse>;

  RemoveLive: grpc.handleUnaryCall<_superverse_v2_RemoveLiveRequest__Output, _superverse_v2_RemoveLiveResponse>;

  ValidateProjectConfiguration: grpc.handleUnaryCall<_superverse_v2_ValidateProjectConfigurationRequest__Output, _superverse_v2_ValidateProjectConfigurationResponse>;

  WatchProject: grpc.handleServerStreamingCall<_superverse_v2_WatchProjectRequest__Output, _superverse_v2_WatchProjectResponse>;

}

export interface ProjectServiceDefinition extends grpc.ServiceDefinition {
  CreateProject: MethodDefinition<_superverse_v2_CreateProjectRequest, _superverse_v2_CreateProjectResponse, _superverse_v2_CreateProjectRequest__Output, _superverse_v2_CreateProjectResponse__Output>
  DeleteProject: MethodDefinition<_superverse_v2_DeleteProjectRequest, _superverse_v2_DeleteProjectResponse, _superverse_v2_DeleteProjectRequest__Output, _superverse_v2_DeleteProjectResponse__Output>
  GetProject: MethodDefinition<_superverse_v2_GetProjectRequest, _superverse_v2_GetProjectResponse, _superverse_v2_GetProjectRequest__Output, _superverse_v2_GetProjectResponse__Output>
  GetProjectConfiguration: MethodDefinition<_superverse_v2_GetProjectConfigurationRequest, _superverse_v2_GetProjectConfigurationResponse, _superverse_v2_GetProjectConfigurationRequest__Output, _superverse_v2_GetProjectConfigurationResponse__Output>
  ListEnvironmentEvents: MethodDefinition<_superverse_v2_ListEnvironmentEventsRequest, _superverse_v2_ListEnvironmentEventsResponse, _superverse_v2_ListEnvironmentEventsRequest__Output, _superverse_v2_ListEnvironmentEventsResponse__Output>
  ListProjects: MethodDefinition<_superverse_v2_ListProjectsRequest, _superverse_v2_ListProjectsResponse, _superverse_v2_ListProjectsRequest__Output, _superverse_v2_ListProjectsResponse__Output>
  ListRecentTriggerEvents: MethodDefinition<_superverse_v2_ListRecentTriggerEventsRequest, _superverse_v2_ListRecentTriggerEventsResponse, _superverse_v2_ListRecentTriggerEventsRequest__Output, _superverse_v2_ListRecentTriggerEventsResponse__Output>
  RemoveLive: MethodDefinition<_superverse_v2_RemoveLiveRequest, _superverse_v2_RemoveLiveResponse, _superverse_v2_RemoveLiveRequest__Output, _superverse_v2_RemoveLiveResponse__Output>
  ValidateProjectConfiguration: MethodDefinition<_superverse_v2_ValidateProjectConfigurationRequest, _superverse_v2_ValidateProjectConfigurationResponse, _superverse_v2_ValidateProjectConfigurationRequest__Output, _superverse_v2_ValidateProjectConfigurationResponse__Output>
  WatchProject: MethodDefinition<_superverse_v2_WatchProjectRequest, _superverse_v2_WatchProjectResponse, _superverse_v2_WatchProjectRequest__Output, _superverse_v2_WatchProjectResponse__Output>
}
