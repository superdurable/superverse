// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface UpdatePreviewAutoDeployRequest {
  'projectId'?: (string);
  'isEnabled'?: (boolean);
  'caller'?: (_superverse_v2_CallerContext | null);
  '_isEnabled'?: "isEnabled";
}

export interface UpdatePreviewAutoDeployRequest__Output {
  'projectId': (string);
  'isEnabled'?: (boolean);
  'caller': (_superverse_v2_CallerContext__Output | null);
  '_isEnabled'?: "isEnabled";
}
