// Original file: api/superverse/v2/superverse.proto

export const EvolveGoalDirection = {
  EVOLVE_GOAL_DIRECTION_UNSPECIFIED: 'EVOLVE_GOAL_DIRECTION_UNSPECIFIED',
  EVOLVE_GOAL_DIRECTION_UP: 'EVOLVE_GOAL_DIRECTION_UP',
  EVOLVE_GOAL_DIRECTION_DOWN: 'EVOLVE_GOAL_DIRECTION_DOWN',
} as const;

export type EvolveGoalDirection =
  | 'EVOLVE_GOAL_DIRECTION_UNSPECIFIED'
  | 0
  | 'EVOLVE_GOAL_DIRECTION_UP'
  | 1
  | 'EVOLVE_GOAL_DIRECTION_DOWN'
  | 2

export type EvolveGoalDirection__Output = typeof EvolveGoalDirection[keyof typeof EvolveGoalDirection]
