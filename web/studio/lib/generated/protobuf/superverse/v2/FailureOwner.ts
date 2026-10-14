// Original file: api/superverse/v2/superverse.proto

export const FailureOwner = {
  FAILURE_OWNER_UNSPECIFIED: 'FAILURE_OWNER_UNSPECIFIED',
  FAILURE_OWNER_APPLICATION: 'FAILURE_OWNER_APPLICATION',
  FAILURE_OWNER_CONFIGURATION: 'FAILURE_OWNER_CONFIGURATION',
  FAILURE_OWNER_PLATFORM: 'FAILURE_OWNER_PLATFORM',
} as const;

export type FailureOwner =
  | 'FAILURE_OWNER_UNSPECIFIED'
  | 0
  | 'FAILURE_OWNER_APPLICATION'
  | 1
  | 'FAILURE_OWNER_CONFIGURATION'
  | 2
  | 'FAILURE_OWNER_PLATFORM'
  | 3

export type FailureOwner__Output = typeof FailureOwner[keyof typeof FailureOwner]
