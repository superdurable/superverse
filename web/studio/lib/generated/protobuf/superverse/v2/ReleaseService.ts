// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { CancelReleaseBuildRequest as _superverse_v2_CancelReleaseBuildRequest, CancelReleaseBuildRequest__Output as _superverse_v2_CancelReleaseBuildRequest__Output } from '../../superverse/v2/CancelReleaseBuildRequest';
import type { CancelReleaseBuildResponse as _superverse_v2_CancelReleaseBuildResponse, CancelReleaseBuildResponse__Output as _superverse_v2_CancelReleaseBuildResponse__Output } from '../../superverse/v2/CancelReleaseBuildResponse';
import type { DeriveLiveConfigurationRequest as _superverse_v2_DeriveLiveConfigurationRequest, DeriveLiveConfigurationRequest__Output as _superverse_v2_DeriveLiveConfigurationRequest__Output } from '../../superverse/v2/DeriveLiveConfigurationRequest';
import type { DeriveLiveConfigurationResponse as _superverse_v2_DeriveLiveConfigurationResponse, DeriveLiveConfigurationResponse__Output as _superverse_v2_DeriveLiveConfigurationResponse__Output } from '../../superverse/v2/DeriveLiveConfigurationResponse';
import type { GetReleaseBuildLogRequest as _superverse_v2_GetReleaseBuildLogRequest, GetReleaseBuildLogRequest__Output as _superverse_v2_GetReleaseBuildLogRequest__Output } from '../../superverse/v2/GetReleaseBuildLogRequest';
import type { GetReleaseBuildLogResponse as _superverse_v2_GetReleaseBuildLogResponse, GetReleaseBuildLogResponse__Output as _superverse_v2_GetReleaseBuildLogResponse__Output } from '../../superverse/v2/GetReleaseBuildLogResponse';
import type { GetReleaseRequest as _superverse_v2_GetReleaseRequest, GetReleaseRequest__Output as _superverse_v2_GetReleaseRequest__Output } from '../../superverse/v2/GetReleaseRequest';
import type { GetReleaseResponse as _superverse_v2_GetReleaseResponse, GetReleaseResponse__Output as _superverse_v2_GetReleaseResponse__Output } from '../../superverse/v2/GetReleaseResponse';
import type { ListReleaseCandidatesRequest as _superverse_v2_ListReleaseCandidatesRequest, ListReleaseCandidatesRequest__Output as _superverse_v2_ListReleaseCandidatesRequest__Output } from '../../superverse/v2/ListReleaseCandidatesRequest';
import type { ListReleaseCandidatesResponse as _superverse_v2_ListReleaseCandidatesResponse, ListReleaseCandidatesResponse__Output as _superverse_v2_ListReleaseCandidatesResponse__Output } from '../../superverse/v2/ListReleaseCandidatesResponse';
import type { ListReleasesRequest as _superverse_v2_ListReleasesRequest, ListReleasesRequest__Output as _superverse_v2_ListReleasesRequest__Output } from '../../superverse/v2/ListReleasesRequest';
import type { ListReleasesResponse as _superverse_v2_ListReleasesResponse, ListReleasesResponse__Output as _superverse_v2_ListReleasesResponse__Output } from '../../superverse/v2/ListReleasesResponse';
import type { PrepareReleaseRequest as _superverse_v2_PrepareReleaseRequest, PrepareReleaseRequest__Output as _superverse_v2_PrepareReleaseRequest__Output } from '../../superverse/v2/PrepareReleaseRequest';
import type { PrepareReleaseResponse as _superverse_v2_PrepareReleaseResponse, PrepareReleaseResponse__Output as _superverse_v2_PrepareReleaseResponse__Output } from '../../superverse/v2/PrepareReleaseResponse';
import type { RecoverReleaseBuildRequest as _superverse_v2_RecoverReleaseBuildRequest, RecoverReleaseBuildRequest__Output as _superverse_v2_RecoverReleaseBuildRequest__Output } from '../../superverse/v2/RecoverReleaseBuildRequest';
import type { RecoverReleaseBuildResponse as _superverse_v2_RecoverReleaseBuildResponse, RecoverReleaseBuildResponse__Output as _superverse_v2_RecoverReleaseBuildResponse__Output } from '../../superverse/v2/RecoverReleaseBuildResponse';
import type { StartReleaseBuildRequest as _superverse_v2_StartReleaseBuildRequest, StartReleaseBuildRequest__Output as _superverse_v2_StartReleaseBuildRequest__Output } from '../../superverse/v2/StartReleaseBuildRequest';
import type { StartReleaseBuildResponse as _superverse_v2_StartReleaseBuildResponse, StartReleaseBuildResponse__Output as _superverse_v2_StartReleaseBuildResponse__Output } from '../../superverse/v2/StartReleaseBuildResponse';
import type { WatchReleaseRequest as _superverse_v2_WatchReleaseRequest, WatchReleaseRequest__Output as _superverse_v2_WatchReleaseRequest__Output } from '../../superverse/v2/WatchReleaseRequest';
import type { WatchReleaseResponse as _superverse_v2_WatchReleaseResponse, WatchReleaseResponse__Output as _superverse_v2_WatchReleaseResponse__Output } from '../../superverse/v2/WatchReleaseResponse';

