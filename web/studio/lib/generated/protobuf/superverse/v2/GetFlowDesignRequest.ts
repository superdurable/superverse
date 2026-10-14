// Original file: api/superverse/v2/superverse.proto

import type { AgentFlowDesignSlot as _superverse_v2_AgentFlowDesignSlot, AgentFlowDesignSlot__Output as _superverse_v2_AgentFlowDesignSlot__Output } from '../../superverse/v2/AgentFlowDesignSlot';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface GetFlowDesignRequest {
  'projectId'?: (string);
  'slot'?: (_superverse_v2_AgentFlowDesignSlot);
  'digest'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface GetFlowDesignRequest__Output {
  'projectId': (string);
  'slot': (_superverse_v2_AgentFlowDesignSlot__Output);
  'digest': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
