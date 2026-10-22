// Original file: api/superverse/v2/superverse.proto

export const EvolveGoalsStatus = {
  EVOLVE_GOALS_STATUS_UNSPECIFIED: 'EVOLVE_GOALS_STATUS_UNSPECIFIED',
  EVOLVE_GOALS_STATUS_NONE: 'EVOLVE_GOALS_STATUS_NONE',
  EVOLVE_GOALS_STATUS_VALID: 'EVOLVE_GOALS_STATUS_VALID',
  EVOLVE_GOALS_STATUS_INVALID: 'EVOLVE_GOALS_STATUS_INVALID',
} as const;

export type EvolveGoalsStatus =
  | 'EVOLVE_GOALS_STATUS_UNSPECIFIED'
  | 0
  | 'EVOLVE_GOALS_STATUS_NONE'
  | 1
  | 'EVOLVE_GOALS_STATUS_VALID'
  | 2
  | 'EVOLVE_GOALS_STATUS_INVALID'
  | 3

export type EvolveGoalsStatus__Output = typeof EvolveGoalsStatus[keyof typeof EvolveGoalsStatus]
