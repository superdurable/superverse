// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentBackgroundCommand_EndReason = {
  END_REASON_UNSPECIFIED: 'END_REASON_UNSPECIFIED',
  END_REASON_EXIT: 'END_REASON_EXIT',
  END_REASON_LIFETIME_LIMIT: 'END_REASON_LIFETIME_LIMIT',
  END_REASON_OUTPUT_LIMIT: 'END_REASON_OUTPUT_LIMIT',
  END_REASON_STOPPED: 'END_REASON_STOPPED',
  END_REASON_LOST: 'END_REASON_LOST',
} as const;

export type _superverse_v2_AgentBackgroundCommand_EndReason =
  | 'END_REASON_UNSPECIFIED'
  | 0
  | 'END_REASON_EXIT'
  | 1
  | 'END_REASON_LIFETIME_LIMIT'
  | 2
  | 'END_REASON_OUTPUT_LIMIT'
  | 3
  | 'END_REASON_STOPPED'
  | 4
  | 'END_REASON_LOST'
  | 5

export type _superverse_v2_AgentBackgroundCommand_EndReason__Output = typeof _superverse_v2_AgentBackgroundCommand_EndReason[keyof typeof _superverse_v2_AgentBackgroundCommand_EndReason]

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentBackgroundCommand_Status = {
  STATUS_UNSPECIFIED: 'STATUS_UNSPECIFIED',
  STATUS_RUNNING: 'STATUS_RUNNING',
  STATUS_EXITED: 'STATUS_EXITED',
  STATUS_STOPPED: 'STATUS_STOPPED',
  STATUS_LOST: 'STATUS_LOST',
} as const;

export type _superverse_v2_AgentBackgroundCommand_Status =
  | 'STATUS_UNSPECIFIED'
  | 0
  | 'STATUS_RUNNING'
  | 1
  | 'STATUS_EXITED'
  | 2
  | 'STATUS_STOPPED'
  | 3
  | 'STATUS_LOST'
  | 4

export type _superverse_v2_AgentBackgroundCommand_Status__Output = typeof _superverse_v2_AgentBackgroundCommand_Status[keyof typeof _superverse_v2_AgentBackgroundCommand_Status]

export interface AgentBackgroundCommand {
  'taskId'?: (string);
  'description'?: (string);
  'status'?: (_superverse_v2_AgentBackgroundCommand_Status);
  'endReason'?: (_superverse_v2_AgentBackgroundCommand_EndReason);
  'exitCode'?: (number);
  'outputBytes'?: (number | string | Long);
  'startedAt'?: (string);
  'endedAt'?: (string);
  '_exitCode'?: "exitCode";
  '_endedAt'?: "endedAt";
}

export interface AgentBackgroundCommand__Output {
  'taskId': (string);
  'description': (string);
  'status': (_superverse_v2_AgentBackgroundCommand_Status__Output);
  'endReason': (_superverse_v2_AgentBackgroundCommand_EndReason__Output);
  'exitCode'?: (number);
  'outputBytes': (string);
  'startedAt': (string);
  'endedAt'?: (string);
  '_exitCode'?: "exitCode";
  '_endedAt'?: "endedAt";
}
