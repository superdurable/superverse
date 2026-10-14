// Original file: api/superverse/v2/superverse.proto

import type { TeamSpaceRole as _superverse_v2_TeamSpaceRole, TeamSpaceRole__Output as _superverse_v2_TeamSpaceRole__Output } from '../../superverse/v2/TeamSpaceRole';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface AddTeamSpaceMemberRequest {
  'teamSpaceId'?: (string);
  'email'?: (string);
  'role'?: (_superverse_v2_TeamSpaceRole);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface AddTeamSpaceMemberRequest__Output {
  'teamSpaceId': (string);
  'email': (string);
  'role': (_superverse_v2_TeamSpaceRole__Output);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
