// Original file: api/superverse/v2/superverse.proto

import type { TeamSpaceStatus as _superverse_v2_TeamSpaceStatus, TeamSpaceStatus__Output as _superverse_v2_TeamSpaceStatus__Output } from '../../superverse/v2/TeamSpaceStatus';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface ListTeamSpacesRequest {
  'pageSize'?: (number);
  'pageToken'?: (string);
  'statuses'?: (_superverse_v2_TeamSpaceStatus)[];
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface ListTeamSpacesRequest__Output {
  'pageSize': (number);
  'pageToken': (string);
  'statuses': (_superverse_v2_TeamSpaceStatus__Output)[];
  'caller': (_superverse_v2_CallerContext__Output | null);
}
