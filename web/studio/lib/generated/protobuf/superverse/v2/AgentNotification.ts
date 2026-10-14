// Original file: api/superverse/v2/superverse.proto

import type { CommitVersion as _superverse_v2_CommitVersion, CommitVersion__Output as _superverse_v2_CommitVersion__Output } from '../../superverse/v2/CommitVersion';
import type { FailureRemedy as _superverse_v2_FailureRemedy, FailureRemedy__Output as _superverse_v2_FailureRemedy__Output } from '../../superverse/v2/FailureRemedy';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentNotification_ConnectorEvent = {
  CONNECTOR_EVENT_UNSPECIFIED: 'CONNECTOR_EVENT_UNSPECIFIED',
  CONNECTOR_EVENT_PULL_REQUEST_FAILED: 'CONNECTOR_EVENT_PULL_REQUEST_FAILED',
  CONNECTOR_EVENT_RELEASE_CANDIDATE_PUBLISHED: 'CONNECTOR_EVENT_RELEASE_CANDIDATE_PUBLISHED',
  CONNECTOR_EVENT_RELEASE_CANDIDATE_FAILED: 'CONNECTOR_EVENT_RELEASE_CANDIDATE_FAILED',
} as const;

export type _superverse_v2_AgentNotification_ConnectorEvent =
  | 'CONNECTOR_EVENT_UNSPECIFIED'
  | 0
  | 'CONNECTOR_EVENT_PULL_REQUEST_FAILED'
  | 1
  | 'CONNECTOR_EVENT_RELEASE_CANDIDATE_PUBLISHED'
  | 2
  | 'CONNECTOR_EVENT_RELEASE_CANDIDATE_FAILED'
  | 3

export type _superverse_v2_AgentNotification_ConnectorEvent__Output = typeof _superverse_v2_AgentNotification_ConnectorEvent[keyof typeof _superverse_v2_AgentNotification_ConnectorEvent]

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentNotification_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_TASK: 'KIND_TASK',
  KIND_DELIVERY: 'KIND_DELIVERY',
  KIND_CONNECTOR: 'KIND_CONNECTOR',
} as const;

export type _superverse_v2_AgentNotification_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_TASK'
  | 1
  | 'KIND_DELIVERY'
  | 2
  | 'KIND_CONNECTOR'
  | 3

export type _superverse_v2_AgentNotification_Kind__Output = typeof _superverse_v2_AgentNotification_Kind[keyof typeof _superverse_v2_AgentNotification_Kind]

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentNotification_Outcome = {
  OUTCOME_UNSPECIFIED: 'OUTCOME_UNSPECIFIED',
  OUTCOME_READY: 'OUTCOME_READY',
  OUTCOME_FAILURE: 'OUTCOME_FAILURE',
  OUTCOME_NOTICE: 'OUTCOME_NOTICE',
} as const;

export type _superverse_v2_AgentNotification_Outcome =
  | 'OUTCOME_UNSPECIFIED'
  | 0
  | 'OUTCOME_READY'
  | 1
  | 'OUTCOME_FAILURE'
  | 2
  | 'OUTCOME_NOTICE'
  | 3

export type _superverse_v2_AgentNotification_Outcome__Output = typeof _superverse_v2_AgentNotification_Outcome[keyof typeof _superverse_v2_AgentNotification_Outcome]

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentNotification_Owner = {
  OWNER_UNSPECIFIED: 'OWNER_UNSPECIFIED',
  OWNER_APPLICATION: 'OWNER_APPLICATION',
  OWNER_CONFIGURATION: 'OWNER_CONFIGURATION',
  OWNER_PLATFORM: 'OWNER_PLATFORM',
} as const;

export type _superverse_v2_AgentNotification_Owner =
  | 'OWNER_UNSPECIFIED'
  | 0
  | 'OWNER_APPLICATION'
  | 1
  | 'OWNER_CONFIGURATION'
  | 2
  | 'OWNER_PLATFORM'
  | 3

