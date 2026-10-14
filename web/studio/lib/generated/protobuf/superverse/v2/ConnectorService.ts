// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { InvokeProviderCommandRequest as _superverse_v2_InvokeProviderCommandRequest, InvokeProviderCommandRequest__Output as _superverse_v2_InvokeProviderCommandRequest__Output } from '../../superverse/v2/InvokeProviderCommandRequest';
import type { InvokeProviderCommandResponse as _superverse_v2_InvokeProviderCommandResponse, InvokeProviderCommandResponse__Output as _superverse_v2_InvokeProviderCommandResponse__Output } from '../../superverse/v2/InvokeProviderCommandResponse';
import type { ListConnectionsRequest as _superverse_v2_ListConnectionsRequest, ListConnectionsRequest__Output as _superverse_v2_ListConnectionsRequest__Output } from '../../superverse/v2/ListConnectionsRequest';
import type { ListConnectionsResponse as _superverse_v2_ListConnectionsResponse, ListConnectionsResponse__Output as _superverse_v2_ListConnectionsResponse__Output } from '../../superverse/v2/ListConnectionsResponse';
import type { ReleaseUnusedConnectionsRequest as _superverse_v2_ReleaseUnusedConnectionsRequest, ReleaseUnusedConnectionsRequest__Output as _superverse_v2_ReleaseUnusedConnectionsRequest__Output } from '../../superverse/v2/ReleaseUnusedConnectionsRequest';
import type { ReleaseUnusedConnectionsResponse as _superverse_v2_ReleaseUnusedConnectionsResponse, ReleaseUnusedConnectionsResponse__Output as _superverse_v2_ReleaseUnusedConnectionsResponse__Output } from '../../superverse/v2/ReleaseUnusedConnectionsResponse';
import type { SaveOperationConfigurationRequest as _superverse_v2_SaveOperationConfigurationRequest, SaveOperationConfigurationRequest__Output as _superverse_v2_SaveOperationConfigurationRequest__Output } from '../../superverse/v2/SaveOperationConfigurationRequest';
import type { SaveOperationConfigurationResponse as _superverse_v2_SaveOperationConfigurationResponse, SaveOperationConfigurationResponse__Output as _superverse_v2_SaveOperationConfigurationResponse__Output } from '../../superverse/v2/SaveOperationConfigurationResponse';
import type { SaveTriggerBindingRequest as _superverse_v2_SaveTriggerBindingRequest, SaveTriggerBindingRequest__Output as _superverse_v2_SaveTriggerBindingRequest__Output } from '../../superverse/v2/SaveTriggerBindingRequest';
import type { SaveTriggerBindingResponse as _superverse_v2_SaveTriggerBindingResponse, SaveTriggerBindingResponse__Output as _superverse_v2_SaveTriggerBindingResponse__Output } from '../../superverse/v2/SaveTriggerBindingResponse';
import type { SelectTeamSpaceConnectionRequest as _superverse_v2_SelectTeamSpaceConnectionRequest, SelectTeamSpaceConnectionRequest__Output as _superverse_v2_SelectTeamSpaceConnectionRequest__Output } from '../../superverse/v2/SelectTeamSpaceConnectionRequest';
import type { SelectTeamSpaceConnectionResponse as _superverse_v2_SelectTeamSpaceConnectionResponse, SelectTeamSpaceConnectionResponse__Output as _superverse_v2_SelectTeamSpaceConnectionResponse__Output } from '../../superverse/v2/SelectTeamSpaceConnectionResponse';
import type { StartConnectorUiSessionRequest as _superverse_v2_StartConnectorUiSessionRequest, StartConnectorUiSessionRequest__Output as _superverse_v2_StartConnectorUiSessionRequest__Output } from '../../superverse/v2/StartConnectorUiSessionRequest';
import type { StartConnectorUiSessionResponse as _superverse_v2_StartConnectorUiSessionResponse, StartConnectorUiSessionResponse__Output as _superverse_v2_StartConnectorUiSessionResponse__Output } from '../../superverse/v2/StartConnectorUiSessionResponse';

