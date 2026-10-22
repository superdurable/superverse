// Original file: api/superverse/v2/superverse.proto

import type { ModelDiscoveryResult as _superverse_v2_ModelDiscoveryResult, ModelDiscoveryResult__Output as _superverse_v2_ModelDiscoveryResult__Output } from '../../superverse/v2/ModelDiscoveryResult';

export interface ListModelCredentialModelsResponse {
  'modelIds'?: (string)[];
  'result'?: (_superverse_v2_ModelDiscoveryResult);
}

export interface ListModelCredentialModelsResponse__Output {
  'modelIds': (string)[];
  'result': (_superverse_v2_ModelDiscoveryResult__Output);
}
