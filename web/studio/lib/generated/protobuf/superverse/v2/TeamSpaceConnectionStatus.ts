// Original file: api/superverse/v2/superverse.proto

export const TeamSpaceConnectionStatus = {
  TEAM_SPACE_CONNECTION_STATUS_UNSPECIFIED: 'TEAM_SPACE_CONNECTION_STATUS_UNSPECIFIED',
  TEAM_SPACE_CONNECTION_STATUS_ACTIVE: 'TEAM_SPACE_CONNECTION_STATUS_ACTIVE',
  TEAM_SPACE_CONNECTION_STATUS_DELETED: 'TEAM_SPACE_CONNECTION_STATUS_DELETED',
} as const;

export type TeamSpaceConnectionStatus =
  | 'TEAM_SPACE_CONNECTION_STATUS_UNSPECIFIED'
  | 0
  | 'TEAM_SPACE_CONNECTION_STATUS_ACTIVE'
  | 1
  | 'TEAM_SPACE_CONNECTION_STATUS_DELETED'
  | 2

export type TeamSpaceConnectionStatus__Output = typeof TeamSpaceConnectionStatus[keyof typeof TeamSpaceConnectionStatus]
