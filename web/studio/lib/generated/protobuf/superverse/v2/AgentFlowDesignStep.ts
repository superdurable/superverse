// Original file: api/superverse/v2/superverse.proto

import type { AgentFlowDesignWait as _superverse_v2_AgentFlowDesignWait, AgentFlowDesignWait__Output as _superverse_v2_AgentFlowDesignWait__Output } from '../../superverse/v2/AgentFlowDesignWait';
import type { AgentFlowDesignTransition as _superverse_v2_AgentFlowDesignTransition, AgentFlowDesignTransition__Output as _superverse_v2_AgentFlowDesignTransition__Output } from '../../superverse/v2/AgentFlowDesignTransition';

export interface AgentFlowDesignStep {
  'stepType'?: (string);
  'group'?: (string);
  'explanation'?: (string);
  'waitsFor'?: (_superverse_v2_AgentFlowDesignWait | null);
  'next'?: (_superverse_v2_AgentFlowDesignTransition)[];
  'features'?: (string)[];
}

export interface AgentFlowDesignStep__Output {
  'stepType': (string);
  'group': (string);
  'explanation': (string);
  'waitsFor': (_superverse_v2_AgentFlowDesignWait__Output | null);
  'next': (_superverse_v2_AgentFlowDesignTransition__Output)[];
  'features': (string)[];
}
