// Original file: api/superverse/v2/superverse.proto

import type { TeamSpaceRole as _superverse_v2_TeamSpaceRole, TeamSpaceRole__Output as _superverse_v2_TeamSpaceRole__Output } from '../../superverse/v2/TeamSpaceRole';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface TeamSpaceMemberSnapshot {
  'accountId'?: (string);
  'email'?: (string);
  'role'?: (_superverse_v2_TeamSpaceRole);
  'addedAt'?: (_google_protobuf_Timestamp | null);
  'addedByAccountId'?: (string);
}

export interface TeamSpaceMemberSnapshot__Output {
  'accountId': (string);
  'email': (string);
  'role': (_superverse_v2_TeamSpaceRole__Output);
  'addedAt': (_google_protobuf_Timestamp__Output | null);
  'addedByAccountId': (string);
}
