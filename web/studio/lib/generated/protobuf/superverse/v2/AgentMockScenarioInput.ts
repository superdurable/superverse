// Original file: api/superverse/v2/superverse.proto


// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentMockScenarioInput_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_MESSAGE: 'KIND_MESSAGE',
  KIND_ANSWERS: 'KIND_ANSWERS',
} as const;

export type _superverse_v2_AgentMockScenarioInput_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_MESSAGE'
  | 1
  | 'KIND_ANSWERS'
  | 4

export type _superverse_v2_AgentMockScenarioInput_Kind__Output = typeof _superverse_v2_AgentMockScenarioInput_Kind[keyof typeof _superverse_v2_AgentMockScenarioInput_Kind]

export interface AgentMockScenarioInput {
  'number'?: (number);
  'label'?: (string);
  'kind'?: (_superverse_v2_AgentMockScenarioInput_Kind);
  'content'?: (string);
  'answers'?: (string)[];
}

export interface AgentMockScenarioInput__Output {
  'number': (number);
  'label': (string);
  'kind': (_superverse_v2_AgentMockScenarioInput_Kind__Output);
  'content': (string);
  'answers': (string)[];
}
