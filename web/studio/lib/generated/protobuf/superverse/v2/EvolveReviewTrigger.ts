// Original file: api/superverse/v2/superverse.proto

export const EvolveReviewTrigger = {
  EVOLVE_REVIEW_TRIGGER_UNSPECIFIED: 'EVOLVE_REVIEW_TRIGGER_UNSPECIFIED',
  EVOLVE_REVIEW_TRIGGER_SCHEDULE: 'EVOLVE_REVIEW_TRIGGER_SCHEDULE',
  EVOLVE_REVIEW_TRIGGER_MANUAL: 'EVOLVE_REVIEW_TRIGGER_MANUAL',
} as const;

export type EvolveReviewTrigger =
  | 'EVOLVE_REVIEW_TRIGGER_UNSPECIFIED'
  | 0
  | 'EVOLVE_REVIEW_TRIGGER_SCHEDULE'
  | 1
  | 'EVOLVE_REVIEW_TRIGGER_MANUAL'
  | 2

export type EvolveReviewTrigger__Output = typeof EvolveReviewTrigger[keyof typeof EvolveReviewTrigger]