export type _superverse_v2_AgentNotification_Owner__Output = typeof _superverse_v2_AgentNotification_Owner[keyof typeof _superverse_v2_AgentNotification_Owner]

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentNotification_Source = {
  SOURCE_UNSPECIFIED: 'SOURCE_UNSPECIFIED',
  SOURCE_SUBAGENT: 'SOURCE_SUBAGENT',
  SOURCE_COMMAND: 'SOURCE_COMMAND',
  SOURCE_PREVIEW: 'SOURCE_PREVIEW',
  SOURCE_RELEASE: 'SOURCE_RELEASE',
  SOURCE_DEPLOYMENT: 'SOURCE_DEPLOYMENT',
  SOURCE_CONNECTOR: 'SOURCE_CONNECTOR',
} as const;

export type _superverse_v2_AgentNotification_Source =
  | 'SOURCE_UNSPECIFIED'
  | 0
  | 'SOURCE_SUBAGENT'
  | 1
  | 'SOURCE_COMMAND'
  | 2
  | 'SOURCE_PREVIEW'
  | 3
  | 'SOURCE_RELEASE'
  | 4
  | 'SOURCE_DEPLOYMENT'
  | 5
  | 'SOURCE_CONNECTOR'
  | 6

export type _superverse_v2_AgentNotification_Source__Output = typeof _superverse_v2_AgentNotification_Source[keyof typeof _superverse_v2_AgentNotification_Source]

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentNotification_TaskStatus = {
  TASK_STATUS_UNSPECIFIED: 'TASK_STATUS_UNSPECIFIED',
  TASK_STATUS_EXITED: 'TASK_STATUS_EXITED',
  TASK_STATUS_LOST: 'TASK_STATUS_LOST',
  TASK_STATUS_COMPLETED: 'TASK_STATUS_COMPLETED',
  TASK_STATUS_FAILED: 'TASK_STATUS_FAILED',
  TASK_STATUS_STOPPED: 'TASK_STATUS_STOPPED',
  TASK_STATUS_TIMED_OUT: 'TASK_STATUS_TIMED_OUT',
} as const;

export type _superverse_v2_AgentNotification_TaskStatus =
  | 'TASK_STATUS_UNSPECIFIED'
  | 0
  | 'TASK_STATUS_EXITED'
  | 1
  | 'TASK_STATUS_LOST'
  | 2
  | 'TASK_STATUS_COMPLETED'
  | 3
  | 'TASK_STATUS_FAILED'
  | 4
  | 'TASK_STATUS_STOPPED'
  | 5
  | 'TASK_STATUS_TIMED_OUT'
  | 6

export type _superverse_v2_AgentNotification_TaskStatus__Output = typeof _superverse_v2_AgentNotification_TaskStatus[keyof typeof _superverse_v2_AgentNotification_TaskStatus]

export interface AgentNotification {
  'id'?: (string);
  'kind'?: (_superverse_v2_AgentNotification_Kind);
  'source'?: (_superverse_v2_AgentNotification_Source);
  'occurredAt'?: (string);
  'taskId'?: (string);
  'taskStatus'?: (_superverse_v2_AgentNotification_TaskStatus);
  'summary'?: (string);
  'outcome'?: (_superverse_v2_AgentNotification_Outcome);
  'owner'?: (_superverse_v2_AgentNotification_Owner);
  'code'?: (string);
  'phase'?: (string);
  'commit'?: (string);
  'resourceId'?: (string);
  'gaps'?: (string)[];
  'gapCount'?: (number);
  'message'?: (string);
  'slug'?: (string);
  'connectorEvent'?: (_superverse_v2_AgentNotification_ConnectorEvent);
  'version'?: (_superverse_v2_CommitVersion | null);
  'remedy'?: (_superverse_v2_FailureRemedy);
}

export interface AgentNotification__Output {
  'id': (string);
  'kind': (_superverse_v2_AgentNotification_Kind__Output);
  'source': (_superverse_v2_AgentNotification_Source__Output);
  'occurredAt': (string);
  'taskId': (string);
  'taskStatus': (_superverse_v2_AgentNotification_TaskStatus__Output);
  'summary': (string);
  'outcome': (_superverse_v2_AgentNotification_Outcome__Output);
  'owner': (_superverse_v2_AgentNotification_Owner__Output);
  'code': (string);
  'phase': (string);
  'commit': (string);
  'resourceId': (string);
  'gaps': (string)[];
  'gapCount': (number);
  'message': (string);
  'slug': (string);
  'connectorEvent': (_superverse_v2_AgentNotification_ConnectorEvent__Output);
  'version': (_superverse_v2_CommitVersion__Output | null);
  'remedy': (_superverse_v2_FailureRemedy__Output);
}
