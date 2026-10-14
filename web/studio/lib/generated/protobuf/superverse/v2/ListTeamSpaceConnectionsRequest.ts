// Original file: api/superverse/v2/superverse.proto

import type { TeamSpaceConnectionStatus as _superverse_v2_TeamSpaceConnectionStatus, TeamSpaceConnectionStatus__Output as _superverse_v2_TeamSpaceConnectionStatus__Output } from '../../superverse/v2/TeamSpaceConnectionStatus';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface ListTeamSpaceConnectionsRequest {
  'teamSpaceId'?: (string);
  'connectorId'?: (string);
  'pageSize'?: (number);
  'pageToken'?: (string);
  'statuses'?: (_superverse_v2_TeamSpaceConnectionStatus)[];
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface ListTeamSpaceConnectionsRequest__Output {
  'teamSpaceId': (string);
  'connectorId': (string);
  'pageSize': (number);
  'pageToken': (string);
  'statuses': (_superverse_v2_TeamSpaceConnectionStatus__Output)[];
  'caller': (_superverse_v2_CallerContext__Output | null);
}
