// Original file: api/superverse/v2/superverse.proto

export const EvolveProposalDecision = {
  EVOLVE_PROPOSAL_DECISION_UNSPECIFIED: 'EVOLVE_PROPOSAL_DECISION_UNSPECIFIED',
  EVOLVE_PROPOSAL_DECISION_APPROVE: 'EVOLVE_PROPOSAL_DECISION_APPROVE',
  EVOLVE_PROPOSAL_DECISION_DECLINE: 'EVOLVE_PROPOSAL_DECISION_DECLINE',
} as const;

export type EvolveProposalDecision =
  | 'EVOLVE_PROPOSAL_DECISION_UNSPECIFIED'
  | 0
  | 'EVOLVE_PROPOSAL_DECISION_APPROVE'
  | 1
  | 'EVOLVE_PROPOSAL_DECISION_DECLINE'
  | 2

export type EvolveProposalDecision__Output = typeof EvolveProposalDecision[keyof typeof EvolveProposalDecision]
