// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface WatchDeploymentRequest {
  'projectId'?: (string);
  'deploymentId'?: (string);
  'resumeCursor'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface WatchDeploymentRequest__Output {
  'projectId': (string);
  'deploymentId': (string);
  'resumeCursor': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
