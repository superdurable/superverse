// Original file: api/superverse/v2/superverse.proto

import type { ConfigurationFollowRefusal as _superverse_v2_ConfigurationFollowRefusal, ConfigurationFollowRefusal__Output as _superverse_v2_ConfigurationFollowRefusal__Output } from '../../superverse/v2/ConfigurationFollowRefusal';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_ConfigurationRequirementGap_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_CONNECTION_REQUIRED: 'KIND_CONNECTION_REQUIRED',
  KIND_AUTHORIZATION_REQUIRED: 'KIND_AUTHORIZATION_REQUIRED',
  KIND_CONNECTION_INCOMPLETE: 'KIND_CONNECTION_INCOMPLETE',
  KIND_TRIGGER_BINDING_REQUIRED: 'KIND_TRIGGER_BINDING_REQUIRED',
  KIND_TRIGGER_BINDING_INVALID: 'KIND_TRIGGER_BINDING_INVALID',
  KIND_ENVIRONMENT_REQUIRED: 'KIND_ENVIRONMENT_REQUIRED',
  KIND_OPERATION_CONFIGURATION_REQUIRED: 'KIND_OPERATION_CONFIGURATION_REQUIRED',
} as const;

export type _superverse_v2_ConfigurationRequirementGap_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_CONNECTION_REQUIRED'
  | 1
  | 'KIND_AUTHORIZATION_REQUIRED'
  | 2
  | 'KIND_CONNECTION_INCOMPLETE'
  | 3
  | 'KIND_TRIGGER_BINDING_REQUIRED'
  | 4
  | 'KIND_TRIGGER_BINDING_INVALID'
  | 5
  | 'KIND_ENVIRONMENT_REQUIRED'
  | 6
  | 'KIND_OPERATION_CONFIGURATION_REQUIRED'
  | 7

export type _superverse_v2_ConfigurationRequirementGap_Kind__Output = typeof _superverse_v2_ConfigurationRequirementGap_Kind[keyof typeof _superverse_v2_ConfigurationRequirementGap_Kind]

export interface ConfigurationRequirementGap {
  'kind'?: (_superverse_v2_ConfigurationRequirementGap_Kind);
  'connectionName'?: (string);
  'connectorId'?: (string);
  'triggerName'?: (string);
  'bindingName'?: (string);
  'environmentName'?: (string);
  'unitLabel'?: (string);
  'flowName'?: (string);
  'stepName'?: (string);
  'operationId'?: (string);
  'followRefusal'?: (_superverse_v2_ConfigurationFollowRefusal);
}

export interface ConfigurationRequirementGap__Output {
  'kind': (_superverse_v2_ConfigurationRequirementGap_Kind__Output);
  'connectionName': (string);
  'connectorId': (string);
  'triggerName': (string);
  'bindingName': (string);
  'environmentName': (string);
  'unitLabel': (string);
  'flowName': (string);
  'stepName': (string);
  'operationId': (string);
  'followRefusal': (_superverse_v2_ConfigurationFollowRefusal__Output);
}
