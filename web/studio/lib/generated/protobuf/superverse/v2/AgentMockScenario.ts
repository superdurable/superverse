// Original file: api/superverse/v2/superverse.proto

import type { AgentMockScenarioArgument as _superverse_v2_AgentMockScenarioArgument, AgentMockScenarioArgument__Output as _superverse_v2_AgentMockScenarioArgument__Output } from '../../superverse/v2/AgentMockScenarioArgument';
import type { AgentMockScenarioInput as _superverse_v2_AgentMockScenarioInput, AgentMockScenarioInput__Output as _superverse_v2_AgentMockScenarioInput__Output } from '../../superverse/v2/AgentMockScenarioInput';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentMockScenario_Category = {
  CATEGORY_UNSPECIFIED: 'CATEGORY_UNSPECIFIED',
  CATEGORY_PRODUCT: 'CATEGORY_PRODUCT',
  CATEGORY_AGENT_FEATURE: 'CATEGORY_AGENT_FEATURE',
  CATEGORY_PROJECT_FEATURE: 'CATEGORY_PROJECT_FEATURE',
  CATEGORY_PLATFORM_ERROR: 'CATEGORY_PLATFORM_ERROR',
  CATEGORY_USER_ERROR: 'CATEGORY_USER_ERROR',
  CATEGORY_AGENT_ERROR: 'CATEGORY_AGENT_ERROR',
} as const;

export type _superverse_v2_AgentMockScenario_Category =
  | 'CATEGORY_UNSPECIFIED'
  | 0
  | 'CATEGORY_PRODUCT'
  | 1
  | 'CATEGORY_AGENT_FEATURE'
  | 2
  | 'CATEGORY_PROJECT_FEATURE'
  | 3
  | 'CATEGORY_PLATFORM_ERROR'
  | 4
  | 'CATEGORY_USER_ERROR'
  | 5
  | 'CATEGORY_AGENT_ERROR'
  | 6

export type _superverse_v2_AgentMockScenario_Category__Output = typeof _superverse_v2_AgentMockScenario_Category[keyof typeof _superverse_v2_AgentMockScenario_Category]

export interface AgentMockScenario {
  'name'?: (string);
  'title'?: (string);
  'description'?: (string);
  'arguments'?: (_superverse_v2_AgentMockScenarioArgument)[];
  'inputs'?: (_superverse_v2_AgentMockScenarioInput)[];
  'category'?: (_superverse_v2_AgentMockScenario_Category);
  'needsNewProject'?: (boolean);
  'testAction'?: (string);
}

export interface AgentMockScenario__Output {
  'name': (string);
  'title': (string);
  'description': (string);
  'arguments': (_superverse_v2_AgentMockScenarioArgument__Output)[];
  'inputs': (_superverse_v2_AgentMockScenarioInput__Output)[];
  'category': (_superverse_v2_AgentMockScenario_Category__Output);
  'needsNewProject': (boolean);
  'testAction': (string);
}
