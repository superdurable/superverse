// Original file: api/superverse/v2/superverse.proto

import type { CommitVersion as _superverse_v2_CommitVersion, CommitVersion__Output as _superverse_v2_CommitVersion__Output } from '../../superverse/v2/CommitVersion';
import type { Long } from '@grpc/proto-loader';

export interface ProjectLive {
  'deploymentId'?: (string);
  'releaseId'?: (string);
  'buildRevisionId'?: (string);
  'revision'?: (number | string | Long);
  'url'?: (string);
  'isRemoving'?: (boolean);
  'version'?: (_superverse_v2_CommitVersion | null);
}

export interface ProjectLive__Output {
  'deploymentId': (string);
  'releaseId': (string);
  'buildRevisionId': (string);
  'revision': (string);
  'url': (string);
  'isRemoving': (boolean);
  'version': (_superverse_v2_CommitVersion__Output | null);
}
