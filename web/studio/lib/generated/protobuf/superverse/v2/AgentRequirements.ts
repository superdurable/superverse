// Original file: api/superverse/v2/superverse.proto

import type { AgentRequirementFeature as _superverse_v2_AgentRequirementFeature, AgentRequirementFeature__Output as _superverse_v2_AgentRequirementFeature__Output } from '../../superverse/v2/AgentRequirementFeature';
import type { AgentRequirementConnection as _superverse_v2_AgentRequirementConnection, AgentRequirementConnection__Output as _superverse_v2_AgentRequirementConnection__Output } from '../../superverse/v2/AgentRequirementConnection';
import type { AgentFlowDesignReference as _superverse_v2_AgentFlowDesignReference, AgentFlowDesignReference__Output as _superverse_v2_AgentFlowDesignReference__Output } from '../../superverse/v2/AgentFlowDesignReference';

export interface AgentRequirements {
  'title'?: (string);
  'summary'?: (string);
  'features'?: (_superverse_v2_AgentRequirementFeature)[];
  'connections'?: (_superverse_v2_AgentRequirementConnection)[];
  'userSetup'?: (string)[];
  'acceptance'?: (string)[];
  'design'?: (_superverse_v2_AgentFlowDesignReference | null);
  'designBuilt'?: (boolean);
  'designBuiltCommit'?: (string);
  'designWrittenSteps'?: (string)[];
}

export interface AgentRequirements__Output {
  'title': (string);
  'summary': (string);
  'features': (_superverse_v2_AgentRequirementFeature__Output)[];
  'connections': (_superverse_v2_AgentRequirementConnection__Output)[];
  'userSetup': (string)[];
  'acceptance': (string)[];
  'design': (_superverse_v2_AgentFlowDesignReference__Output | null);
  'designBuilt': (boolean);
  'designBuiltCommit': (string);
  'designWrittenSteps': (string)[];
}