export interface ConnectorServiceClient extends grpc.Client {
  InvokeProviderCommand(argument: _superverse_v2_InvokeProviderCommandRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InvokeProviderCommandResponse__Output>): grpc.ClientUnaryCall;
  InvokeProviderCommand(argument: _superverse_v2_InvokeProviderCommandRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_InvokeProviderCommandResponse__Output>): grpc.ClientUnaryCall;
  InvokeProviderCommand(argument: _superverse_v2_InvokeProviderCommandRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InvokeProviderCommandResponse__Output>): grpc.ClientUnaryCall;
  InvokeProviderCommand(argument: _superverse_v2_InvokeProviderCommandRequest, callback: grpc.requestCallback<_superverse_v2_InvokeProviderCommandResponse__Output>): grpc.ClientUnaryCall;
  invokeProviderCommand(argument: _superverse_v2_InvokeProviderCommandRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InvokeProviderCommandResponse__Output>): grpc.ClientUnaryCall;
  invokeProviderCommand(argument: _superverse_v2_InvokeProviderCommandRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_InvokeProviderCommandResponse__Output>): grpc.ClientUnaryCall;
  invokeProviderCommand(argument: _superverse_v2_InvokeProviderCommandRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_InvokeProviderCommandResponse__Output>): grpc.ClientUnaryCall;
  invokeProviderCommand(argument: _superverse_v2_InvokeProviderCommandRequest, callback: grpc.requestCallback<_superverse_v2_InvokeProviderCommandResponse__Output>): grpc.ClientUnaryCall;

  ListConnections(argument: _superverse_v2_ListConnectionsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListConnectionsResponse__Output>): grpc.ClientUnaryCall;
  ListConnections(argument: _superverse_v2_ListConnectionsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListConnectionsResponse__Output>): grpc.ClientUnaryCall;
  ListConnections(argument: _superverse_v2_ListConnectionsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListConnectionsResponse__Output>): grpc.ClientUnaryCall;
  ListConnections(argument: _superverse_v2_ListConnectionsRequest, callback: grpc.requestCallback<_superverse_v2_ListConnectionsResponse__Output>): grpc.ClientUnaryCall;
  listConnections(argument: _superverse_v2_ListConnectionsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListConnectionsResponse__Output>): grpc.ClientUnaryCall;
  listConnections(argument: _superverse_v2_ListConnectionsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListConnectionsResponse__Output>): grpc.ClientUnaryCall;
  listConnections(argument: _superverse_v2_ListConnectionsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListConnectionsResponse__Output>): grpc.ClientUnaryCall;
  listConnections(argument: _superverse_v2_ListConnectionsRequest, callback: grpc.requestCallback<_superverse_v2_ListConnectionsResponse__Output>): grpc.ClientUnaryCall;

  ReleaseUnusedConnections(argument: _superverse_v2_ReleaseUnusedConnectionsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ReleaseUnusedConnectionsResponse__Output>): grpc.ClientUnaryCall;
  ReleaseUnusedConnections(argument: _superverse_v2_ReleaseUnusedConnectionsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ReleaseUnusedConnectionsResponse__Output>): grpc.ClientUnaryCall;
  ReleaseUnusedConnections(argument: _superverse_v2_ReleaseUnusedConnectionsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ReleaseUnusedConnectionsResponse__Output>): grpc.ClientUnaryCall;
  ReleaseUnusedConnections(argument: _superverse_v2_ReleaseUnusedConnectionsRequest, callback: grpc.requestCallback<_superverse_v2_ReleaseUnusedConnectionsResponse__Output>): grpc.ClientUnaryCall;
  releaseUnusedConnections(argument: _superverse_v2_ReleaseUnusedConnectionsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ReleaseUnusedConnectionsResponse__Output>): grpc.ClientUnaryCall;
  releaseUnusedConnections(argument: _superverse_v2_ReleaseUnusedConnectionsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ReleaseUnusedConnectionsResponse__Output>): grpc.ClientUnaryCall;
  releaseUnusedConnections(argument: _superverse_v2_ReleaseUnusedConnectionsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ReleaseUnusedConnectionsResponse__Output>): grpc.ClientUnaryCall;
  releaseUnusedConnections(argument: _superverse_v2_ReleaseUnusedConnectionsRequest, callback: grpc.requestCallback<_superverse_v2_ReleaseUnusedConnectionsResponse__Output>): grpc.ClientUnaryCall;

