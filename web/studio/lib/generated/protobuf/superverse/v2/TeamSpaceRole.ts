// Original file: api/superverse/v2/superverse.proto

export const TeamSpaceRole = {
  TEAM_SPACE_ROLE_UNSPECIFIED: 'TEAM_SPACE_ROLE_UNSPECIFIED',
  TEAM_SPACE_ROLE_ADMIN: 'TEAM_SPACE_ROLE_ADMIN',
  TEAM_SPACE_ROLE_MEMBER: 'TEAM_SPACE_ROLE_MEMBER',
} as const;

export type TeamSpaceRole =
  | 'TEAM_SPACE_ROLE_UNSPECIFIED'
  | 0
  | 'TEAM_SPACE_ROLE_ADMIN'
  | 1
  | 'TEAM_SPACE_ROLE_MEMBER'
  | 2

export type TeamSpaceRole__Output = typeof TeamSpaceRole[keyof typeof TeamSpaceRole]
