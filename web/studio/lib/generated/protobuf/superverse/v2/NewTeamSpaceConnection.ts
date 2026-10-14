// Original file: api/superverse/v2/superverse.proto

import type { ConnectionClientSource as _superverse_v2_ConnectionClientSource, ConnectionClientSource__Output as _superverse_v2_ConnectionClientSource__Output } from '../../superverse/v2/ConnectionClientSource';

export interface NewTeamSpaceConnection {
  'requestId'?: (string);
  'name'?: (string);
  'connectorId'?: (string);
  'modulePath'?: (string);
  'version'?: (string);
  'authMethodId'?: (string);
  'clientSource'?: (_superverse_v2_ConnectionClientSource);
  'settingsJson'?: (string);
}

export interface NewTeamSpaceConnection__Output {
  'requestId': (string);
  'name': (string);
  'connectorId': (string);
  'modulePath': (string);
  'version': (string);
  'authMethodId': (string);
  'clientSource': (_superverse_v2_ConnectionClientSource__Output);
  'settingsJson': (string);
}
