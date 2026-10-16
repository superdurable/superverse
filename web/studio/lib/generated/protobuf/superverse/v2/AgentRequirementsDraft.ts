// Original file: api/superverse/v2/superverse.proto

import type { AgentRequirementFeature as _superverse_v2_AgentRequirementFeature, AgentRequirementFeature__Output as _superverse_v2_AgentRequirementFeature__Output } from '../../superverse/v2/AgentRequirementFeature';
import type { AgentRequirementsDraftConnection as _superverse_v2_AgentRequirementsDraftConnection, AgentRequirementsDraftConnection__Output as _superverse_v2_AgentRequirementsDraftConnection__Output } from '../../superverse/v2/AgentRequirementsDraftConnection';
import type { AgentRequirementsDraftFlow as _superverse_v2_AgentRequirementsDraftFlow, AgentRequirementsDraftFlow__Output as _superverse_v2_AgentRequirementsDraftFlow__Output } from '../../superverse/v2/AgentRequirementsDraftFlow';
import type { Long } from '@grpc/proto-loader';

export interface AgentRequirementsDraft {
  'callId'?: (string);
  'turn'?: (number | string | Long);
  'baseRevision'?: (number | string | Long);
  'title'?: (string);
  'summary'?: (string);
  'features'?: (_superverse_v2_AgentRequirementFeature)[];
  'connections'?: (_superverse_v2_AgentRequirementsDraftConnection)[];
  'designSummary'?: (string);
  'flows'?: (_superverse_v2_AgentRequirementsDraftFlow)[];
  'complete'?: (boolean);
}

export interface AgentRequirementsDraft__Output {
  'callId': (string);
  'turn': (string);
  'baseRevision': (string);
  'title': (string);
  'summary': (string);
  'features': (_superverse_v2_AgentRequirementFeature__Output)[];
  'connections': (_superverse_v2_AgentRequirementsDraftConnection__Output)[];
  'designSummary': (string);
  'flows': (_superverse_v2_AgentRequirementsDraftFlow__Output)[];
  'complete': (boolean);
}
