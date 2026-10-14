// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface PreparePreviewSourceRequest {
  'projectId'?: (string);
  'requestId'?: (string);
  'expectedRevision'?: (number | string | Long);
  'generation'?: (number | string | Long);
  'workspaceRequestId'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface PreparePreviewSourceRequest__Output {
  'projectId': (string);
  'requestId': (string);
  'expectedRevision': (string);
  'generation': (string);
  'workspaceRequestId': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
