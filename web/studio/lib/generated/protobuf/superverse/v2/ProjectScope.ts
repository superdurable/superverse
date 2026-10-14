// Original file: api/superverse/v2/superverse.proto

export const ProjectScope = {
  PROJECT_SCOPE_UNSPECIFIED: 'PROJECT_SCOPE_UNSPECIFIED',
  PROJECT_SCOPE_PREVIEW: 'PROJECT_SCOPE_PREVIEW',
  PROJECT_SCOPE_LIVE: 'PROJECT_SCOPE_LIVE',
} as const;

export type ProjectScope =
  | 'PROJECT_SCOPE_UNSPECIFIED'
  | 0
  | 'PROJECT_SCOPE_PREVIEW'
  | 1
  | 'PROJECT_SCOPE_LIVE'
  | 2

export type ProjectScope__Output = typeof ProjectScope[keyof typeof ProjectScope]
