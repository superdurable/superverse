// Original file: api/superverse/v2/superverse.proto

import type { ProjectRole as _superverse_v2_ProjectRole, ProjectRole__Output as _superverse_v2_ProjectRole__Output } from '../../superverse/v2/ProjectRole';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface ProjectMemberSnapshot {
  'accountId'?: (string);
  'email'?: (string);
  'role'?: (_superverse_v2_ProjectRole);
  'addedAt'?: (_google_protobuf_Timestamp | null);
  'addedByAccountId'?: (string);
  'isTeamSpaceMember'?: (boolean);
}

export interface ProjectMemberSnapshot__Output {
  'accountId': (string);
  'email': (string);
  'role': (_superverse_v2_ProjectRole__Output);
  'addedAt': (_google_protobuf_Timestamp__Output | null);
  'addedByAccountId': (string);
  'isTeamSpaceMember': (boolean);
}
