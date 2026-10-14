// Original file: api/superverse/v2/superverse.proto

import type { DeploymentSnapshot as _superverse_v2_DeploymentSnapshot, DeploymentSnapshot__Output as _superverse_v2_DeploymentSnapshot__Output } from '../../superverse/v2/DeploymentSnapshot';
import type { ResourceProgress as _superverse_v2_ResourceProgress, ResourceProgress__Output as _superverse_v2_ResourceProgress__Output } from '../../superverse/v2/ResourceProgress';

export interface WatchDeploymentResponse {
  'snapshot'?: (_superverse_v2_DeploymentSnapshot | null);
  'progress'?: (_superverse_v2_ResourceProgress | null);
  'resumeCursor'?: (string);
  'event'?: "snapshot"|"progress";
}

export interface WatchDeploymentResponse__Output {
  'snapshot'?: (_superverse_v2_DeploymentSnapshot__Output | null);
  'progress'?: (_superverse_v2_ResourceProgress__Output | null);
  'resumeCursor': (string);
  'event'?: "snapshot"|"progress";
}
