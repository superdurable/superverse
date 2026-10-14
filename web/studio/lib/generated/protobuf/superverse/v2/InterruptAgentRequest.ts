// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface InterruptAgentRequest {
  'projectId'?: (string);
  'turn'?: (number | string | Long);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface InterruptAgentRequest__Output {
  'projectId': (string);
  'turn': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
