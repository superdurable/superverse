// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { DecideEvolveProposalRequest as _superverse_v2_DecideEvolveProposalRequest, DecideEvolveProposalRequest__Output as _superverse_v2_DecideEvolveProposalRequest__Output } from '../../superverse/v2/DecideEvolveProposalRequest';
import type { DecideEvolveProposalResponse as _superverse_v2_DecideEvolveProposalResponse, DecideEvolveProposalResponse__Output as _superverse_v2_DecideEvolveProposalResponse__Output } from '../../superverse/v2/DecideEvolveProposalResponse';
import type { GetEvolveProposalRequest as _superverse_v2_GetEvolveProposalRequest, GetEvolveProposalRequest__Output as _superverse_v2_GetEvolveProposalRequest__Output } from '../../superverse/v2/GetEvolveProposalRequest';
import type { GetEvolveProposalResponse as _superverse_v2_GetEvolveProposalResponse, GetEvolveProposalResponse__Output as _superverse_v2_GetEvolveProposalResponse__Output } from '../../superverse/v2/GetEvolveProposalResponse';
import type { GetEvolveRequest as _superverse_v2_GetEvolveRequest, GetEvolveRequest__Output as _superverse_v2_GetEvolveRequest__Output } from '../../superverse/v2/GetEvolveRequest';
import type { GetEvolveResponse as _superverse_v2_GetEvolveResponse, GetEvolveResponse__Output as _superverse_v2_GetEvolveResponse__Output } from '../../superverse/v2/GetEvolveResponse';
import type { StartEvolveReviewRequest as _superverse_v2_StartEvolveReviewRequest, StartEvolveReviewRequest__Output as _superverse_v2_StartEvolveReviewRequest__Output } from '../../superverse/v2/StartEvolveReviewRequest';
import type { StartEvolveReviewResponse as _superverse_v2_StartEvolveReviewResponse, StartEvolveReviewResponse__Output as _superverse_v2_StartEvolveReviewResponse__Output } from '../../superverse/v2/StartEvolveReviewResponse';

export interface EvolveServiceClient extends grpc.Client {
  DecideEvolveProposal(argument: _superverse_v2_DecideEvolveProposalRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DecideEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  DecideEvolveProposal(argument: _superverse_v2_DecideEvolveProposalRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DecideEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  DecideEvolveProposal(argument: _superverse_v2_DecideEvolveProposalRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DecideEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  DecideEvolveProposal(argument: _superverse_v2_DecideEvolveProposalRequest, callback: grpc.requestCallback<_superverse_v2_DecideEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  decideEvolveProposal(argument: _superverse_v2_DecideEvolveProposalRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DecideEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  decideEvolveProposal(argument: _superverse_v2_DecideEvolveProposalRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_DecideEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  decideEvolveProposal(argument: _superverse_v2_DecideEvolveProposalRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_DecideEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  decideEvolveProposal(argument: _superverse_v2_DecideEvolveProposalRequest, callback: grpc.requestCallback<_superverse_v2_DecideEvolveProposalResponse__Output>): grpc.ClientUnaryCall;

  GetEvolve(argument: _superverse_v2_GetEvolveRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  GetEvolve(argument: _superverse_v2_GetEvolveRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  GetEvolve(argument: _superverse_v2_GetEvolveRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  GetEvolve(argument: _superverse_v2_GetEvolveRequest, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  getEvolve(argument: _superverse_v2_GetEvolveRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  getEvolve(argument: _superverse_v2_GetEvolveRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  getEvolve(argument: _superverse_v2_GetEvolveRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;
  getEvolve(argument: _superverse_v2_GetEvolveRequest, callback: grpc.requestCallback<_superverse_v2_GetEvolveResponse__Output>): grpc.ClientUnaryCall;

  GetEvolveProposal(argument: _superverse_v2_GetEvolveProposalRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  GetEvolveProposal(argument: _superverse_v2_GetEvolveProposalRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  GetEvolveProposal(argument: _superverse_v2_GetEvolveProposalRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  GetEvolveProposal(argument: _superverse_v2_GetEvolveProposalRequest, callback: grpc.requestCallback<_superverse_v2_GetEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  getEvolveProposal(argument: _superverse_v2_GetEvolveProposalRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  getEvolveProposal(argument: _superverse_v2_GetEvolveProposalRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  getEvolveProposal(argument: _superverse_v2_GetEvolveProposalRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetEvolveProposalResponse__Output>): grpc.ClientUnaryCall;
  getEvolveProposal(argument: _superverse_v2_GetEvolveProposalRequest, callback: grpc.requestCallback<_superverse_v2_GetEvolveProposalResponse__Output>): grpc.ClientUnaryCall;

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
  DecideEvolveProposal: grpc.handleUnaryCall<_superverse_v2_DecideEvolveProposalRequest__Output, _superverse_v2_DecideEvolveProposalResponse>;

  GetEvolve: grpc.handleUnaryCall<_superverse_v2_GetEvolveRequest__Output, _superverse_v2_GetEvolveResponse>;

  GetEvolveProposal: grpc.handleUnaryCall<_superverse_v2_GetEvolveProposalRequest__Output, _superverse_v2_GetEvolveProposalResponse>;

  StartEvolveReview: grpc.handleUnaryCall<_superverse_v2_StartEvolveReviewRequest__Output, _superverse_v2_StartEvolveReviewResponse>;

}

export interface EvolveServiceDefinition extends grpc.ServiceDefinition {
  DecideEvolveProposal: MethodDefinition<_superverse_v2_DecideEvolveProposalRequest, _superverse_v2_DecideEvolveProposalResponse, _superverse_v2_DecideEvolveProposalRequest__Output, _superverse_v2_DecideEvolveProposalResponse__Output>
  GetEvolve: MethodDefinition<_superverse_v2_GetEvolveRequest, _superverse_v2_GetEvolveResponse, _superverse_v2_GetEvolveRequest__Output, _superverse_v2_GetEvolveResponse__Output>
  GetEvolveProposal: MethodDefinition<_superverse_v2_GetEvolveProposalRequest, _superverse_v2_GetEvolveProposalResponse, _superverse_v2_GetEvolveProposalRequest__Output, _superverse_v2_GetEvolveProposalResponse__Output>
  StartEvolveReview: MethodDefinition<_superverse_v2_StartEvolveReviewRequest, _superverse_v2_StartEvolveReviewResponse, _superverse_v2_StartEvolveReviewRequest__Output, _superverse_v2_StartEvolveReviewResponse__Output>
}
