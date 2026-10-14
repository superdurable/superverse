// Original file: api/superverse/v2/superverse.proto

import type { DeploymentStatus as _superverse_v2_DeploymentStatus, DeploymentStatus__Output as _superverse_v2_DeploymentStatus__Output } from '../../superverse/v2/DeploymentStatus';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { PhaseTiming as _superverse_v2_PhaseTiming, PhaseTiming__Output as _superverse_v2_PhaseTiming__Output } from '../../superverse/v2/PhaseTiming';
import type { CommitVersion as _superverse_v2_CommitVersion, CommitVersion__Output as _superverse_v2_CommitVersion__Output } from '../../superverse/v2/CommitVersion';
import type { Cancellation as _superverse_v2_Cancellation, Cancellation__Output as _superverse_v2_Cancellation__Output } from '../../superverse/v2/Cancellation';
import type { Long } from '@grpc/proto-loader';

export interface DeploymentSnapshot {
  'projectId'?: (string);
  'deploymentId'?: (string);
  'releaseId'?: (string);
  'status'?: (_superverse_v2_DeploymentStatus);
  'revision'?: (number | string | Long);
  'configurationRevision'?: (number | string | Long);
  'url'?: (string);
  'failure'?: (_superverse_v2_Failure | null);
  'createdAt'?: (_google_protobuf_Timestamp | null);
  'generation'?: (number | string | Long);
  'buildRevisionId'?: (string);
  'phaseTimings'?: (_superverse_v2_PhaseTiming)[];
  'version'?: (_superverse_v2_CommitVersion | null);
  'cancellation'?: (_superverse_v2_Cancellation | null);
}

export interface DeploymentSnapshot__Output {
  'projectId': (string);
  'deploymentId': (string);
  'releaseId': (string);
  'status': (_superverse_v2_DeploymentStatus__Output);
  'revision': (string);
  'configurationRevision': (string);
  'url': (string);
  'failure': (_superverse_v2_Failure__Output | null);
  'createdAt': (_google_protobuf_Timestamp__Output | null);
  'generation': (string);
  'buildRevisionId': (string);
  'phaseTimings': (_superverse_v2_PhaseTiming__Output)[];
  'version': (_superverse_v2_CommitVersion__Output | null);
  'cancellation': (_superverse_v2_Cancellation__Output | null);
}
