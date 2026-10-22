// Original file: api/superverse/v2/superverse.proto

import type { EvolveReviewPeriod as _superverse_v2_EvolveReviewPeriod, EvolveReviewPeriod__Output as _superverse_v2_EvolveReviewPeriod__Output } from '../../superverse/v2/EvolveReviewPeriod';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface EvolveSchedule {
  'period'?: (_superverse_v2_EvolveReviewPeriod);
  'active'?: (boolean);
  'nextAt'?: (_google_protobuf_Timestamp | null);
}

export interface EvolveSchedule__Output {
  'period': (_superverse_v2_EvolveReviewPeriod__Output);
  'active': (boolean);
  'nextAt': (_google_protobuf_Timestamp__Output | null);
}
