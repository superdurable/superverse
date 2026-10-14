// Original file: api/superverse/v2/superverse.proto

import type { AgentUserMessage as _superverse_v2_AgentUserMessage, AgentUserMessage__Output as _superverse_v2_AgentUserMessage__Output } from '../../superverse/v2/AgentUserMessage';
import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';

export interface AgentPendingUserMessage {
  'messageId'?: (string);
  'value'?: (_superverse_v2_AgentUserMessage | null);
  'steeredBy'?: (_superverse_v2_Actor | null);
}

export interface AgentPendingUserMessage__Output {
  'messageId': (string);
  'value': (_superverse_v2_AgentUserMessage__Output | null);
  'steeredBy': (_superverse_v2_Actor__Output | null);
}
