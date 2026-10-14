// Original file: api/superverse/v2/superverse.proto

import type { TeamSpaceMemberSnapshot as _superverse_v2_TeamSpaceMemberSnapshot, TeamSpaceMemberSnapshot__Output as _superverse_v2_TeamSpaceMemberSnapshot__Output } from '../../superverse/v2/TeamSpaceMemberSnapshot';
import type { Long } from '@grpc/proto-loader';

export interface RemoveTeamSpaceMemberResponse {
  'members'?: (_superverse_v2_TeamSpaceMemberSnapshot)[];
  'revision'?: (number | string | Long);
}

export interface RemoveTeamSpaceMemberResponse__Output {
  'members': (_superverse_v2_TeamSpaceMemberSnapshot__Output)[];
  'revision': (string);
}
