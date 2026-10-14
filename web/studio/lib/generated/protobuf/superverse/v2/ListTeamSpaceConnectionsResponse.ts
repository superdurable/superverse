// Original file: api/superverse/v2/superverse.proto

import type { TeamSpaceConnectionSnapshot as _superverse_v2_TeamSpaceConnectionSnapshot, TeamSpaceConnectionSnapshot__Output as _superverse_v2_TeamSpaceConnectionSnapshot__Output } from '../../superverse/v2/TeamSpaceConnectionSnapshot';

export interface ListTeamSpaceConnectionsResponse {
  'connections'?: (_superverse_v2_TeamSpaceConnectionSnapshot)[];
  'nextPageToken'?: (string);
}

export interface ListTeamSpaceConnectionsResponse__Output {
  'connections': (_superverse_v2_TeamSpaceConnectionSnapshot__Output)[];
  'nextPageToken': (string);
}
