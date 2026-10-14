// Original file: api/superverse/v2/superverse.proto

export const TeamSpaceStatus = {
  TEAM_SPACE_STATUS_UNSPECIFIED: 'TEAM_SPACE_STATUS_UNSPECIFIED',
  TEAM_SPACE_STATUS_READY: 'TEAM_SPACE_STATUS_READY',
  TEAM_SPACE_STATUS_DELETING: 'TEAM_SPACE_STATUS_DELETING',
  TEAM_SPACE_STATUS_DELETED: 'TEAM_SPACE_STATUS_DELETED',
} as const;

export type TeamSpaceStatus =
  | 'TEAM_SPACE_STATUS_UNSPECIFIED'
  | 0
  | 'TEAM_SPACE_STATUS_READY'
  | 1
  | 'TEAM_SPACE_STATUS_DELETING'
  | 2
  | 'TEAM_SPACE_STATUS_DELETED'
  | 3

export type TeamSpaceStatus__Output = typeof TeamSpaceStatus[keyof typeof TeamSpaceStatus]
