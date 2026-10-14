// Original file: api/superverse/v2/superverse.proto

export const SessionExecutorMode = {
  SESSION_EXECUTOR_MODE_UNSPECIFIED: 'SESSION_EXECUTOR_MODE_UNSPECIFIED',
  SESSION_EXECUTOR_MODE_PLATFORM: 'SESSION_EXECUTOR_MODE_PLATFORM',
  SESSION_EXECUTOR_MODE_REMOTE: 'SESSION_EXECUTOR_MODE_REMOTE',
} as const;

export type SessionExecutorMode =
  | 'SESSION_EXECUTOR_MODE_UNSPECIFIED'
  | 0
  | 'SESSION_EXECUTOR_MODE_PLATFORM'
  | 1
  | 'SESSION_EXECUTOR_MODE_REMOTE'
  | 2

export type SessionExecutorMode__Output = typeof SessionExecutorMode[keyof typeof SessionExecutorMode]