  SaveOperationConfiguration(argument: _superverse_v2_SaveOperationConfigurationRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SaveOperationConfigurationResponse__Output>): grpc.ClientUnaryCall;
  SaveOperationConfiguration(argument: _superverse_v2_SaveOperationConfigurationRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SaveOperationConfigurationResponse__Output>): grpc.ClientUnaryCall;
  SaveOperationConfiguration(argument: _superverse_v2_SaveOperationConfigurationRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SaveOperationConfigurationResponse__Output>): grpc.ClientUnaryCall;
  SaveOperationConfiguration(argument: _superverse_v2_SaveOperationConfigurationRequest, callback: grpc.requestCallback<_superverse_v2_SaveOperationConfigurationResponse__Output>): grpc.ClientUnaryCall;
  saveOperationConfiguration(argument: _superverse_v2_SaveOperationConfigurationRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SaveOperationConfigurationResponse__Output>): grpc.ClientUnaryCall;
  saveOperationConfiguration(argument: _superverse_v2_SaveOperationConfigurationRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SaveOperationConfigurationResponse__Output>): grpc.ClientUnaryCall;
  saveOperationConfiguration(argument: _superverse_v2_SaveOperationConfigurationRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SaveOperationConfigurationResponse__Output>): grpc.ClientUnaryCall;
  saveOperationConfiguration(argument: _superverse_v2_SaveOperationConfigurationRequest, callback: grpc.requestCallback<_superverse_v2_SaveOperationConfigurationResponse__Output>): grpc.ClientUnaryCall;

  SaveTriggerBinding(argument: _superverse_v2_SaveTriggerBindingRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SaveTriggerBindingResponse__Output>): grpc.ClientUnaryCall;
  SaveTriggerBinding(argument: _superverse_v2_SaveTriggerBindingRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SaveTriggerBindingResponse__Output>): grpc.ClientUnaryCall;
  SaveTriggerBinding(argument: _superverse_v2_SaveTriggerBindingRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SaveTriggerBindingResponse__Output>): grpc.ClientUnaryCall;
  SaveTriggerBinding(argument: _superverse_v2_SaveTriggerBindingRequest, callback: grpc.requestCallback<_superverse_v2_SaveTriggerBindingResponse__Output>): grpc.ClientUnaryCall;
  saveTriggerBinding(argument: _superverse_v2_SaveTriggerBindingRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SaveTriggerBindingResponse__Output>): grpc.ClientUnaryCall;
  saveTriggerBinding(argument: _superverse_v2_SaveTriggerBindingRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SaveTriggerBindingResponse__Output>): grpc.ClientUnaryCall;
  saveTriggerBinding(argument: _superverse_v2_SaveTriggerBindingRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SaveTriggerBindingResponse__Output>): grpc.ClientUnaryCall;
  saveTriggerBinding(argument: _superverse_v2_SaveTriggerBindingRequest, callback: grpc.requestCallback<_superverse_v2_SaveTriggerBindingResponse__Output>): grpc.ClientUnaryCall;

  SelectTeamSpaceConnection(argument: _superverse_v2_SelectTeamSpaceConnectionRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SelectTeamSpaceConnectionResponse__Output>): grpc.ClientUnaryCall;
  SelectTeamSpaceConnection(argument: _superverse_v2_SelectTeamSpaceConnectionRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SelectTeamSpaceConnectionResponse__Output>): grpc.ClientUnaryCall;
  SelectTeamSpaceConnection(argument: _superverse_v2_SelectTeamSpaceConnectionRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SelectTeamSpaceConnectionResponse__Output>): grpc.ClientUnaryCall;
  SelectTeamSpaceConnection(argument: _superverse_v2_SelectTeamSpaceConnectionRequest, callback: grpc.requestCallback<_superverse_v2_SelectTeamSpaceConnectionResponse__Output>): grpc.ClientUnaryCall;
  selectTeamSpaceConnection(argument: _superverse_v2_SelectTeamSpaceConnectionRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SelectTeamSpaceConnectionResponse__Output>): grpc.ClientUnaryCall;
  selectTeamSpaceConnection(argument: _superverse_v2_SelectTeamSpaceConnectionRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_SelectTeamSpaceConnectionResponse__Output>): grpc.ClientUnaryCall;
  selectTeamSpaceConnection(argument: _superverse_v2_SelectTeamSpaceConnectionRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_SelectTeamSpaceConnectionResponse__Output>): grpc.ClientUnaryCall;
  selectTeamSpaceConnection(argument: _superverse_v2_SelectTeamSpaceConnectionRequest, callback: grpc.requestCallback<_superverse_v2_SelectTeamSpaceConnectionResponse__Output>): grpc.ClientUnaryCall;

