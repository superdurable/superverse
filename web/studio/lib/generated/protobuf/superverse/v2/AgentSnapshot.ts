// Original file: api/superverse/v2/superverse.proto

import type { AgentHistoryPage as _superverse_v2_AgentHistoryPage, AgentHistoryPage__Output as _superverse_v2_AgentHistoryPage__Output } from '../../superverse/v2/AgentHistoryPage';
import type { AgentDescription as _superverse_v2_AgentDescription, AgentDescription__Output as _superverse_v2_AgentDescription__Output } from '../../superverse/v2/AgentDescription';
import type { AgentPendingUserMessage as _superverse_v2_AgentPendingUserMessage, AgentPendingUserMessage__Output as _superverse_v2_AgentPendingUserMessage__Output } from '../../superverse/v2/AgentPendingUserMessage';

export interface AgentSnapshot {
  'history'?: (_superverse_v2_AgentHistoryPage | null);
  'description'?: (_superverse_v2_AgentDescription | null);
  'queued'?: (_superverse_v2_AgentPendingUserMessage)[];
  'steered'?: (_superverse_v2_AgentPendingUserMessage)[];
}

export interface AgentSnapshot__Output {
  'history': (_superverse_v2_AgentHistoryPage__Output | null);
  'description': (_superverse_v2_AgentDescription__Output | null);
  'queued': (_superverse_v2_AgentPendingUserMessage__Output)[];
  'steered': (_superverse_v2_AgentPendingUserMessage__Output)[];
}
