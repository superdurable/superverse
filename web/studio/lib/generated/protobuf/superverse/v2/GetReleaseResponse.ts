// Original file: api/superverse/v2/superverse.proto

import type { ReleaseSnapshot as _superverse_v2_ReleaseSnapshot, ReleaseSnapshot__Output as _superverse_v2_ReleaseSnapshot__Output } from '../../superverse/v2/ReleaseSnapshot';
import type { FlowDefinition as _superverse_v2_FlowDefinition, FlowDefinition__Output as _superverse_v2_FlowDefinition__Output } from '../../superverse/v2/FlowDefinition';

export interface GetReleaseResponse {
  'release'?: (_superverse_v2_ReleaseSnapshot | null);
  'flowDefinitions'?: (_superverse_v2_FlowDefinition)[];
}

export interface GetReleaseResponse__Output {
  'release': (_superverse_v2_ReleaseSnapshot__Output | null);
  'flowDefinitions': (_superverse_v2_FlowDefinition__Output)[];
}
