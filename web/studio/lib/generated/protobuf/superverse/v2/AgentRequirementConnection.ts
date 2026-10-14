// Original file: api/superverse/v2/superverse.proto


// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentRequirementConnection_Status = {
  STATUS_UNSPECIFIED: 'STATUS_UNSPECIFIED',
  STATUS_EXISTING: 'STATUS_EXISTING',
  STATUS_NEW: 'STATUS_NEW',
} as const;

export type _superverse_v2_AgentRequirementConnection_Status =
  | 'STATUS_UNSPECIFIED'
  | 0
  | 'STATUS_EXISTING'
  | 1
  | 'STATUS_NEW'
  | 2

export type _superverse_v2_AgentRequirementConnection_Status__Output = typeof _superverse_v2_AgentRequirementConnection_Status[keyof typeof _superverse_v2_AgentRequirementConnection_Status]

export interface AgentRequirementConnection {
  'need'?: (string);
  'connector'?: (string);
  'operations'?: (string)[];
  'status'?: (_superverse_v2_AgentRequirementConnection_Status);
  'userSetup'?: (string);
}

export interface AgentRequirementConnection__Output {
  'need': (string);
  'connector': (string);
  'operations': (string)[];
  'status': (_superverse_v2_AgentRequirementConnection_Status__Output);
  'userSetup': (string);
}
