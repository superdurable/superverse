// Original file: api/superverse/v2/superverse.proto


// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentFlowDesignTransition_End = {
  END_UNSPECIFIED: 'END_UNSPECIFIED',
  END_COMPLETE: 'END_COMPLETE',
  END_FAIL: 'END_FAIL',
} as const;

export type _superverse_v2_AgentFlowDesignTransition_End =
  | 'END_UNSPECIFIED'
  | 0
  | 'END_COMPLETE'
  | 1
  | 'END_FAIL'
  | 2

export type _superverse_v2_AgentFlowDesignTransition_End__Output = typeof _superverse_v2_AgentFlowDesignTransition_End[keyof typeof _superverse_v2_AgentFlowDesignTransition_End]

export interface AgentFlowDesignTransition {
  'toStep'?: (string);
  'end'?: (_superverse_v2_AgentFlowDesignTransition_End);
  'when'?: (string);
  'isFailureRoute'?: (boolean);
}

export interface AgentFlowDesignTransition__Output {
  'toStep': (string);
  'end': (_superverse_v2_AgentFlowDesignTransition_End__Output);
  'when': (string);
  'isFailureRoute': (boolean);
}
