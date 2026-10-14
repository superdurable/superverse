// Original file: api/superverse/v2/superverse.proto

export const ConfigurationFollowRefusal = {
  CONFIGURATION_FOLLOW_REFUSAL_UNSPECIFIED: 'CONFIGURATION_FOLLOW_REFUSAL_UNSPECIFIED',
  CONFIGURATION_FOLLOW_REFUSAL_SECRET: 'CONFIGURATION_FOLLOW_REFUSAL_SECRET',
  CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND: 'CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND',
} as const;

export type ConfigurationFollowRefusal =
  | 'CONFIGURATION_FOLLOW_REFUSAL_UNSPECIFIED'
  | 0
  | 'CONFIGURATION_FOLLOW_REFUSAL_SECRET'
  | 1
  | 'CONFIGURATION_FOLLOW_REFUSAL_ENDPOINT_BOUND'
  | 2

export type ConfigurationFollowRefusal__Output = typeof ConfigurationFollowRefusal[keyof typeof ConfigurationFollowRefusal]
