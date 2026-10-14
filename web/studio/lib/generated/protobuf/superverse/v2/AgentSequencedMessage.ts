// Original file: api/superverse/v2/superverse.proto

import type { AgentMessage as _superverse_v2_AgentMessage, AgentMessage__Output as _superverse_v2_AgentMessage__Output } from '../../superverse/v2/AgentMessage';
import type { Long } from '@grpc/proto-loader';

export interface AgentSequencedMessage {
  'sequence'?: (number | string | Long);
  'message'?: (_superverse_v2_AgentMessage | null);
}

export interface AgentSequencedMessage__Output {
  'sequence': (string);
  'message': (_superverse_v2_AgentMessage__Output | null);
}
