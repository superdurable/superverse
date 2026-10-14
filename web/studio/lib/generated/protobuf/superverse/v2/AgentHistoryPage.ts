// Original file: api/superverse/v2/superverse.proto

import type { AgentSequencedMessage as _superverse_v2_AgentSequencedMessage, AgentSequencedMessage__Output as _superverse_v2_AgentSequencedMessage__Output } from '../../superverse/v2/AgentSequencedMessage';
import type { Long } from '@grpc/proto-loader';

export interface AgentHistoryPage {
  'messages'?: (_superverse_v2_AgentSequencedMessage)[];
  'nextBeforeSequence'?: (number | string | Long);
  '_nextBeforeSequence'?: "nextBeforeSequence";
}

export interface AgentHistoryPage__Output {
  'messages': (_superverse_v2_AgentSequencedMessage__Output)[];
  'nextBeforeSequence'?: (string);
  '_nextBeforeSequence'?: "nextBeforeSequence";
}
