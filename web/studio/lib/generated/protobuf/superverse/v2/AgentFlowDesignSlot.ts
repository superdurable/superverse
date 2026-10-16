// Original file: api/superverse/v2/superverse.proto

export const AgentFlowDesignSlot = {
  AGENT_FLOW_DESIGN_SLOT_UNSPECIFIED: 'AGENT_FLOW_DESIGN_SLOT_UNSPECIFIED',
  AGENT_FLOW_DESIGN_SLOT_CONFIRMED: 'AGENT_FLOW_DESIGN_SLOT_CONFIRMED',
} as const;

export type AgentFlowDesignSlot =
  | 'AGENT_FLOW_DESIGN_SLOT_UNSPECIFIED'
  | 0
  | 'AGENT_FLOW_DESIGN_SLOT_CONFIRMED'
  | 2

export type AgentFlowDesignSlot__Output = typeof AgentFlowDesignSlot[keyof typeof AgentFlowDesignSlot]
