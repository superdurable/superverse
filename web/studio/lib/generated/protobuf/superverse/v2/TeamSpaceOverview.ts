// Original file: api/superverse/v2/superverse.proto

import type { TeamSpaceSnapshot as _superverse_v2_TeamSpaceSnapshot, TeamSpaceSnapshot__Output as _superverse_v2_TeamSpaceSnapshot__Output } from '../../superverse/v2/TeamSpaceSnapshot';

export interface TeamSpaceOverview {
  'teamSpace'?: (_superverse_v2_TeamSpaceSnapshot | null);
  'memberCount'?: (number);
  'projectCount'?: (number);
  'liveProjectCount'?: (number);
  'connectionCount'?: (number);
}

export interface TeamSpaceOverview__Output {
  'teamSpace': (_superverse_v2_TeamSpaceSnapshot__Output | null);
  'memberCount': (number);
  'projectCount': (number);
  'liveProjectCount': (number);
  'connectionCount': (number);
}
