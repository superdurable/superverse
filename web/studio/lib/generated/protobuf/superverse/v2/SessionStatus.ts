// Original file: api/superverse/v2/superverse.proto

export const SessionStatus = {
  SESSION_STATUS_UNSPECIFIED: 'SESSION_STATUS_UNSPECIFIED',
  SESSION_STATUS_ACTIVE: 'SESSION_STATUS_ACTIVE',
  SESSION_STATUS_ARCHIVED: 'SESSION_STATUS_ARCHIVED',
  SESSION_STATUS_FAILED: 'SESSION_STATUS_FAILED',
} as const;

export type SessionStatus =
  | 'SESSION_STATUS_UNSPECIFIED'
  | 0
  | 'SESSION_STATUS_ACTIVE'
  | 1
  | 'SESSION_STATUS_ARCHIVED'
  | 3
  | 'SESSION_STATUS_FAILED'
  | 4

export type SessionStatus__Output = typeof SessionStatus[keyof typeof SessionStatus]
