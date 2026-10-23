// Original file: api/superverse/v2/superverse.proto

import type { AgentToolCall as _superverse_v2_AgentToolCall, AgentToolCall__Output as _superverse_v2_AgentToolCall__Output } from '../../superverse/v2/AgentToolCall';
import type { AgentModelCallMetrics as _superverse_v2_AgentModelCallMetrics, AgentModelCallMetrics__Output as _superverse_v2_AgentModelCallMetrics__Output } from '../../superverse/v2/AgentModelCallMetrics';
import type { AgentToolExecutionMetrics as _superverse_v2_AgentToolExecutionMetrics, AgentToolExecutionMetrics__Output as _superverse_v2_AgentToolExecutionMetrics__Output } from '../../superverse/v2/AgentToolExecutionMetrics';
import type { AgentNotification as _superverse_v2_AgentNotification, AgentNotification__Output as _superverse_v2_AgentNotification__Output } from '../../superverse/v2/AgentNotification';
import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';
import type { AgentAttachment as _superverse_v2_AgentAttachment, AgentAttachment__Output as _superverse_v2_AgentAttachment__Output } from '../../superverse/v2/AgentAttachment';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentMessage_Role = {
  ROLE_UNSPECIFIED: 'ROLE_UNSPECIFIED',
  ROLE_SYSTEM: 'ROLE_SYSTEM',
  ROLE_USER: 'ROLE_USER',
  ROLE_ASSISTANT: 'ROLE_ASSISTANT',
  ROLE_TOOL: 'ROLE_TOOL',
  ROLE_NOTIFICATION: 'ROLE_NOTIFICATION',
} as const;

export type _superverse_v2_AgentMessage_Role =
  | 'ROLE_UNSPECIFIED'
  | 0
  | 'ROLE_SYSTEM'
  | 1
  | 'ROLE_USER'
  | 2
  | 'ROLE_ASSISTANT'
  | 3
  | 'ROLE_TOOL'
  | 4
  | 'ROLE_NOTIFICATION'
  | 5

export type _superverse_v2_AgentMessage_Role__Output = typeof _superverse_v2_AgentMessage_Role[keyof typeof _superverse_v2_AgentMessage_Role]

export interface AgentMessage {
  'role'?: (_superverse_v2_AgentMessage_Role);
  'content'?: (string);
  'toolCalls'?: (_superverse_v2_AgentToolCall)[];
  'toolCallId'?: (string);
  'toolName'?: (string);
  'answeredInputCallId'?: (string);
  'createdAt'?: (string);
  'startedAt'?: (string);
  'modelCall'?: (_superverse_v2_AgentModelCallMetrics | null);
  'toolExecution'?: (_superverse_v2_AgentToolExecutionMetrics | null);
  'notification'?: (_superverse_v2_AgentNotification | null);
  'actor'?: (_superverse_v2_Actor | null);
  'steeredBy'?: (_superverse_v2_Actor | null);
  'via'?: (string);
  'imported'?: (boolean);
  'attachments'?: (_superverse_v2_AgentAttachment)[];
  'executorChanged'?: (boolean);
  'notificationContinuesTurn'?: (boolean);
  '_toolCallId'?: "toolCallId";
  '_toolName'?: "toolName";
  '_answeredInputCallId'?: "answeredInputCallId";
  '_startedAt'?: "startedAt";
  '_via'?: "via";
}

export interface AgentMessage__Output {
  'role': (_superverse_v2_AgentMessage_Role__Output);
  'content': (string);
  'toolCalls': (_superverse_v2_AgentToolCall__Output)[];
  'toolCallId'?: (string);
  'toolName'?: (string);
  'answeredInputCallId'?: (string);
  'createdAt': (string);
  'startedAt'?: (string);
  'modelCall': (_superverse_v2_AgentModelCallMetrics__Output | null);
  'toolExecution': (_superverse_v2_AgentToolExecutionMetrics__Output | null);
  'notification': (_superverse_v2_AgentNotification__Output | null);
  'actor': (_superverse_v2_Actor__Output | null);
  'steeredBy': (_superverse_v2_Actor__Output | null);
  'via'?: (string);
  'imported': (boolean);
  'attachments': (_superverse_v2_AgentAttachment__Output)[];
  'executorChanged': (boolean);
  'notificationContinuesTurn': (boolean);
  '_toolCallId'?: "toolCallId";
  '_toolName'?: "toolName";
  '_answeredInputCallId'?: "answeredInputCallId";
  '_startedAt'?: "startedAt";
  '_via'?: "via";
}
