// Original file: api/superverse/v2/superverse.proto

export const EvolveReviewPeriod = {
  EVOLVE_REVIEW_PERIOD_UNSPECIFIED: 'EVOLVE_REVIEW_PERIOD_UNSPECIFIED',
  EVOLVE_REVIEW_PERIOD_WEEKLY: 'EVOLVE_REVIEW_PERIOD_WEEKLY',
  EVOLVE_REVIEW_PERIOD_BIWEEKLY: 'EVOLVE_REVIEW_PERIOD_BIWEEKLY',
  EVOLVE_REVIEW_PERIOD_OFF: 'EVOLVE_REVIEW_PERIOD_OFF',
} as const;

export type EvolveReviewPeriod =
  | 'EVOLVE_REVIEW_PERIOD_UNSPECIFIED'
  | 0
  | 'EVOLVE_REVIEW_PERIOD_WEEKLY'
  | 1
  | 'EVOLVE_REVIEW_PERIOD_BIWEEKLY'
  | 2
  | 'EVOLVE_REVIEW_PERIOD_OFF'
  | 3

export type EvolveReviewPeriod__Output = typeof EvolveReviewPeriod[keyof typeof EvolveReviewPeriod]
