// Original file: api/superverse/v2/superverse.proto

import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';
import type { AgentAttachment as _superverse_v2_AgentAttachment, AgentAttachment__Output as _superverse_v2_AgentAttachment__Output } from '../../superverse/v2/AgentAttachment';

export interface AgentUserMessage {
  'content'?: (string);
  'actor'?: (_superverse_v2_Actor | null);
  'attachments'?: (_superverse_v2_AgentAttachment)[];
}

export interface AgentUserMessage__Output {
  'content': (string);
  'actor': (_superverse_v2_Actor__Output | null);
  'attachments': (_superverse_v2_AgentAttachment__Output)[];
}