export interface ReleaseServiceClient extends grpc.Client {
  CancelReleaseBuild(argument: _superverse_v2_CancelReleaseBuildRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CancelReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  CancelReleaseBuild(argument: _superverse_v2_CancelReleaseBuildRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CancelReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  CancelReleaseBuild(argument: _superverse_v2_CancelReleaseBuildRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CancelReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  CancelReleaseBuild(argument: _superverse_v2_CancelReleaseBuildRequest, callback: grpc.requestCallback<_superverse_v2_CancelReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  cancelReleaseBuild(argument: _superverse_v2_CancelReleaseBuildRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CancelReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  cancelReleaseBuild(argument: _superverse_v2_CancelReleaseBuildRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_CancelReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  cancelReleaseBuild(argument: _superverse_v2_CancelReleaseBuildRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_CancelReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  cancelReleaseBuild(argument: _superverse_v2_CancelReleaseBuildRequest, callback: grpc.requestCallback<_superverse_v2_CancelReleaseBuildResponse__Output>): grpc.ClientUnaryCall;

  DeriveLiveConfiguration(argument: _superverse_v2_DeriveLiveConfigurationRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeriveLiveConfigurationResponse__Output>): grpc.ClientUnaryCall;
  DeriveLiveConfiguration(argument: _superverse_v2_DeriveLiveConfigurationRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeriveLiveConfigurationResponse__Output>): grpc.ClientUnaryCall;
  DeriveLiveConfiguration(argument: _superverse_v2_DeriveLiveConfigurationRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeriveLiveConfigurationResponse__Output>): grpc.ClientUnaryCall;
  DeriveLiveConfiguration(argument: _superverse_v2_DeriveLiveConfigurationRequest, callback: grpc.requestCallback<_superverse_v2_DeriveLiveConfigurationResponse__Output>): grpc.ClientUnaryCall;
  deriveLiveConfiguration(argument: _superverse_v2_DeriveLiveConfigurationRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeriveLiveConfigurationResponse__Output>): grpc.ClientUnaryCall;
  deriveLiveConfiguration(argument: _superverse_v2_DeriveLiveConfigurationRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DeriveLiveConfigurationResponse__Output>): grpc.ClientUnaryCall;
  deriveLiveConfiguration(argument: _superverse_v2_DeriveLiveConfigurationRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DeriveLiveConfigurationResponse__Output>): grpc.ClientUnaryCall;
  deriveLiveConfiguration(argument: _superverse_v2_DeriveLiveConfigurationRequest, callback: grpc.requestCallback<_superverse_v2_DeriveLiveConfigurationResponse__Output>): grpc.ClientUnaryCall;

  GetRelease(argument: _superverse_v2_GetReleaseRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetReleaseResponse__Output>): grpc.ClientUnaryCall;
  GetRelease(argument: _superverse_v2_GetReleaseRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetReleaseResponse__Output>): grpc.ClientUnaryCall;
  GetRelease(argument: _superverse_v2_GetReleaseRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetReleaseResponse__Output>): grpc.ClientUnaryCall;
  GetRelease(argument: _superverse_v2_GetReleaseRequest, callback: grpc.requestCallback<_superverse_v2_GetReleaseResponse__Output>): grpc.ClientUnaryCall;
  getRelease(argument: _superverse_v2_GetReleaseRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetReleaseResponse__Output>): grpc.ClientUnaryCall;
  getRelease(argument: _superverse_v2_GetReleaseRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetReleaseResponse__Output>): grpc.ClientUnaryCall;
  getRelease(argument: _superverse_v2_GetReleaseRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetReleaseResponse__Output>): grpc.ClientUnaryCall;
  getRelease(argument: _superverse_v2_GetReleaseRequest, callback: grpc.requestCallback<_superverse_v2_GetReleaseResponse__Output>): grpc.ClientUnaryCall;

  GetReleaseBuildLog(argument: _superverse_v2_GetReleaseBuildLogRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetReleaseBuildLogResponse__Output>): grpc.ClientUnaryCall;
  GetReleaseBuildLog(argument: _superverse_v2_GetReleaseBuildLogRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetReleaseBuildLogResponse__Output>): grpc.ClientUnaryCall;
  GetReleaseBuildLog(argument: _superverse_v2_GetReleaseBuildLogRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetReleaseBuildLogResponse__Output>): grpc.ClientUnaryCall;
  GetReleaseBuildLog(argument: _superverse_v2_GetReleaseBuildLogRequest, callback: grpc.requestCallback<_superverse_v2_GetReleaseBuildLogResponse__Output>): grpc.ClientUnaryCall;
  getReleaseBuildLog(argument: _superverse_v2_GetReleaseBuildLogRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetReleaseBuildLogResponse__Output>): grpc.ClientUnaryCall;
  getReleaseBuildLog(argument: _superverse_v2_GetReleaseBuildLogRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetReleaseBuildLogResponse__Output>): grpc.ClientUnaryCall;
  getReleaseBuildLog(argument: _superverse_v2_GetReleaseBuildLogRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetReleaseBuildLogResponse__Output>): grpc.ClientUnaryCall;
  getReleaseBuildLog(argument: _superverse_v2_GetReleaseBuildLogRequest, callback: grpc.requestCallback<_superverse_v2_GetReleaseBuildLogResponse__Output>): grpc.ClientUnaryCall;

  ListReleaseCandidates(argument: _superverse_v2_ListReleaseCandidatesRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListReleaseCandidatesResponse__Output>): grpc.ClientUnaryCall;
  ListReleaseCandidates(argument: _superverse_v2_ListReleaseCandidatesRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListReleaseCandidatesResponse__Output>): grpc.ClientUnaryCall;
  ListReleaseCandidates(argument: _superverse_v2_ListReleaseCandidatesRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListReleaseCandidatesResponse__Output>): grpc.ClientUnaryCall;
  ListReleaseCandidates(argument: _superverse_v2_ListReleaseCandidatesRequest, callback: grpc.requestCallback<_superverse_v2_ListReleaseCandidatesResponse__Output>): grpc.ClientUnaryCall;
  listReleaseCandidates(argument: _superverse_v2_ListReleaseCandidatesRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListReleaseCandidatesResponse__Output>): grpc.ClientUnaryCall;
  listReleaseCandidates(argument: _superverse_v2_ListReleaseCandidatesRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListReleaseCandidatesResponse__Output>): grpc.ClientUnaryCall;
  listReleaseCandidates(argument: _superverse_v2_ListReleaseCandidatesRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListReleaseCandidatesResponse__Output>): grpc.ClientUnaryCall;
  listReleaseCandidates(argument: _superverse_v2_ListReleaseCandidatesRequest, callback: grpc.requestCallback<_superverse_v2_ListReleaseCandidatesResponse__Output>): grpc.ClientUnaryCall;

  ListReleases(argument: _superverse_v2_ListReleasesRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListReleasesResponse__Output>): grpc.ClientUnaryCall;
  ListReleases(argument: _superverse_v2_ListReleasesRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListReleasesResponse__Output>): grpc.ClientUnaryCall;
  ListReleases(argument: _superverse_v2_ListReleasesRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListReleasesResponse__Output>): grpc.ClientUnaryCall;
  ListReleases(argument: _superverse_v2_ListReleasesRequest, callback: grpc.requestCallback<_superverse_v2_ListReleasesResponse__Output>): grpc.ClientUnaryCall;
  listReleases(argument: _superverse_v2_ListReleasesRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListReleasesResponse__Output>): grpc.ClientUnaryCall;
  listReleases(argument: _superverse_v2_ListReleasesRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListReleasesResponse__Output>): grpc.ClientUnaryCall;
  listReleases(argument: _superverse_v2_ListReleasesRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListReleasesResponse__Output>): grpc.ClientUnaryCall;
  listReleases(argument: _superverse_v2_ListReleasesRequest, callback: grpc.requestCallback<_superverse_v2_ListReleasesResponse__Output>): grpc.ClientUnaryCall;

  PrepareRelease(argument: _superverse_v2_PrepareReleaseRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_PrepareReleaseResponse__Output>): grpc.ClientUnaryCall;
  PrepareRelease(argument: _superverse_v2_PrepareReleaseRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_PrepareReleaseResponse__Output>): grpc.ClientUnaryCall;
  PrepareRelease(argument: _superverse_v2_PrepareReleaseRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_PrepareReleaseResponse__Output>): grpc.ClientUnaryCall;
  PrepareRelease(argument: _superverse_v2_PrepareReleaseRequest, callback: grpc.requestCallback<_superverse_v2_PrepareReleaseResponse__Output>): grpc.ClientUnaryCall;
  prepareRelease(argument: _superverse_v2_PrepareReleaseRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_PrepareReleaseResponse__Output>): grpc.ClientUnaryCall;
  prepareRelease(argument: _superverse_v2_PrepareReleaseRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_PrepareReleaseResponse__Output>): grpc.ClientUnaryCall;
  prepareRelease(argument: _superverse_v2_PrepareReleaseRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_PrepareReleaseResponse__Output>): grpc.ClientUnaryCall;
  prepareRelease(argument: _superverse_v2_PrepareReleaseRequest, callback: grpc.requestCallback<_superverse_v2_PrepareReleaseResponse__Output>): grpc.ClientUnaryCall;

  RecoverReleaseBuild(argument: _superverse_v2_RecoverReleaseBuildRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RecoverReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  RecoverReleaseBuild(argument: _superverse_v2_RecoverReleaseBuildRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RecoverReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  RecoverReleaseBuild(argument: _superverse_v2_RecoverReleaseBuildRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RecoverReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  RecoverReleaseBuild(argument: _superverse_v2_RecoverReleaseBuildRequest, callback: grpc.requestCallback<_superverse_v2_RecoverReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  recoverReleaseBuild(argument: _superverse_v2_RecoverReleaseBuildRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RecoverReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  recoverReleaseBuild(argument: _superverse_v2_RecoverReleaseBuildRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RecoverReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  recoverReleaseBuild(argument: _superverse_v2_RecoverReleaseBuildRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RecoverReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  recoverReleaseBuild(argument: _superverse_v2_RecoverReleaseBuildRequest, callback: grpc.requestCallback<_superverse_v2_RecoverReleaseBuildResponse__Output>): grpc.ClientUnaryCall;

  StartReleaseBuild(argument: _superverse_v2_StartReleaseBuildRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  StartReleaseBuild(argument: _superverse_v2_StartReleaseBuildRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  StartReleaseBuild(argument: _superverse_v2_StartReleaseBuildRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  StartReleaseBuild(argument: _superverse_v2_StartReleaseBuildRequest, callback: grpc.requestCallback<_superverse_v2_StartReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  startReleaseBuild(argument: _superverse_v2_StartReleaseBuildRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  startReleaseBuild(argument: _superverse_v2_StartReleaseBuildRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  startReleaseBuild(argument: _superverse_v2_StartReleaseBuildRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartReleaseBuildResponse__Output>): grpc.ClientUnaryCall;
  startReleaseBuild(argument: _superverse_v2_StartReleaseBuildRequest, callback: grpc.requestCallback<_superverse_v2_StartReleaseBuildResponse__Output>): grpc.ClientUnaryCall;

  WatchRelease(argument: _superverse_v2_WatchReleaseRequest, metadata: grpc.Metadata, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchReleaseResponse__Output>;
  WatchRelease(argument: _superverse_v2_WatchReleaseRequest, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchReleaseResponse__Output>;
  watchRelease(argument: _superverse_v2_WatchReleaseRequest, metadata: grpc.Metadata, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchReleaseResponse__Output>;
  watchRelease(argument: _superverse_v2_WatchReleaseRequest, options?: grpc.CallOptions): grpc.ClientReadableStream<_superverse_v2_WatchReleaseResponse__Output>;

}

export interface ReleaseServiceHandlers extends grpc.UntypedServiceImplementation {
  CancelReleaseBuild: grpc.handleUnaryCall<_superverse_v2_CancelReleaseBuildRequest__Output, _superverse_v2_CancelReleaseBuildResponse>;

  DeriveLiveConfiguration: grpc.handleUnaryCall<_superverse_v2_DeriveLiveConfigurationRequest__Output, _superverse_v2_DeriveLiveConfigurationResponse>;

  GetRelease: grpc.handleUnaryCall<_superverse_v2_GetReleaseRequest__Output, _superverse_v2_GetReleaseResponse>;

  GetReleaseBuildLog: grpc.handleUnaryCall<_superverse_v2_GetReleaseBuildLogRequest__Output, _superverse_v2_GetReleaseBuildLogResponse>;

  ListReleaseCandidates: grpc.handleUnaryCall<_superverse_v2_ListReleaseCandidatesRequest__Output, _superverse_v2_ListReleaseCandidatesResponse>;

  ListReleases: grpc.handleUnaryCall<_superverse_v2_ListReleasesRequest__Output, _superverse_v2_ListReleasesResponse>;

  PrepareRelease: grpc.handleUnaryCall<_superverse_v2_PrepareReleaseRequest__Output, _superverse_v2_PrepareReleaseResponse>;

  RecoverReleaseBuild: grpc.handleUnaryCall<_superverse_v2_RecoverReleaseBuildRequest__Output, _superverse_v2_RecoverReleaseBuildResponse>;

  StartReleaseBuild: grpc.handleUnaryCall<_superverse_v2_StartReleaseBuildRequest__Output, _superverse_v2_StartReleaseBuildResponse>;

  WatchRelease: grpc.handleServerStreamingCall<_superverse_v2_WatchReleaseRequest__Output, _superverse_v2_WatchReleaseResponse>;

}

export interface ReleaseServiceDefinition extends grpc.ServiceDefinition {
  CancelReleaseBuild: MethodDefinition<_superverse_v2_CancelReleaseBuildRequest, _superverse_v2_CancelReleaseBuildResponse, _superverse_v2_CancelReleaseBuildRequest__Output, _superverse_v2_CancelReleaseBuildResponse__Output>
  DeriveLiveConfiguration: MethodDefinition<_superverse_v2_DeriveLiveConfigurationRequest, _superverse_v2_DeriveLiveConfigurationResponse, _superverse_v2_DeriveLiveConfigurationRequest__Output, _superverse_v2_DeriveLiveConfigurationResponse__Output>
  GetRelease: MethodDefinition<_superverse_v2_GetReleaseRequest, _superverse_v2_GetReleaseResponse, _superverse_v2_GetReleaseRequest__Output, _superverse_v2_GetReleaseResponse__Output>
  GetReleaseBuildLog: MethodDefinition<_superverse_v2_GetReleaseBuildLogRequest, _superverse_v2_GetReleaseBuildLogResponse, _superverse_v2_GetReleaseBuildLogRequest__Output, _superverse_v2_GetReleaseBuildLogResponse__Output>
  ListReleaseCandidates: MethodDefinition<_superverse_v2_ListReleaseCandidatesRequest, _superverse_v2_ListReleaseCandidatesResponse, _superverse_v2_ListReleaseCandidatesRequest__Output, _superverse_v2_ListReleaseCandidatesResponse__Output>
  ListReleases: MethodDefinition<_superverse_v2_ListReleasesRequest, _superverse_v2_ListReleasesResponse, _superverse_v2_ListReleasesRequest__Output, _superverse_v2_ListReleasesResponse__Output>
  PrepareRelease: MethodDefinition<_superverse_v2_PrepareReleaseRequest, _superverse_v2_PrepareReleaseResponse, _superverse_v2_PrepareReleaseRequest__Output, _superverse_v2_PrepareReleaseResponse__Output>
  RecoverReleaseBuild: MethodDefinition<_superverse_v2_RecoverReleaseBuildRequest, _superverse_v2_RecoverReleaseBuildResponse, _superverse_v2_RecoverReleaseBuildRequest__Output, _superverse_v2_RecoverReleaseBuildResponse__Output>
  StartReleaseBuild: MethodDefinition<_superverse_v2_StartReleaseBuildRequest, _superverse_v2_StartReleaseBuildResponse, _superverse_v2_StartReleaseBuildRequest__Output, _superverse_v2_StartReleaseBuildResponse__Output>
  WatchRelease: MethodDefinition<_superverse_v2_WatchReleaseRequest, _superverse_v2_WatchReleaseResponse, _superverse_v2_WatchReleaseRequest__Output, _superverse_v2_WatchReleaseResponse__Output>
}
