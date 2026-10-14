// Original file: api/superverse/v2/superverse.proto

import type { TeamSpaceConnectionSnapshot as _superverse_v2_TeamSpaceConnectionSnapshot, TeamSpaceConnectionSnapshot__Output as _superverse_v2_TeamSpaceConnectionSnapshot__Output } from '../../superverse/v2/TeamSpaceConnectionSnapshot';

export interface GetTeamSpaceConnectionResponse {
  'connection'?: (_superverse_v2_TeamSpaceConnectionSnapshot | null);
  'storedCredentialFields'?: (string)[];
}

export interface GetTeamSpaceConnectionResponse__Output {
  'connection': (_superverse_v2_TeamSpaceConnectionSnapshot__Output | null);
  'storedCredentialFields': (string)[];
}
