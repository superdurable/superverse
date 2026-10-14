// Original file: api/superverse/v2/superverse.proto

import type { AgentFlowDesignStep as _superverse_v2_AgentFlowDesignStep, AgentFlowDesignStep__Output as _superverse_v2_AgentFlowDesignStep__Output } from '../../superverse/v2/AgentFlowDesignStep';
import type { AgentFlowDesignChannel as _superverse_v2_AgentFlowDesignChannel, AgentFlowDesignChannel__Output as _superverse_v2_AgentFlowDesignChannel__Output } from '../../superverse/v2/AgentFlowDesignChannel';
import type { AgentFlowDesignAction as _superverse_v2_AgentFlowDesignAction, AgentFlowDesignAction__Output as _superverse_v2_AgentFlowDesignAction__Output } from '../../superverse/v2/AgentFlowDesignAction';
import type { AgentFlowDesignAttribute as _superverse_v2_AgentFlowDesignAttribute, AgentFlowDesignAttribute__Output as _superverse_v2_AgentFlowDesignAttribute__Output } from '../../superverse/v2/AgentFlowDesignAttribute';

export interface AgentFlowDesignFlow {
  'flowType'?: (string);
  'purpose'?: (string);
  'startStep'?: (string);
  'steps'?: (_superverse_v2_AgentFlowDesignStep)[];
  'channels'?: (_superverse_v2_AgentFlowDesignChannel)[];
  'actions'?: (_superverse_v2_AgentFlowDesignAction)[];
  'attributes'?: (_superverse_v2_AgentFlowDesignAttribute)[];
  'starts'?: (string)[];
}

export interface AgentFlowDesignFlow__Output {
  'flowType': (string);
  'purpose': (string);
  'startStep': (string);
  'steps': (_superverse_v2_AgentFlowDesignStep__Output)[];
  'channels': (_superverse_v2_AgentFlowDesignChannel__Output)[];
  'actions': (_superverse_v2_AgentFlowDesignAction__Output)[];
  'attributes': (_superverse_v2_AgentFlowDesignAttribute__Output)[];
  'starts': (string)[];
}
