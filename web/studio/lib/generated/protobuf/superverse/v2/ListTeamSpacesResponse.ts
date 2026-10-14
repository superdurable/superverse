// Original file: api/superverse/v2/superverse.proto

import type { TeamSpaceSnapshot as _superverse_v2_TeamSpaceSnapshot, TeamSpaceSnapshot__Output as _superverse_v2_TeamSpaceSnapshot__Output } from '../../superverse/v2/TeamSpaceSnapshot';

export interface ListTeamSpacesResponse {
  'teamSpaces'?: (_superverse_v2_TeamSpaceSnapshot)[];
  'nextPageToken'?: (string);
}

export interface ListTeamSpacesResponse__Output {
  'teamSpaces': (_superverse_v2_TeamSpaceSnapshot__Output)[];
  'nextPageToken': (string);
}
