// Original file: api/superverse/v2/superverse.proto


// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentStartingExample_Choice = {
  CHOICE_UNSPECIFIED: 'CHOICE_UNSPECIFIED',
  CHOICE_PICKED: 'CHOICE_PICKED',
  CHOICE_MATCHED: 'CHOICE_MATCHED',
} as const;

export type _superverse_v2_AgentStartingExample_Choice =
  | 'CHOICE_UNSPECIFIED'
  | 0
  | 'CHOICE_PICKED'
  | 1
  | 'CHOICE_MATCHED'
  | 2

export type _superverse_v2_AgentStartingExample_Choice__Output = typeof _superverse_v2_AgentStartingExample_Choice[keyof typeof _superverse_v2_AgentStartingExample_Choice]

export interface AgentStartingExample {
  'exampleId'?: (string);
  'choice'?: (_superverse_v2_AgentStartingExample_Choice);
}

export interface AgentStartingExample__Output {
  'exampleId': (string);
  'choice': (_superverse_v2_AgentStartingExample_Choice__Output);
}
