// Original file: api/superverse/v2/superverse.proto

import type { AgentFlowDesignFlow as _superverse_v2_AgentFlowDesignFlow, AgentFlowDesignFlow__Output as _superverse_v2_AgentFlowDesignFlow__Output } from '../../superverse/v2/AgentFlowDesignFlow';

export interface AgentFlowDesign {
  'digest'?: (string);
  'summary'?: (string);
  'flows'?: (_superverse_v2_AgentFlowDesignFlow)[];
  'removedFlows'?: (string)[];
}

export interface AgentFlowDesign__Output {
  'digest': (string);
  'summary': (string);
  'flows': (_superverse_v2_AgentFlowDesignFlow__Output)[];
  'removedFlows': (string)[];
}
