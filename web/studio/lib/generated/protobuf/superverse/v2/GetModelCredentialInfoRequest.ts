// Original file: api/superverse/v2/superverse.proto

import type { ModelProvider as _superverse_v2_ModelProvider, ModelProvider__Output as _superverse_v2_ModelProvider__Output } from '../../superverse/v2/ModelProvider';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface GetModelCredentialInfoRequest {
  'projectId'?: (string);
  'provider'?: (_superverse_v2_ModelProvider);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface GetModelCredentialInfoRequest__Output {
  'projectId': (string);
  'provider': (_superverse_v2_ModelProvider__Output);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
