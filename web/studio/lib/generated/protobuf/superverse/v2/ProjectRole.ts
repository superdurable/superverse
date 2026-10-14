// Original file: api/superverse/v2/superverse.proto

export const ProjectRole = {
  PROJECT_ROLE_UNSPECIFIED: 'PROJECT_ROLE_UNSPECIFIED',
  PROJECT_ROLE_OWNER: 'PROJECT_ROLE_OWNER',
  PROJECT_ROLE_BUILDER: 'PROJECT_ROLE_BUILDER',
  PROJECT_ROLE_OPERATOR: 'PROJECT_ROLE_OPERATOR',
} as const;

export type ProjectRole =
  | 'PROJECT_ROLE_UNSPECIFIED'
  | 0
  | 'PROJECT_ROLE_OWNER'
  | 1
  | 'PROJECT_ROLE_BUILDER'
  | 2
  | 'PROJECT_ROLE_OPERATOR'
  | 3

export type ProjectRole__Output = typeof ProjectRole[keyof typeof ProjectRole]
