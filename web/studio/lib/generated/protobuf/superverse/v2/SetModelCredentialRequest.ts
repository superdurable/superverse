// Original file: api/superverse/v2/superverse.proto

import type { ModelProvider as _superverse_v2_ModelProvider, ModelProvider__Output as _superverse_v2_ModelProvider__Output } from '../../superverse/v2/ModelProvider';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface SetModelCredentialRequest {
  'projectId'?: (string);
  'provider'?: (_superverse_v2_ModelProvider);
  'requestId'?: (string);
  'expectedRevision'?: (number | string | Long);
  'apiKey'?: (string);
  'baseUrl'?: (string);
  'customModelId'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
  '_expectedRevision'?: "expectedRevision";
}

export interface SetModelCredentialRequest__Output {
  'projectId': (string);
  'provider': (_superverse_v2_ModelProvider__Output);
  'requestId': (string);
  'expectedRevision'?: (string);
  'apiKey': (string);
  'baseUrl': (string);
  'customModelId': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
  '_expectedRevision'?: "expectedRevision";
}
