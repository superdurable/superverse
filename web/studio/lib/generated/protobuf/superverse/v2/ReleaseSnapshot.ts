// Original file: api/superverse/v2/superverse.proto

import type { ReleaseStatus as _superverse_v2_ReleaseStatus, ReleaseStatus__Output as _superverse_v2_ReleaseStatus__Output } from '../../superverse/v2/ReleaseStatus';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { PreparedReleaseSourceSnapshot as _superverse_v2_PreparedReleaseSourceSnapshot, PreparedReleaseSourceSnapshot__Output as _superverse_v2_PreparedReleaseSourceSnapshot__Output } from '../../superverse/v2/PreparedReleaseSourceSnapshot';
import type { ReleaseBuildRevisionSnapshot as _superverse_v2_ReleaseBuildRevisionSnapshot, ReleaseBuildRevisionSnapshot__Output as _superverse_v2_ReleaseBuildRevisionSnapshot__Output } from '../../superverse/v2/ReleaseBuildRevisionSnapshot';
import type { Long } from '@grpc/proto-loader';

export interface ReleaseSnapshot {
  'projectId'?: (string);
  'releaseId'?: (string);
  'status'?: (_superverse_v2_ReleaseStatus);
  'revision'?: (number | string | Long);
  'sourceCommit'?: (string);
  'buildProfileVersion'?: (string);
  'failure'?: (_superverse_v2_Failure | null);
  'createdAt'?: (_google_protobuf_Timestamp | null);
  'preparedSource'?: (_superverse_v2_PreparedReleaseSourceSnapshot | null);
  'activeBuildRevisionId'?: (string);
  'readyBuildRevisionId'?: (string);
  'buildRevisions'?: (_superverse_v2_ReleaseBuildRevisionSnapshot)[];
}

export interface ReleaseSnapshot__Output {
  'projectId': (string);
  'releaseId': (string);
  'status': (_superverse_v2_ReleaseStatus__Output);
  'revision': (string);
  'sourceCommit': (string);
  'buildProfileVersion': (string);
  'failure': (_superverse_v2_Failure__Output | null);
  'createdAt': (_google_protobuf_Timestamp__Output | null);
  'preparedSource': (_superverse_v2_PreparedReleaseSourceSnapshot__Output | null);
  'activeBuildRevisionId': (string);
  'readyBuildRevisionId': (string);
  'buildRevisions': (_superverse_v2_ReleaseBuildRevisionSnapshot__Output)[];
}
