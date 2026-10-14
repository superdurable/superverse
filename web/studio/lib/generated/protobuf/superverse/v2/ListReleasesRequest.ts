// Original file: api/superverse/v2/superverse.proto

import type { ReleaseStatus as _superverse_v2_ReleaseStatus, ReleaseStatus__Output as _superverse_v2_ReleaseStatus__Output } from '../../superverse/v2/ReleaseStatus';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface ListReleasesRequest {
  'projectId'?: (string);
  'pageSize'?: (number);
  'pageToken'?: (string);
  'statuses'?: (_superverse_v2_ReleaseStatus)[];
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface ListReleasesRequest__Output {
  'projectId': (string);
  'pageSize': (number);
  'pageToken': (string);
  'statuses': (_superverse_v2_ReleaseStatus__Output)[];
  'caller': (_superverse_v2_CallerContext__Output | null);
}
