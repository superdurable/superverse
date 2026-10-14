// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { GetPreviewDefinitionsRequest as _superverse_v2_GetPreviewDefinitionsRequest, GetPreviewDefinitionsRequest__Output as _superverse_v2_GetPreviewDefinitionsRequest__Output } from '../../superverse/v2/GetPreviewDefinitionsRequest';
import type { GetPreviewDefinitionsResponse as _superverse_v2_GetPreviewDefinitionsResponse, GetPreviewDefinitionsResponse__Output as _superverse_v2_GetPreviewDefinitionsResponse__Output } from '../../superverse/v2/GetPreviewDefinitionsResponse';
import type { GetPreviewRequest as _superverse_v2_GetPreviewRequest, GetPreviewRequest__Output as _superverse_v2_GetPreviewRequest__Output } from '../../superverse/v2/GetPreviewRequest';
import type { GetPreviewResponse as _superverse_v2_GetPreviewResponse, GetPreviewResponse__Output as _superverse_v2_GetPreviewResponse__Output } from '../../superverse/v2/GetPreviewResponse';
import type { PreparePreviewSourceRequest as _superverse_v2_PreparePreviewSourceRequest, PreparePreviewSourceRequest__Output as _superverse_v2_PreparePreviewSourceRequest__Output } from '../../superverse/v2/PreparePreviewSourceRequest';
import type { PreparePreviewSourceResponse as _superverse_v2_PreparePreviewSourceResponse, PreparePreviewSourceResponse__Output as _superverse_v2_PreparePreviewSourceResponse__Output } from '../../superverse/v2/PreparePreviewSourceResponse';
import type { StartPreviewRequest as _superverse_v2_StartPreviewRequest, StartPreviewRequest__Output as _superverse_v2_StartPreviewRequest__Output } from '../../superverse/v2/StartPreviewRequest';
import type { StartPreviewResponse as _superverse_v2_StartPreviewResponse, StartPreviewResponse__Output as _superverse_v2_StartPreviewResponse__Output } from '../../superverse/v2/StartPreviewResponse';
import type { StopPreviewRequest as _superverse_v2_StopPreviewRequest, StopPreviewRequest__Output as _superverse_v2_StopPreviewRequest__Output } from '../../superverse/v2/StopPreviewRequest';
import type { StopPreviewResponse as _superverse_v2_StopPreviewResponse, StopPreviewResponse__Output as _superverse_v2_StopPreviewResponse__Output } from '../../superverse/v2/StopPreviewResponse';
import type { UpdatePreviewAutoDeployRequest as _superverse_v2_UpdatePreviewAutoDeployRequest, UpdatePreviewAutoDeployRequest__Output as _superverse_v2_UpdatePreviewAutoDeployRequest__Output } from '../../superverse/v2/UpdatePreviewAutoDeployRequest';
import type { UpdatePreviewAutoDeployResponse as _superverse_v2_UpdatePreviewAutoDeployResponse, UpdatePreviewAutoDeployResponse__Output as _superverse_v2_UpdatePreviewAutoDeployResponse__Output } from '../../superverse/v2/UpdatePreviewAutoDeployResponse';

export interface PreviewServiceClient extends grpc.Client {
  GetPreview(argument: _superverse_v2_GetPreviewRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetPreviewResponse__Output>): grpc.ClientUnaryCall;
  GetPreview(argument: _superverse_v2_GetPreviewRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetPreviewResponse__Output>): grpc.ClientUnaryCall;
  GetPreview(argument: _superverse_v2_GetPreviewRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetPreviewResponse__Output>): grpc.ClientUnaryCall;
  GetPreview(argument: _superverse_v2_GetPreviewRequest, callback: grpc.requestCallback<_superverse_v2_GetPreviewResponse__Output>): grpc.ClientUnaryCall;
  getPreview(argument: _superverse_v2_GetPreviewRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetPreviewResponse__Output>): grpc.ClientUnaryCall;
  getPreview(argument: _superverse_v2_GetPreviewRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetPreviewResponse__Output>): grpc.ClientUnaryCall;
  getPreview(argument: _superverse_v2_GetPreviewRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetPreviewResponse__Output>): grpc.ClientUnaryCall;
  getPreview(argument: _superverse_v2_GetPreviewRequest, callback: grpc.requestCallback<_superverse_v2_GetPreviewResponse__Output>): grpc.ClientUnaryCall;

