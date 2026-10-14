// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

export interface AgentPendingTimer {
  'callId'?: (string);
  'durationSeconds'?: (number | string | Long);
  'reason'?: (string);
  'startedAt'?: (string);
  '_startedAt'?: "startedAt";
}

export interface AgentPendingTimer__Output {
  'callId': (string);
  'durationSeconds': (string);
  'reason': (string);
  'startedAt'?: (string);
  '_startedAt'?: "startedAt";
}
