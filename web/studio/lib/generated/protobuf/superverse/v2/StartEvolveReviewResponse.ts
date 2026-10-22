// Original file: api/superverse/v2/superverse.proto

import type { EvolveReviewRefusal as _superverse_v2_EvolveReviewRefusal, EvolveReviewRefusal__Output as _superverse_v2_EvolveReviewRefusal__Output } from '../../superverse/v2/EvolveReviewRefusal';

export interface StartEvolveReviewResponse {
  'reviewId'?: (string);
  'refusal'?: (_superverse_v2_EvolveReviewRefusal);
}

export interface StartEvolveReviewResponse__Output {
  'reviewId': (string);
  'refusal': (_superverse_v2_EvolveReviewRefusal__Output);
}
