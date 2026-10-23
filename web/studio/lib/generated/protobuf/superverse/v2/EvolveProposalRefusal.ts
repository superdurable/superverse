// Original file: api/superverse/v2/superverse.proto

export const EvolveProposalRefusal = {
  EVOLVE_PROPOSAL_REFUSAL_UNSPECIFIED: 'EVOLVE_PROPOSAL_REFUSAL_UNSPECIFIED',
  EVOLVE_PROPOSAL_REFUSAL_STALE_PROPOSAL: 'EVOLVE_PROPOSAL_REFUSAL_STALE_PROPOSAL',
} as const;

export type EvolveProposalRefusal =
  | 'EVOLVE_PROPOSAL_REFUSAL_UNSPECIFIED'
  | 0
  | 'EVOLVE_PROPOSAL_REFUSAL_STALE_PROPOSAL'
  | 1

export type EvolveProposalRefusal__Output = typeof EvolveProposalRefusal[keyof typeof EvolveProposalRefusal]
