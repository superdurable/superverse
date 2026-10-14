// Original file: api/superverse/v2/superverse.proto

import type { ProjectSnapshot as _superverse_v2_ProjectSnapshot, ProjectSnapshot__Output as _superverse_v2_ProjectSnapshot__Output } from '../../superverse/v2/ProjectSnapshot';
import type { ResourceProgress as _superverse_v2_ResourceProgress, ResourceProgress__Output as _superverse_v2_ResourceProgress__Output } from '../../superverse/v2/ResourceProgress';

export interface WatchProjectResponse {
  'snapshot'?: (_superverse_v2_ProjectSnapshot | null);
  'progress'?: (_superverse_v2_ResourceProgress | null);
  'resumeCursor'?: (string);
  'event'?: "snapshot"|"progress";
}

export interface WatchProjectResponse__Output {
  'snapshot'?: (_superverse_v2_ProjectSnapshot__Output | null);
  'progress'?: (_superverse_v2_ResourceProgress__Output | null);
  'resumeCursor': (string);
  'event'?: "snapshot"|"progress";
}
