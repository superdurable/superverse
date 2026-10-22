// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { GetEvolveRequest as _superverse_v2_GetEvolveRequest, GetEvolveRequest__Output as _superverse_v2_GetEvolveRequest__Output } from '../../superverse/v2/GetEvolveRequest';
import type { GetEvolveResponse as _superverse_v2_GetEvolveResponse, GetEvolveResponse__Output as _superverse_v2_GetEvolveResponse__Output } from '../../superverse/v2/GetEvolveResponse';
import type { StartEvolveReviewRequest as _superverse_v2_StartEvolveReviewRequest, StartEvolveReviewRequest__Output as _superverse_v2_StartEvolveReviewRequest__Output } from '../../superverse/v2/StartEvolveReviewRequest';
import type { StartEvolveReviewResponse as _superverse_v2_StartEvolveReviewResponse, StartEvolveReviewResponse__Output as _superverse_v2_StartEvolveReviewResponse__Output } from '../../superverse/v2/StartEvolveReviewResponse';

export interface EvolveServiceClient extends grpc.Client {
  GetEvolve(argument: _superverse_v2_GetEvolveRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  GetEvolve(argument: _superverse_v2_GetEvolveRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  GetEvolve(argument: _superverse_v2_GetEvolveRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  GetEvolve(argument: _superverse_v2_GetEvolveRequest, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  getEvolve(argument: _superverse_v2_GetEvolveRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  getEvolve(argument: _superverse_v2_GetEvolveRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  getEvolve(argument: _superverse_v2_GetEvolveRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  getEvolve(argument: _superverse_v2_GetEvolveRequest, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;

  StartEvolveReview(argument: _superverse_v2_StartEvolveReviewRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartEvolveReviewResponse__Output>): grpc.ClientUnaryCall;
  StartEvolveReview(argument: _superverse_v2_StartEvolveReviewRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartEvolveReviewResponse__Output>): grpc.ClientUnaryCall;
  StartEvolveReview(argument: _superverse_v2_StartEvolveReviewRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartEvolveReviewResponse__Output>): grpc.ClientUnaryCall;
  StartEvolveReview(argument: _superverse_v2_StartEvolveReviewRequest, callback: grpc.requestCallback<_superverse_v2_StartEvolveReviewResponse__Output>): grpc.ClientUnaryCall;
  startEvolveReview(argument: _superverse_v2_StartEvolveReviewRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartEvolveReviewResponse__Output>): grpc.ClientUnaryCall;
  startEvolveReview(argument: _superverse_v2_StartEvolveReviewRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_StartEvolveReviewResponse__Output>): grpc.ClientUnaryCall;
  startEvolveReview(argument: _superverse_v2_StartEvolveReviewRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_StartEvolveReviewResponse__Output>): grpc.ClientUnaryCall;
  startEvolveReview(argument: _superverse_v2_StartEvolveReviewRequest, callback: grpc.requestCallback<_superverse_v2_StartEvolveReviewResponse__Output>): grpc.ClientUnaryCall;

}

export interface EvolveServiceHandlers extends grpc.UntypedServiceImplementation {
  GetEvolve: grpc.handleUnaryCall<_superverse_v2_GetEvolveRequest__Output, _superverse_v2_GetEvolveResponse>;

  StartEvolveReview: grpc.handleUnaryCall<_superverse_v2_StartEvolveReviewRequest__Output, _superverse_v2_StartEvolveReviewResponse>;

}

export interface EvolveServiceDefinition extends grpc.ServiceDefinition {
  GetEvolve: MethodDefinition<_superverse_v2_GetEvolveRequest, _superverse_v2_GetEvolveResponse, _superverse_v2_GetEvolveRequest__Output, _superverse_v2_GetEvolveResponse__Output>
  StartEvolveReview: MethodDefinition<_superverse_v2_StartEvolveReviewRequest, _superverse_v2_StartEvolveReviewResponse, _superverse_v2_StartEvolveReviewRequest__Output, _superverse_v2_StartEvolveReviewResponse__Output>
}
