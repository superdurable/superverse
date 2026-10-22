// Original file: api/superverse/v2/superverse.proto

import type { EvolveGoalsStatus as _superverse_v2_EvolveGoalsStatus, EvolveGoalsStatus__Output as _superverse_v2_EvolveGoalsStatus__Output } from '../../superverse/v2/EvolveGoalsStatus';
import type { EvolveGoal as _superverse_v2_EvolveGoal, EvolveGoal__Output as _superverse_v2_EvolveGoal__Output } from '../../superverse/v2/EvolveGoal';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface EvolveGoals {
  'commit'?: (string);
  'status'?: (_superverse_v2_EvolveGoalsStatus);
  'goals'?: (_superverse_v2_EvolveGoal)[];
  'errors'?: (string)[];
  'readAt'?: (_google_protobuf_Timestamp | null);
  'flowTypes'?: (string)[];
}

export interface EvolveGoals__Output {
  'commit': (string);
  'status': (_superverse_v2_EvolveGoalsStatus__Output);
  'goals': (_superverse_v2_EvolveGoal__Output)[];
  'errors': (string)[];
  'readAt': (_google_protobuf_Timestamp__Output | null);
  'flowTypes': (string)[];
}
