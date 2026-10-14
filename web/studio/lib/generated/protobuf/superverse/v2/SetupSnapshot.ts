// Original file: api/superverse/v2/superverse.proto

import type { SetupStatus as _superverse_v2_SetupStatus, SetupStatus__Output as _superverse_v2_SetupStatus__Output } from '../../superverse/v2/SetupStatus';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { Long } from '@grpc/proto-loader';

export interface SetupSnapshot {
  'setupId'?: (string);
  'status'?: (_superverse_v2_SetupStatus);
  'revision'?: (number | string | Long);
  'phase'?: (string);
  'failure'?: (_superverse_v2_Failure | null);
  'sourceCommit'?: (string);
}

export interface SetupSnapshot__Output {
  'setupId': (string);
  'status': (_superverse_v2_SetupStatus__Output);
  'revision': (string);
  'phase': (string);
  'failure': (_superverse_v2_Failure__Output | null);
  'sourceCommit': (string);
}
