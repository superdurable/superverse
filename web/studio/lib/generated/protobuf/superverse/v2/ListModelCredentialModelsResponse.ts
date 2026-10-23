// Original file: api/superverse/v2/superverse.proto

import type { ModelDiscoveryResult as _superverse_v2_ModelDiscoveryResult, ModelDiscoveryResult__Output as _superverse_v2_ModelDiscoveryResult__Output } from '../../superverse/v2/ModelDiscoveryResult';
import type { ModelCredentialModel as _superverse_v2_ModelCredentialModel, ModelCredentialModel__Output as _superverse_v2_ModelCredentialModel__Output } from '../../superverse/v2/ModelCredentialModel';

export interface ListModelCredentialModelsResponse {
  'modelIds'?: (string)[];
  'result'?: (_superverse_v2_ModelDiscoveryResult);
  'models'?: (_superverse_v2_ModelCredentialModel)[];
}

export interface ListModelCredentialModelsResponse__Output {
  'modelIds': (string)[];
  'result': (_superverse_v2_ModelDiscoveryResult__Output);
  'models': (_superverse_v2_ModelCredentialModel__Output)[];
}
