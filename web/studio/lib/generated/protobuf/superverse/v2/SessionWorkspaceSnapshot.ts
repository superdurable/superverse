// Original file: api/superverse/v2/superverse.proto

import type { WorkspaceStatus as _superverse_v2_WorkspaceStatus, WorkspaceStatus__Output as _superverse_v2_WorkspaceStatus__Output } from '../../superverse/v2/WorkspaceStatus';
import type { WorkspaceState as _superverse_v2_WorkspaceState, WorkspaceState__Output as _superverse_v2_WorkspaceState__Output } from '../../superverse/v2/WorkspaceState';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { PhaseTiming as _superverse_v2_PhaseTiming, PhaseTiming__Output as _superverse_v2_PhaseTiming__Output } from '../../superverse/v2/PhaseTiming';
import type { Long } from '@grpc/proto-loader';

export interface SessionWorkspaceSnapshot {
  'status'?: (_superverse_v2_WorkspaceStatus);
  'generation'?: (number | string | Long);
  'revision'?: (number | string | Long);
  'workspaceState'?: (_superverse_v2_WorkspaceState);
  'updatedAt'?: (_google_protobuf_Timestamp | null);
  'failure'?: (_superverse_v2_Failure | null);
  'expiresAt'?: (_google_protobuf_Timestamp | null);
  'workspaceRequestId'?: (string);
  'phaseTimings'?: (_superverse_v2_PhaseTiming)[];
}

export interface SessionWorkspaceSnapshot__Output {
  'status': (_superverse_v2_WorkspaceStatus__Output);
  'generation': (string);
  'revision': (string);
  'workspaceState': (_superverse_v2_WorkspaceState__Output);
  'updatedAt': (_google_protobuf_Timestamp__Output | null);
  'failure': (_superverse_v2_Failure__Output | null);
  'expiresAt': (_google_protobuf_Timestamp__Output | null);
  'workspaceRequestId': (string);
  'phaseTimings': (_superverse_v2_PhaseTiming__Output)[];
}
