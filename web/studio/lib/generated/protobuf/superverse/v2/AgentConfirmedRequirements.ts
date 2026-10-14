// Original file: api/superverse/v2/superverse.proto

import type { AgentRequirements as _superverse_v2_AgentRequirements, AgentRequirements__Output as _superverse_v2_AgentRequirements__Output } from '../../superverse/v2/AgentRequirements';
import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';
import type { Long } from '@grpc/proto-loader';

export interface AgentConfirmedRequirements {
  'revision'?: (number | string | Long);
  'spec'?: (_superverse_v2_AgentRequirements | null);
  'confirmedAt'?: (string);
  'confirmedBy'?: (_superverse_v2_Actor | null);
}

export interface AgentConfirmedRequirements__Output {
  'revision': (string);
  'spec': (_superverse_v2_AgentRequirements__Output | null);
  'confirmedAt': (string);
  'confirmedBy': (_superverse_v2_Actor__Output | null);
}
