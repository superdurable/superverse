// Original file: api/superverse/v2/superverse.proto

import type { ConnectionClientSource as _superverse_v2_ConnectionClientSource, ConnectionClientSource__Output as _superverse_v2_ConnectionClientSource__Output } from '../../superverse/v2/ConnectionClientSource';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface CreateTeamSpaceConnectionRequest {
  'teamSpaceId'?: (string);
  'requestId'?: (string);
  'name'?: (string);
  'connectorId'?: (string);
  'modulePath'?: (string);
  'version'?: (string);
  'authMethodId'?: (string);
  'clientSource'?: (_superverse_v2_ConnectionClientSource);
  'settingsJson'?: (string);
  'credentialsJson'?: (string);
  'copyFromConnectionId'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface CreateTeamSpaceConnectionRequest__Output {
  'teamSpaceId': (string);
  'requestId': (string);
  'name': (string);
  'connectorId': (string);
  'modulePath': (string);
  'version': (string);
  'authMethodId': (string);
  'clientSource': (_superverse_v2_ConnectionClientSource__Output);
  'settingsJson': (string);
  'credentialsJson': (string);
  'copyFromConnectionId': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
