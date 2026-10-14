// Original file: api/superverse/v2/superverse.proto

import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';

export interface AgentUserMessage {
  'content'?: (string);
  'actor'?: (_superverse_v2_Actor | null);
}

export interface AgentUserMessage__Output {
  'content': (string);
  'actor': (_superverse_v2_Actor__Output | null);
}
