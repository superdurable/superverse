// Original file: api/superverse/v2/superverse.proto

export const EvolveGoalKind = {
  EVOLVE_GOAL_KIND_UNSPECIFIED: 'EVOLVE_GOAL_KIND_UNSPECIFIED',
  EVOLVE_GOAL_KIND_OUTCOME: 'EVOLVE_GOAL_KIND_OUTCOME',
  EVOLVE_GOAL_KIND_EXPERIENCE: 'EVOLVE_GOAL_KIND_EXPERIENCE',
  EVOLVE_GOAL_KIND_COST: 'EVOLVE_GOAL_KIND_COST',
} as const;

export type EvolveGoalKind =
  | 'EVOLVE_GOAL_KIND_UNSPECIFIED'
  | 0
  | 'EVOLVE_GOAL_KIND_OUTCOME'
  | 1
  | 'EVOLVE_GOAL_KIND_EXPERIENCE'
  | 2
  | 'EVOLVE_GOAL_KIND_COST'
  | 3

export type EvolveGoalKind__Output = typeof EvolveGoalKind[keyof typeof EvolveGoalKind]
