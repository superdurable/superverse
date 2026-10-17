// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { ListExamplesRequest as _superverse_v2_ListExamplesRequest, ListExamplesRequest__Output as _superverse_v2_ListExamplesRequest__Output } from '../../superverse/v2/ListExamplesRequest';
import type { ListExamplesResponse as _superverse_v2_ListExamplesResponse, ListExamplesResponse__Output as _superverse_v2_ListExamplesResponse__Output } from '../../superverse/v2/ListExamplesResponse';

export interface ExampleServiceClient extends grpc.Client {
  ListExamples(argument: _superverse_v2_ListExamplesRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListExamplesResponse__Output>): grpc.ClientUnaryCall;
  ListExamples(argument: _superverse_v2_ListExamplesRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListExamplesResponse__Output>): grpc.ClientUnaryCall;
  ListExamples(argument: _superverse_v2_ListExamplesRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListExamplesResponse__Output>): grpc.ClientUnaryCall;
  ListExamples(argument: _superverse_v2_ListExamplesRequest, callback: grpc.requestCallback<_superverse_v2_ListExamplesResponse__Output>): grpc.ClientUnaryCall;
  listExamples(argument: _superverse_v2_ListExamplesRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListExamplesResponse__Output>): grpc.ClientUnaryCall;
  listExamples(argument: _superverse_v2_ListExamplesRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_ListExamplesResponse__Output>): grpc.ClientUnaryCall;
  listExamples(argument: _superverse_v2_ListExamplesRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_ListExamplesResponse__Output>): grpc.ClientUnaryCall;
  listExamples(argument: _superverse_v2_ListExamplesRequest, callback: grpc.requestCallback<_superverse_v2_ListExamplesResponse__Output>): grpc.ClientUnaryCall;

}

export interface ExampleServiceHandlers extends grpc.UntypedServiceImplementation {
  ListExamples: grpc.handleUnaryCall<_superverse_v2_ListExamplesRequest__Output, _superverse_v2_ListExamplesResponse>;

}

export interface ExampleServiceDefinition extends grpc.ServiceDefinition {
  ListExamples: MethodDefinition<_superverse_v2_ListExamplesRequest, _superverse_v2_ListExamplesResponse, _superverse_v2_ListExamplesRequest__Output, _superverse_v2_ListExamplesResponse__Output>
}
