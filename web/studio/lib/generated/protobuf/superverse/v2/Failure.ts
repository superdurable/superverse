// Original file: api/superverse/v2/superverse.proto

import type { FailureRemedy as _superverse_v2_FailureRemedy, FailureRemedy__Output as _superverse_v2_FailureRemedy__Output } from '../../superverse/v2/FailureRemedy';
import type { FailureOwner as _superverse_v2_FailureOwner, FailureOwner__Output as _superverse_v2_FailureOwner__Output } from '../../superverse/v2/FailureOwner';

export interface Failure {
  'code'?: (string);
  'phase'?: (string);
  'message'?: (string);
  'isRecoverable'?: (boolean);
  'remedy'?: (_superverse_v2_FailureRemedy);
  'owner'?: (_superverse_v2_FailureOwner);
}

export interface Failure__Output {
  'code': (string);
  'phase': (string);
  'message': (string);
  'isRecoverable': (boolean);
  'remedy': (_superverse_v2_FailureRemedy__Output);
  'owner': (_superverse_v2_FailureOwner__Output);
}
