// Original file: api/superverse/v2/superverse.proto

import type { ProjectRole as _superverse_v2_ProjectRole, ProjectRole__Output as _superverse_v2_ProjectRole__Output } from '../../superverse/v2/ProjectRole';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface AddProjectMemberRequest {
  'projectId'?: (string);
  'accountId'?: (string);
  'role'?: (_superverse_v2_ProjectRole);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface AddProjectMemberRequest__Output {
  'projectId': (string);
  'accountId': (string);
  'role': (_superverse_v2_ProjectRole__Output);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
