// Original file: api/superverse/v2/superverse.proto

import type { ProjectStatus as _superverse_v2_ProjectStatus, ProjectStatus__Output as _superverse_v2_ProjectStatus__Output } from '../../superverse/v2/ProjectStatus';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface ListProjectsRequest {
  'pageSize'?: (number);
  'pageToken'?: (string);
  'statuses'?: (_superverse_v2_ProjectStatus)[];
  'teamSpaceId'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface ListProjectsRequest__Output {
  'pageSize': (number);
  'pageToken': (string);
  'statuses': (_superverse_v2_ProjectStatus__Output)[];
  'teamSpaceId': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
