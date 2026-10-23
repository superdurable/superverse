// Original file: api/superverse/v2/superverse.proto

import type { ConnectorTriggerBinding as _superverse_v2_ConnectorTriggerBinding, ConnectorTriggerBinding__Output as _superverse_v2_ConnectorTriggerBinding__Output } from '../../superverse/v2/ConnectorTriggerBinding';

export interface ConnectorRequirement {
  'connectionName'?: (string);
  'connectorId'?: (string);
  'version'?: (string);
  'authMethodId'?: (string);
  'operations'?: (string)[];
  'modulePath'?: (string);
  'triggerBindings'?: (_superverse_v2_ConnectorTriggerBinding)[];
  'optional'?: (boolean);
}

export interface ConnectorRequirement__Output {
  'connectionName': (string);
  'connectorId': (string);
  'version': (string);
  'authMethodId': (string);
  'operations': (string)[];
  'modulePath': (string);
  'triggerBindings': (_superverse_v2_ConnectorTriggerBinding__Output)[];
  'optional': (boolean);
}
