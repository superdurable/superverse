// Original file: api/superverse/v2/superverse.proto

import type { EvolveLive as _superverse_v2_EvolveLive, EvolveLive__Output as _superverse_v2_EvolveLive__Output } from '../../superverse/v2/EvolveLive';
import type { EvolveGoals as _superverse_v2_EvolveGoals, EvolveGoals__Output as _superverse_v2_EvolveGoals__Output } from '../../superverse/v2/EvolveGoals';
import type { EvolveSchedule as _superverse_v2_EvolveSchedule, EvolveSchedule__Output as _superverse_v2_EvolveSchedule__Output } from '../../superverse/v2/EvolveSchedule';
import type { EvolveReview as _superverse_v2_EvolveReview, EvolveReview__Output as _superverse_v2_EvolveReview__Output } from '../../superverse/v2/EvolveReview';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { EvolveProposal as _superverse_v2_EvolveProposal, EvolveProposal__Output as _superverse_v2_EvolveProposal__Output } from '../../superverse/v2/EvolveProposal';

export interface Evolve {
  'projectId'?: (string);
  'live'?: (_superverse_v2_EvolveLive | null);
  'goals'?: (_superverse_v2_EvolveGoals | null);
  'schedule'?: (_superverse_v2_EvolveSchedule | null);
  'activeReviewId'?: (string);
  'reviews'?: (_superverse_v2_EvolveReview)[];
  'agentReviews'?: (boolean);
  'nextManualReviewAt'?: (_google_protobuf_Timestamp | null);
  'proposals'?: (_superverse_v2_EvolveProposal)[];
}

export interface Evolve__Output {
  'projectId': (string);
  'live': (_superverse_v2_EvolveLive__Output | null);
  'goals': (_superverse_v2_EvolveGoals__Output | null);
  'schedule': (_superverse_v2_EvolveSchedule__Output | null);
  'activeReviewId': (string);
  'reviews': (_superverse_v2_EvolveReview__Output)[];
  'agentReviews': (boolean);
  'nextManualReviewAt': (_google_protobuf_Timestamp__Output | null);
  'proposals': (_superverse_v2_EvolveProposal__Output)[];
}
