// Original file: api/superverse/v2/superverse.proto

export const AgentModel = {
  AGENT_MODEL_UNSPECIFIED: 'AGENT_MODEL_UNSPECIFIED',
  AGENT_MODEL_OPUS_5_5: 'AGENT_MODEL_OPUS_5_5',
  AGENT_MODEL_SONNET_5_5: 'AGENT_MODEL_SONNET_5_5',
  AGENT_MODEL_HAIKU_4_5: 'AGENT_MODEL_HAIKU_4_5',
  AGENT_MODEL_MOCK: 'AGENT_MODEL_MOCK',
} as const;

export type AgentModel =
  | 'AGENT_MODEL_UNSPECIFIED'
  | 0
  | 'AGENT_MODEL_OPUS_5_5'
  | 1
  | 'AGENT_MODEL_SONNET_5_5'
  | 2
  | 'AGENT_MODEL_HAIKU_4_5'
  | 3
  | 'AGENT_MODEL_MOCK'
  | 4

export type AgentModel__Output = typeof AgentModel[keyof typeof AgentModel]
