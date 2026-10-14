// Original file: api/superverse/v2/superverse.proto


// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentSubagent_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_INVESTIGATE: 'KIND_INVESTIGATE',
  KIND_CHANGE: 'KIND_CHANGE',
  KIND_CONNECTOR_SELECT: 'KIND_CONNECTOR_SELECT',
  KIND_CONNECTOR_DEVELOP: 'KIND_CONNECTOR_DEVELOP',
} as const;

export type _superverse_v2_AgentSubagent_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_INVESTIGATE'
  | 1
  | 'KIND_CHANGE'
  | 2
  | 'KIND_CONNECTOR_SELECT'
  | 3
  | 'KIND_CONNECTOR_DEVELOP'
  | 4

export type _superverse_v2_AgentSubagent_Kind__Output = typeof _superverse_v2_AgentSubagent_Kind[keyof typeof _superverse_v2_AgentSubagent_Kind]

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentSubagent_Status = {
  STATUS_UNSPECIFIED: 'STATUS_UNSPECIFIED',
  STATUS_STARTING: 'STATUS_STARTING',
  STATUS_RUNNING: 'STATUS_RUNNING',
  STATUS_COMPLETED: 'STATUS_COMPLETED',
  STATUS_FAILED: 'STATUS_FAILED',
  STATUS_STOPPED: 'STATUS_STOPPED',
  STATUS_TIMED_OUT: 'STATUS_TIMED_OUT',
  STATUS_MERGED: 'STATUS_MERGED',
  STATUS_DISCARDED: 'STATUS_DISCARDED',
} as const;

export type _superverse_v2_AgentSubagent_Status =
  | 'STATUS_UNSPECIFIED'
  | 0
  | 'STATUS_STARTING'
  | 1
  | 'STATUS_RUNNING'
  | 2
  | 'STATUS_COMPLETED'
  | 3
  | 'STATUS_FAILED'
  | 4
  | 'STATUS_STOPPED'
  | 5
  | 'STATUS_TIMED_OUT'
  | 6
  | 'STATUS_MERGED'
  | 7
  | 'STATUS_DISCARDED'
  | 8

export type _superverse_v2_AgentSubagent_Status__Output = typeof _superverse_v2_AgentSubagent_Status[keyof typeof _superverse_v2_AgentSubagent_Status]

export interface AgentSubagent {
  'taskId'?: (string);
  'kind'?: (_superverse_v2_AgentSubagent_Kind);
  'description'?: (string);
  'status'?: (_superverse_v2_AgentSubagent_Status);
  'startedAt'?: (string);
  'finishedAt'?: (string);
  'summary'?: (string);
  'changedFiles'?: (number);
  'insertions'?: (number);
  'deletions'?: (number);
  'lost'?: (boolean);
  '_finishedAt'?: "finishedAt";
}

export interface AgentSubagent__Output {
  'taskId': (string);
  'kind': (_superverse_v2_AgentSubagent_Kind__Output);
  'description': (string);
  'status': (_superverse_v2_AgentSubagent_Status__Output);
  'startedAt': (string);
  'finishedAt'?: (string);
  'summary': (string);
  'changedFiles': (number);
  'insertions': (number);
  'deletions': (number);
  'lost': (boolean);
  '_finishedAt'?: "finishedAt";
}
