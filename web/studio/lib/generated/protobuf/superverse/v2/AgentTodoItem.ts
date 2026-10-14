// Original file: api/superverse/v2/superverse.proto


// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentTodoItem_Status = {
  STATUS_UNSPECIFIED: 'STATUS_UNSPECIFIED',
  STATUS_PENDING: 'STATUS_PENDING',
  STATUS_IN_PROGRESS: 'STATUS_IN_PROGRESS',
  STATUS_COMPLETED: 'STATUS_COMPLETED',
} as const;

export type _superverse_v2_AgentTodoItem_Status =
  | 'STATUS_UNSPECIFIED'
  | 0
  | 'STATUS_PENDING'
  | 1
  | 'STATUS_IN_PROGRESS'
  | 2
  | 'STATUS_COMPLETED'
  | 3

export type _superverse_v2_AgentTodoItem_Status__Output = typeof _superverse_v2_AgentTodoItem_Status[keyof typeof _superverse_v2_AgentTodoItem_Status]

export interface AgentTodoItem {
  'content'?: (string);
  'status'?: (_superverse_v2_AgentTodoItem_Status);
}

export interface AgentTodoItem__Output {
  'content': (string);
  'status': (_superverse_v2_AgentTodoItem_Status__Output);
}
