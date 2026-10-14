// Original file: api/superverse/v2/superverse.proto

import type { AgentInputConsumption as _superverse_v2_AgentInputConsumption, AgentInputConsumption__Output as _superverse_v2_AgentInputConsumption__Output } from '../../superverse/v2/AgentInputConsumption';
import type { Long } from '@grpc/proto-loader';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentEvent_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_INPUT_CONSUMED: 'KIND_INPUT_CONSUMED',
  KIND_USER_INPUT_ANSWERED: 'KIND_USER_INPUT_ANSWERED',
  KIND_USER_INPUT_CANCELLED: 'KIND_USER_INPUT_CANCELLED',
  KIND_SNAPSHOT_REQUIRED: 'KIND_SNAPSHOT_REQUIRED',
  KIND_STEERING_APPLIED: 'KIND_STEERING_APPLIED',
  KIND_COMPACTION_FAILED: 'KIND_COMPACTION_FAILED',
  KIND_COMPACTED: 'KIND_COMPACTED',
  KIND_MODEL_STARTED: 'KIND_MODEL_STARTED',
  KIND_MODEL_FAILED: 'KIND_MODEL_FAILED',
  KIND_MODEL_COMPLETED: 'KIND_MODEL_COMPLETED',
  KIND_MODEL_TOOL_CALL: 'KIND_MODEL_TOOL_CALL',
  KIND_USER_INPUT_REQUESTED: 'KIND_USER_INPUT_REQUESTED',
  KIND_TOOL_APPROVAL_REQUESTED: 'KIND_TOOL_APPROVAL_REQUESTED',
  KIND_TOOL_APPROVAL_RESOLVED: 'KIND_TOOL_APPROVAL_RESOLVED',
  KIND_TIMER_STARTED: 'KIND_TIMER_STARTED',
  KIND_TIMER_RESOLVED: 'KIND_TIMER_RESOLVED',
  KIND_TOOL_PROGRESS: 'KIND_TOOL_PROGRESS',
  KIND_TOOL_FAILED: 'KIND_TOOL_FAILED',
  KIND_TOOL_COMPLETED: 'KIND_TOOL_COMPLETED',
  KIND_MODEL_FALLBACK: 'KIND_MODEL_FALLBACK',
  KIND_AGENT_INTERRUPTED: 'KIND_AGENT_INTERRUPTED',
} as const;

export type _superverse_v2_AgentEvent_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_INPUT_CONSUMED'
  | 4
  | 'KIND_USER_INPUT_ANSWERED'
  | 5
  | 'KIND_USER_INPUT_CANCELLED'
  | 6
  | 'KIND_SNAPSHOT_REQUIRED'
  | 7
  | 'KIND_STEERING_APPLIED'
  | 8
  | 'KIND_COMPACTION_FAILED'
  | 9
  | 'KIND_COMPACTED'
  | 10
  | 'KIND_MODEL_STARTED'
  | 11
  | 'KIND_MODEL_FAILED'
  | 12
  | 'KIND_MODEL_COMPLETED'
  | 13
  | 'KIND_MODEL_TOOL_CALL'
  | 14
  | 'KIND_USER_INPUT_REQUESTED'
  | 15
  | 'KIND_TOOL_APPROVAL_REQUESTED'
  | 16
  | 'KIND_TOOL_APPROVAL_RESOLVED'
  | 17
  | 'KIND_TIMER_STARTED'
  | 18
  | 'KIND_TIMER_RESOLVED'
  | 19
  | 'KIND_TOOL_PROGRESS'
  | 20
  | 'KIND_TOOL_FAILED'
  | 21
  | 'KIND_TOOL_COMPLETED'
  | 22
  | 'KIND_MODEL_FALLBACK'
  | 25
  | 'KIND_AGENT_INTERRUPTED'
  | 26

export type _superverse_v2_AgentEvent_Kind__Output = typeof _superverse_v2_AgentEvent_Kind[keyof typeof _superverse_v2_AgentEvent_Kind]

export interface AgentEvent {
  'kind'?: (_superverse_v2_AgentEvent_Kind);
  'message'?: (string);
  'callId'?: (string);
  'toolName'?: (string);
  'messageSequence'?: (number | string | Long);
  'inputConsumption'?: (_superverse_v2_AgentInputConsumption | null);
  'attempt'?: (number);
  'target'?: (string);
  '_callId'?: "callId";
  '_toolName'?: "toolName";
  '_messageSequence'?: "messageSequence";
  '_attempt'?: "attempt";
}

export interface AgentEvent__Output {
  'kind': (_superverse_v2_AgentEvent_Kind__Output);
  'message': (string);
  'callId'?: (string);
  'toolName'?: (string);
  'messageSequence'?: (string);
  'inputConsumption': (_superverse_v2_AgentInputConsumption__Output | null);
  'attempt'?: (number);
  'target': (string);
  '_callId'?: "callId";
  '_toolName'?: "toolName";
  '_messageSequence'?: "messageSequence";
  '_attempt'?: "attempt";
}
