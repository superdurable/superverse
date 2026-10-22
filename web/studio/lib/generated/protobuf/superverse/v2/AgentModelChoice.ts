// Original file: api/superverse/v2/superverse.proto

import type { AgentModel as _superverse_v2_AgentModel, AgentModel__Output as _superverse_v2_AgentModel__Output } from '../../superverse/v2/AgentModel';
import type { ModelProvider as _superverse_v2_ModelProvider, ModelProvider__Output as _superverse_v2_ModelProvider__Output } from '../../superverse/v2/ModelProvider';

export interface AgentModelChoice {
  'model'?: (_superverse_v2_AgentModel);
  'provider'?: (_superverse_v2_ModelProvider);
  'label'?: (string);
  'providerName'?: (string);
  'keyHint'?: (string);
  'mockEndpoint'?: (boolean);
}

export interface AgentModelChoice__Output {
  'model': (_superverse_v2_AgentModel__Output);
  'provider': (_superverse_v2_ModelProvider__Output);
  'label': (string);
  'providerName': (string);
  'keyHint': (string);
  'mockEndpoint': (boolean);
}
