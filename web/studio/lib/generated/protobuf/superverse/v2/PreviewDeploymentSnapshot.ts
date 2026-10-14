// Original file: api/superverse/v2/superverse.proto

import type { PreviewDeploymentStatus as _superverse_v2_PreviewDeploymentStatus, PreviewDeploymentStatus__Output as _superverse_v2_PreviewDeploymentStatus__Output } from '../../superverse/v2/PreviewDeploymentStatus';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface PreviewDeploymentSnapshot {
  'commit'?: (string);
  'status'?: (_superverse_v2_PreviewDeploymentStatus);
  'failure'?: (_superverse_v2_Failure | null);
  'updatedAt'?: (_google_protobuf_Timestamp | null);
  'queuedCommit'?: (string);
}

export interface PreviewDeploymentSnapshot__Output {
  'commit': (string);
  'status': (_superverse_v2_PreviewDeploymentStatus__Output);
  'failure': (_superverse_v2_Failure__Output | null);
  'updatedAt': (_google_protobuf_Timestamp__Output | null);
  'queuedCommit': (string);
}
