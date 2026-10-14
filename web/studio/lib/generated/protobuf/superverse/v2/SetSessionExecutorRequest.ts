// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface SetSessionExecutorRequest {
  'projectId'?: (string);
  'basisEpoch'?: (number | string | Long);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface SetSessionExecutorRequest__Output {
  'projectId': (string);
  'basisEpoch': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
