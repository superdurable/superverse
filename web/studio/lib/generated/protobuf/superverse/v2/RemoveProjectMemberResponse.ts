// Original file: api/superverse/v2/superverse.proto

import type { ProjectMemberSnapshot as _superverse_v2_ProjectMemberSnapshot, ProjectMemberSnapshot__Output as _superverse_v2_ProjectMemberSnapshot__Output } from '../../superverse/v2/ProjectMemberSnapshot';
import type { Long } from '@grpc/proto-loader';

export interface RemoveProjectMemberResponse {
  'members'?: (_superverse_v2_ProjectMemberSnapshot)[];
  'revision'?: (number | string | Long);
}

export interface RemoveProjectMemberResponse__Output {
  'members': (_superverse_v2_ProjectMemberSnapshot__Output)[];
  'revision': (string);
}
