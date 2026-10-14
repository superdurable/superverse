// Original file: api/superverse/v2/superverse.proto

export const WorkspaceState = {
  WORKSPACE_STATE_UNSPECIFIED: 'WORKSPACE_STATE_UNSPECIFIED',
  WORKSPACE_STATE_ABSENT: 'WORKSPACE_STATE_ABSENT',
  WORKSPACE_STATE_CLEAN: 'WORKSPACE_STATE_CLEAN',
  WORKSPACE_STATE_DIRTY: 'WORKSPACE_STATE_DIRTY',
  WORKSPACE_STATE_UNKNOWN: 'WORKSPACE_STATE_UNKNOWN',
} as const;

export type WorkspaceState =
  | 'WORKSPACE_STATE_UNSPECIFIED'
  | 0
  | 'WORKSPACE_STATE_ABSENT'
  | 1
  | 'WORKSPACE_STATE_CLEAN'
  | 2
  | 'WORKSPACE_STATE_DIRTY'
  | 3
  | 'WORKSPACE_STATE_UNKNOWN'
  | 4

export type WorkspaceState__Output = typeof WorkspaceState[keyof typeof WorkspaceState]
