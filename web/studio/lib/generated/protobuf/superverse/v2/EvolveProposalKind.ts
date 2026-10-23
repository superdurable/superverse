// Original file: api/superverse/v2/superverse.proto

export const EvolveProposalKind = {
  EVOLVE_PROPOSAL_KIND_UNSPECIFIED: 'EVOLVE_PROPOSAL_KIND_UNSPECIFIED',
  EVOLVE_PROPOSAL_KIND_CHANGE: 'EVOLVE_PROPOSAL_KIND_CHANGE',
  EVOLVE_PROPOSAL_KIND_TRIAL: 'EVOLVE_PROPOSAL_KIND_TRIAL',
  EVOLVE_PROPOSAL_KIND_UNDO: 'EVOLVE_PROPOSAL_KIND_UNDO',
} as const;

export type EvolveProposalKind =
  | 'EVOLVE_PROPOSAL_KIND_UNSPECIFIED'
  | 0
  | 'EVOLVE_PROPOSAL_KIND_CHANGE'
  | 1
  | 'EVOLVE_PROPOSAL_KIND_TRIAL'
  | 2
  | 'EVOLVE_PROPOSAL_KIND_UNDO'
  | 3

export type EvolveProposalKind__Output = typeof EvolveProposalKind[keyof typeof EvolveProposalKind]
