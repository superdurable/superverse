// Original file: api/superverse/v2/superverse.proto

import type { EvolveReviewTrigger as _superverse_v2_EvolveReviewTrigger, EvolveReviewTrigger__Output as _superverse_v2_EvolveReviewTrigger__Output } from '../../superverse/v2/EvolveReviewTrigger';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { EvolveReviewStatus as _superverse_v2_EvolveReviewStatus, EvolveReviewStatus__Output as _superverse_v2_EvolveReviewStatus__Output } from '../../superverse/v2/EvolveReviewStatus';
import type { EvolveReviewReason as _superverse_v2_EvolveReviewReason, EvolveReviewReason__Output as _superverse_v2_EvolveReviewReason__Output } from '../../superverse/v2/EvolveReviewReason';
import type { EvolveExport as _superverse_v2_EvolveExport, EvolveExport__Output as _superverse_v2_EvolveExport__Output } from '../../superverse/v2/EvolveExport';
import type { EvolveGoalValue as _superverse_v2_EvolveGoalValue, EvolveGoalValue__Output as _superverse_v2_EvolveGoalValue__Output } from '../../superverse/v2/EvolveGoalValue';

export interface EvolveReview {
  'reviewId'?: (string);
  'trigger'?: (_superverse_v2_EvolveReviewTrigger);
  'actorAccountId'?: (string);
  'requestedAt'?: (_google_protobuf_Timestamp | null);
  'startedAt'?: (_google_protobuf_Timestamp | null);
  'finishedAt'?: (_google_protobuf_Timestamp | null);
  'windowFrom'?: (_google_protobuf_Timestamp | null);
  'windowTo'?: (_google_protobuf_Timestamp | null);
  'goalsCommit'?: (string);
  'status'?: (_superverse_v2_EvolveReviewStatus);
  'reason'?: (_superverse_v2_EvolveReviewReason);
  'detail'?: (string);
  'export'?: (_superverse_v2_EvolveExport | null);
  'runs'?: (number);
  'values'?: (_superverse_v2_EvolveGoalValue)[];
}

export interface EvolveReview__Output {
  'reviewId': (string);
  'trigger': (_superverse_v2_EvolveReviewTrigger__Output);
  'actorAccountId': (string);
  'requestedAt': (_google_protobuf_Timestamp__Output | null);
  'startedAt': (_google_protobuf_Timestamp__Output | null);
  'finishedAt': (_google_protobuf_Timestamp__Output | null);
  'windowFrom': (_google_protobuf_Timestamp__Output | null);
  'windowTo': (_google_protobuf_Timestamp__Output | null);
  'goalsCommit': (string);
  'status': (_superverse_v2_EvolveReviewStatus__Output);
  'reason': (_superverse_v2_EvolveReviewReason__Output);
  'detail': (string);
  'export': (_superverse_v2_EvolveExport__Output | null);
  'runs': (number);
  'values': (_superverse_v2_EvolveGoalValue__Output)[];
}
