// Original file: api/superverse/v2/superverse.proto

import type { AgentMockScenario as _superverse_v2_AgentMockScenario, AgentMockScenario__Output as _superverse_v2_AgentMockScenario__Output } from '../../superverse/v2/AgentMockScenario';

export interface ListMockScenariosResponse {
  'selectable'?: (boolean);
  'scenarios'?: (_superverse_v2_AgentMockScenario)[];
}

export interface ListMockScenariosResponse__Output {
  'selectable': (boolean);
  'scenarios': (_superverse_v2_AgentMockScenario__Output)[];
}
