// Original file: api/superverse/v2/superverse.proto


// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_ConfigurationItemKey_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_CONNECTION: 'KIND_CONNECTION',
  KIND_TRIGGER_BINDING: 'KIND_TRIGGER_BINDING',
  KIND_STEP_SETTING: 'KIND_STEP_SETTING',
  KIND_ENVIRONMENT: 'KIND_ENVIRONMENT',
} as const;

export type _superverse_v2_ConfigurationItemKey_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_CONNECTION'
  | 1
  | 'KIND_TRIGGER_BINDING'
  | 2
  | 'KIND_STEP_SETTING'
  | 3
  | 'KIND_ENVIRONMENT'
  | 4

export type _superverse_v2_ConfigurationItemKey_Kind__Output = typeof _superverse_v2_ConfigurationItemKey_Kind[keyof typeof _superverse_v2_ConfigurationItemKey_Kind]

export interface ConfigurationItemKey {
  'kind'?: (_superverse_v2_ConfigurationItemKey_Kind);
  'connectorId'?: (string);
  'connectionName'?: (string);
  'triggerName'?: (string);
  'bindingName'?: (string);
  'operationId'?: (string);
  'flowType'?: (string);
  'stepType'?: (string);
  'environmentName'?: (string);
}

export interface ConfigurationItemKey__Output {
  'kind': (_superverse_v2_ConfigurationItemKey_Kind__Output);
  'connectorId': (string);
  'connectionName': (string);
  'triggerName': (string);
  'bindingName': (string);
  'operationId': (string);
  'flowType': (string);
  'stepType': (string);
  'environmentName': (string);
}
