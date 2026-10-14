// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Long } from '@grpc/proto-loader';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_RecentTriggerEvent_Fate = {
  FATE_UNSPECIFIED: 'FATE_UNSPECIFIED',
  FATE_RECORDED: 'FATE_RECORDED',
  FATE_NOT_RUNNING: 'FATE_NOT_RUNNING',
  FATE_APPLICATION_UNAVAILABLE: 'FATE_APPLICATION_UNAVAILABLE',
  FATE_REFUSED: 'FATE_REFUSED',
  FATE_TIMED_OUT: 'FATE_TIMED_OUT',
  FATE_TOKEN_UNAVAILABLE: 'FATE_TOKEN_UNAVAILABLE',
  FATE_QUEUE_FULL: 'FATE_QUEUE_FULL',
  FATE_EVENT_TOO_LARGE: 'FATE_EVENT_TOO_LARGE',
} as const;

export type _superverse_v2_RecentTriggerEvent_Fate =
  | 'FATE_UNSPECIFIED'
  | 0
  | 'FATE_RECORDED'
  | 1
  | 'FATE_NOT_RUNNING'
  | 2
  | 'FATE_APPLICATION_UNAVAILABLE'
  | 3
  | 'FATE_REFUSED'
  | 4
  | 'FATE_TIMED_OUT'
  | 5
  | 'FATE_TOKEN_UNAVAILABLE'
  | 6
  | 'FATE_QUEUE_FULL'
  | 7
  | 'FATE_EVENT_TOO_LARGE'
  | 8

export type _superverse_v2_RecentTriggerEvent_Fate__Output = typeof _superverse_v2_RecentTriggerEvent_Fate[keyof typeof _superverse_v2_RecentTriggerEvent_Fate]

export interface RecentTriggerEvent {
  'receivedAt'?: (_google_protobuf_Timestamp | null);
  'eventId'?: (string);
  'eventType'?: (string);
  'eventSubtype'?: (string);
  'teamId'?: (string);
  'channelId'?: (string);
  'connectionName'?: (string);
  'fate'?: (_superverse_v2_RecentTriggerEvent_Fate);
  'httpStatus'?: (number);
  'durationMs'?: (number | string | Long);
}

export interface RecentTriggerEvent__Output {
  'receivedAt': (_google_protobuf_Timestamp__Output | null);
  'eventId': (string);
  'eventType': (string);
  'eventSubtype': (string);
  'teamId': (string);
  'channelId': (string);
  'connectionName': (string);
  'fate': (_superverse_v2_RecentTriggerEvent_Fate__Output);
  'httpStatus': (number);
  'durationMs': (string);
}