  StartConnectorUiSession(argument: _superverse_v2_StartConnectorUiSessionRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartConnectorUiSessionResponse__Output>): grpc.ClientUnaryCall;
  StartConnectorUiSession(argument: _superverse_v2_StartConnectorUiSessionRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartConnectorUiSessionResponse__Output>): grpc.ClientUnaryCall;
  StartConnectorUiSession(argument: _superverse_v2_StartConnectorUiSessionRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartConnectorUiSessionResponse__Output>): grpc.ClientUnaryCall;
  StartConnectorUiSession(argument: _superverse_v2_StartConnectorUiSessionRequest, callback: grpc.requestCallback<_superverse_v2_StartConnectorUiSessionResponse__Output>): grpc.ClientUnaryCall;
  startConnectorUiSession(argument: _superverse_v2_StartConnectorUiSessionRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartConnectorUiSessionResponse__Output>): grpc.ClientUnaryCall;
  startConnectorUiSession(argument: _superverse_v2_StartConnectorUiSessionRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartConnectorUiSessionResponse__Output>): grpc.ClientUnaryCall;
  startConnectorUiSession(argument: _superverse_v2_StartConnectorUiSessionRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartConnectorUiSessionResponse__Output>): grpc.ClientUnaryCall;
  startConnectorUiSession(argument: _superverse_v2_StartConnectorUiSessionRequest, callback: grpc.requestCallback<_superverse_v2_StartConnectorUiSessionResponse__Output>): grpc.ClientUnaryCall;

}

export interface ConnectorServiceHandlers extends grpc.UntypedServiceImplementation {
  InvokeProviderCommand: grpc.handleUnaryCall<_superverse_v2_InvokeProviderCommandRequest__Output, _superverse_v2_InvokeProviderCommandResponse>;

  ListConnections: grpc.handleUnaryCall<_superverse_v2_ListConnectionsRequest__Output, _superverse_v2_ListConnectionsResponse>;

  ReleaseUnusedConnections: grpc.handleUnaryCall<_superverse_v2_ReleaseUnusedConnectionsRequest__Output, _superverse_v2_ReleaseUnusedConnectionsResponse>;

  SaveOperationConfiguration: grpc.handleUnaryCall<_superverse_v2_SaveOperationConfigurationRequest__Output, _superverse_v2_SaveOperationConfigurationResponse>;

  SaveTriggerBinding: grpc.handleUnaryCall<_superverse_v2_SaveTriggerBindingRequest__Output, _superverse_v2_SaveTriggerBindingResponse>;

  SelectTeamSpaceConnection: grpc.handleUnaryCall<_superverse_v2_SelectTeamSpaceConnectionRequest__Output, _superverse_v2_SelectTeamSpaceConnectionResponse>;

  StartConnectorUiSession: grpc.handleUnaryCall<_superverse_v2_StartConnectorUiSessionRequest__Output, _superverse_v2_StartConnectorUiSessionResponse>;

}

export interface ConnectorServiceDefinition extends grpc.ServiceDefinition {
  InvokeProviderCommand: MethodDefinition<_superverse_v2_InvokeProviderCommandRequest, _superverse_v2_InvokeProviderCommandResponse, _superverse_v2_InvokeProviderCommandRequest__Output, _superverse_v2_InvokeProviderCommandResponse__Output>
  ListConnections: MethodDefinition<_superverse_v2_ListConnectionsRequest, _superverse_v2_ListConnectionsResponse, _superverse_v2_ListConnectionsRequest__Output, _superverse_v2_ListConnectionsResponse__Output>
  ReleaseUnusedConnections: MethodDefinition<_superverse_v2_ReleaseUnusedConnectionsRequest, _superverse_v2_ReleaseUnusedConnectionsResponse, _superverse_v2_ReleaseUnusedConnectionsRequest__Output, _superverse_v2_ReleaseUnusedConnectionsResponse__Output>
  SaveOperationConfiguration: MethodDefinition<_superverse_v2_SaveOperationConfigurationRequest, _superverse_v2_SaveOperationConfigurationResponse, _superverse_v2_SaveOperationConfigurationRequest__Output, _superverse_v2_SaveOperationConfigurationResponse__Output>
  SaveTriggerBinding: MethodDefinition<_superverse_v2_SaveTriggerBindingRequest, _superverse_v2_SaveTriggerBindingResponse, _superverse_v2_SaveTriggerBindingRequest__Output, _superverse_v2_SaveTriggerBindingResponse__Output>
  SelectTeamSpaceConnection: MethodDefinition<_superverse_v2_SelectTeamSpaceConnectionRequest, _superverse_v2_SelectTeamSpaceConnectionResponse, _superverse_v2_SelectTeamSpaceConnectionRequest__Output, _superverse_v2_SelectTeamSpaceConnectionResponse__Output>
  StartConnectorUiSession: MethodDefinition<_superverse_v2_StartConnectorUiSessionRequest, _superverse_v2_StartConnectorUiSessionResponse, _superverse_v2_StartConnectorUiSessionRequest__Output, _superverse_v2_StartConnectorUiSessionResponse__Output>
}
