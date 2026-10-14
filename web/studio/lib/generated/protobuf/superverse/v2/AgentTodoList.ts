// Original file: api/superverse/v2/superverse.proto

import type { AgentTodoItem as _superverse_v2_AgentTodoItem, AgentTodoItem__Output as _superverse_v2_AgentTodoItem__Output } from '../../superverse/v2/AgentTodoItem';
import type { Long } from '@grpc/proto-loader';

export interface AgentTodoList {
  'revision'?: (number | string | Long);
  'items'?: (_superverse_v2_AgentTodoItem)[];
}

export interface AgentTodoList__Output {
  'revision': (string);
  'items': (_superverse_v2_AgentTodoItem__Output)[];
}
