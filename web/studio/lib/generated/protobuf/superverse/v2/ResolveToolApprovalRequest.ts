// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface ResolveToolApprovalRequest {
  'projectId'?: (string);
  'callId'?: (string);
  'isApproved'?: (boolean);
  'caller'?: (_superverse_v2_CallerContext | null);
  '_isApproved'?: "isApproved";
}

export interface ResolveToolApprovalRequest__Output {
  'projectId': (string);
  'callId': (string);
  'isApproved'?: (boolean);
  'caller': (_superverse_v2_CallerContext__Output | null);
  '_isApproved'?: "isApproved";
}