  GetPreviewDefinitions(argument: _superverse_v2_GetPreviewDefinitionsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetPreviewDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  GetPreviewDefinitions(argument: _superverse_v2_GetPreviewDefinitionsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetPreviewDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  GetPreviewDefinitions(argument: _superverse_v2_GetPreviewDefinitionsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetPreviewDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  GetPreviewDefinitions(argument: _superverse_v2_GetPreviewDefinitionsRequest, callback: grpc.requestCallback<_superverse_v2_GetPreviewDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  getPreviewDefinitions(argument: _superverse_v2_GetPreviewDefinitionsRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetPreviewDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  getPreviewDefinitions(argument: _superverse_v2_GetPreviewDefinitionsRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetPreviewDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  getPreviewDefinitions(argument: _superverse_v2_GetPreviewDefinitionsRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetPreviewDefinitionsResponse__Output>): grpc.ClientUnaryCall;
  getPreviewDefinitions(argument: _superverse_v2_GetPreviewDefinitionsRequest, callback: grpc.requestCallback<_superverse_v2_GetPreviewDefinitionsResponse__Output>): grpc.ClientUnaryCall;

  PreparePreviewSource(argument: _superverse_v2_PreparePreviewSourceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_PreparePreviewSourceResponse__Output>): grpc.ClientUnaryCall;
  PreparePreviewSource(argument: _superverse_v2_PreparePreviewSourceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_PreparePreviewSourceResponse__Output>): grpc.ClientUnaryCall;
  PreparePreviewSource(argument: _superverse_v2_PreparePreviewSourceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_PreparePreviewSourceResponse__Output>): grpc.ClientUnaryCall;
  PreparePreviewSource(argument: _superverse_v2_PreparePreviewSourceRequest, callback: grpc.requestCallback<_superverse_v2_PreparePreviewSourceResponse__Output>): grpc.ClientUnaryCall;
  preparePreviewSource(argument: _superverse_v2_PreparePreviewSourceRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_PreparePreviewSourceResponse__Output>): grpc.ClientUnaryCall;
  preparePreviewSource(argument: _superverse_v2_PreparePreviewSourceRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_PreparePreviewSourceResponse__Output>): grpc.ClientUnaryCall;
  preparePreviewSource(argument: _superverse_v2_PreparePreviewSourceRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_PreparePreviewSourceResponse__Output>): grpc.ClientUnaryCall;
  preparePreviewSource(argument: _superverse_v2_PreparePreviewSourceRequest, callback: grpc.requestCallback<_superverse_v2_PreparePreviewSourceResponse__Output>): grpc.ClientUnaryCall;

  StartPreview(argument: _superverse_v2_StartPreviewRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartPreviewResponse__Output>): grpc.ClientUnaryCall;
  StartPreview(argument: _superverse_v2_StartPreviewRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartPreviewResponse__Output>): grpc.ClientUnaryCall;
  StartPreview(argument: _superverse_v2_StartPreviewRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartPreviewResponse__Output>): grpc.ClientUnaryCall;
  StartPreview(argument: _superverse_v2_StartPreviewRequest, callback: grpc.requestCallback<_superverse_v2_StartPreviewResponse__Output>): grpc.ClientUnaryCall;
  startPreview(argument: _superverse_v2_StartPreviewRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartPreviewResponse__Output>): grpc.ClientUnaryCall;
  startPreview(argument: _superverse_v2_StartPreviewRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartPreviewResponse__Output>): grpc.ClientUnaryCall;
  startPreview(argument: _superverse_v2_StartPreviewRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartPreviewResponse__Output>): grpc.ClientUnaryCall;
  startPreview(argument: _superverse_v2_StartPreviewRequest, callback: grpc.requestCallback<_superverse_v2_StartPreviewResponse__Output>): grpc.ClientUnaryCall;

  StopPreview(argument: _superverse_v2_StopPreviewRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StopPreviewResponse__Output>): grpc.ClientUnaryCall;
  StopPreview(argument: _superverse_v2_StopPreviewRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StopPreviewResponse__Output>): grpc.ClientUnaryCall;
  StopPreview(argument: _superverse_v2_StopPreviewRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StopPreviewResponse__Output>): grpc.ClientUnaryCall;
  StopPreview(argument: _superverse_v2_StopPreviewRequest, callback: grpc.requestCallback<_superverse_v2_StopPreviewResponse__Output>): grpc.ClientUnaryCall;
  stopPreview(argument: _superverse_v2_StopPreviewRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StopPreviewResponse__Output>): grpc.ClientUnaryCall;
  stopPreview(argument: _superverse_v2_StopPreviewRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StopPreviewResponse__Output>): grpc.ClientUnaryCall;
  stopPreview(argument: _superverse_v2_StopPreviewRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StopPreviewResponse__Output>): grpc.ClientUnaryCall;
  stopPreview(argument: _superverse_v2_StopPreviewRequest, callback: grpc.requestCallback<_superverse_v2_StopPreviewResponse__Output>): grpc.ClientUnaryCall;

  UpdatePreviewAutoDeploy(argument: _superverse_v2_UpdatePreviewAutoDeployRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdatePreviewAutoDeployResponse__Output>): grpc.ClientUnaryCall;
  UpdatePreviewAutoDeploy(argument: _superverse_v2_UpdatePreviewAutoDeployRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_UpdatePreviewAutoDeployResponse__Output>): grpc.ClientUnaryCall;
  UpdatePreviewAutoDeploy(argument: _superverse_v2_UpdatePreviewAutoDeployRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdatePreviewAutoDeployResponse__Output>): grpc.ClientUnaryCall;
  UpdatePreviewAutoDeploy(argument: _superverse_v2_UpdatePreviewAutoDeployRequest, callback: grpc.requestCallback<_superverse_v2_UpdatePreviewAutoDeployResponse__Output>): grpc.ClientUnaryCall;
  updatePreviewAutoDeploy(argument: _superverse_v2_UpdatePreviewAutoDeployRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdatePreviewAutoDeployResponse__Output>): grpc.ClientUnaryCall;
  updatePreviewAutoDeploy(argument: _superverse_v2_UpdatePreviewAutoDeployRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_UpdatePreviewAutoDeployResponse__Output>): grpc.ClientUnaryCall;
  updatePreviewAutoDeploy(argument: _superverse_v2_UpdatePreviewAutoDeployRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_UpdatePreviewAutoDeployResponse__Output>): grpc.ClientUnaryCall;
  updatePreviewAutoDeploy(argument: _superverse_v2_UpdatePreviewAutoDeployRequest, callback: grpc.requestCallback<_superverse_v2_UpdatePreviewAutoDeployResponse__Output>): grpc.ClientUnaryCall;

}

export interface PreviewServiceHandlers extends grpc.UntypedServiceImplementation {
  GetPreview: grpc.handleUnaryCall<_superverse_v2_GetPreviewRequest__Output, _superverse_v2_GetPreviewResponse>;

  GetPreviewDefinitions: grpc.handleUnaryCall<_superverse_v2_GetPreviewDefinitionsRequest__Output, _superverse_v2_GetPreviewDefinitionsResponse>;

  PreparePreviewSource: grpc.handleUnaryCall<_superverse_v2_PreparePreviewSourceRequest__Output, _superverse_v2_PreparePreviewSourceResponse>;

  StartPreview: grpc.handleUnaryCall<_superverse_v2_StartPreviewRequest__Output, _superverse_v2_StartPreviewResponse>;

  StopPreview: grpc.handleUnaryCall<_superverse_v2_StopPreviewRequest__Output, _superverse_v2_StopPreviewResponse>;

  UpdatePreviewAutoDeploy: grpc.handleUnaryCall<_superverse_v2_UpdatePreviewAutoDeployRequest__Output, _superverse_v2_UpdatePreviewAutoDeployResponse>;

}

export interface PreviewServiceDefinition extends grpc.ServiceDefinition {
  GetPreview: MethodDefinition<_superverse_v2_GetPreviewRequest, _superverse_v2_GetPreviewResponse, _superverse_v2_GetPreviewRequest__Output, _superverse_v2_GetPreviewResponse__Output>
  GetPreviewDefinitions: MethodDefinition<_superverse_v2_GetPreviewDefinitionsRequest, _superverse_v2_GetPreviewDefinitionsResponse, _superverse_v2_GetPreviewDefinitionsRequest__Output, _superverse_v2_GetPreviewDefinitionsResponse__Output>
  PreparePreviewSource: MethodDefinition<_superverse_v2_PreparePreviewSourceRequest, _superverse_v2_PreparePreviewSourceResponse, _superverse_v2_PreparePreviewSourceRequest__Output, _superverse_v2_PreparePreviewSourceResponse__Output>
  StartPreview: MethodDefinition<_superverse_v2_StartPreviewRequest, _superverse_v2_StartPreviewResponse, _superverse_v2_StartPreviewRequest__Output, _superverse_v2_StartPreviewResponse__Output>
  StopPreview: MethodDefinition<_superverse_v2_StopPreviewRequest, _superverse_v2_StopPreviewResponse, _superverse_v2_StopPreviewRequest__Output, _superverse_v2_StopPreviewResponse__Output>
  UpdatePreviewAutoDeploy: MethodDefinition<_superverse_v2_UpdatePreviewAutoDeployRequest, _superverse_v2_UpdatePreviewAutoDeployResponse, _superverse_v2_UpdatePreviewAutoDeployRequest__Output, _superverse_v2_UpdatePreviewAutoDeployResponse__Output>
}
