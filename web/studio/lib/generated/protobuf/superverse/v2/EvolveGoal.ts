// Original file: api/superverse/v2/superverse.proto

import type { EvolveGoalKind as _superverse_v2_EvolveGoalKind, EvolveGoalKind__Output as _superverse_v2_EvolveGoalKind__Output } from '../../superverse/v2/EvolveGoalKind';
import type { EvolveGoalDirection as _superverse_v2_EvolveGoalDirection, EvolveGoalDirection__Output as _superverse_v2_EvolveGoalDirection__Output } from '../../superverse/v2/EvolveGoalDirection';

export interface EvolveGoal {
  'id'?: (string);
  'kind'?: (_superverse_v2_EvolveGoalKind);
  'statement'?: (string);
  'metric'?: (string);
  'direction'?: (_superverse_v2_EvolveGoalDirection);
  'target'?: (number | string);
  'guardrailMax'?: (number | string);
  'guardrailMin'?: (number | string);
  '_target'?: "target";
  '_guardrailMax'?: "guardrailMax";
  '_guardrailMin'?: "guardrailMin";
}

export interface EvolveGoal__Output {
  'id': (string);
  'kind': (_superverse_v2_EvolveGoalKind__Output);
  'statement': (string);
  'metric': (string);
  'direction': (_superverse_v2_EvolveGoalDirection__Output);
  'target'?: (number);
  'guardrailMax'?: (number);
  'guardrailMin'?: (number);
  '_target'?: "target";
  '_guardrailMax'?: "guardrailMax";
  '_guardrailMin'?: "guardrailMin";
}
