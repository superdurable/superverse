// Original file: api/superverse/v2/superverse.proto


// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentFlowDesignWait_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_NONE: 'KIND_NONE',
  KIND_ACTION: 'KIND_ACTION',
  KIND_CHANNEL: 'KIND_CHANNEL',
  KIND_TIMER: 'KIND_TIMER',
} as const;

export type _superverse_v2_AgentFlowDesignWait_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_NONE'
  | 1
  | 'KIND_ACTION'
  | 2
  | 'KIND_CHANNEL'
  | 3
  | 'KIND_TIMER'
  | 4

export type _superverse_v2_AgentFlowDesignWait_Kind__Output = typeof _superverse_v2_AgentFlowDesignWait_Kind[keyof typeof _superverse_v2_AgentFlowDesignWait_Kind]

export interface AgentFlowDesignWait {
  'kind'?: (_superverse_v2_AgentFlowDesignWait_Kind);
  'name'?: (string);
}

export interface AgentFlowDesignWait__Output {
  'kind': (_superverse_v2_AgentFlowDesignWait_Kind__Output);
  'name': (string);
}
