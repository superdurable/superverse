// Original file: api/superverse/v2/superverse.proto

export const ConnectionClientSource = {
  CONNECTION_CLIENT_SOURCE_UNSPECIFIED: 'CONNECTION_CLIENT_SOURCE_UNSPECIFIED',
  CONNECTION_CLIENT_SOURCE_USER: 'CONNECTION_CLIENT_SOURCE_USER',
  CONNECTION_CLIENT_SOURCE_PLATFORM: 'CONNECTION_CLIENT_SOURCE_PLATFORM',
} as const;

export type ConnectionClientSource =
  | 'CONNECTION_CLIENT_SOURCE_UNSPECIFIED'
  | 0
  | 'CONNECTION_CLIENT_SOURCE_USER'
  | 1
  | 'CONNECTION_CLIENT_SOURCE_PLATFORM'
  | 2

export type ConnectionClientSource__Output = typeof ConnectionClientSource[keyof typeof ConnectionClientSource]
