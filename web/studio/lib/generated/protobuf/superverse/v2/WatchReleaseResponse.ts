// Original file: api/superverse/v2/superverse.proto

import type { ReleaseSnapshot as _superverse_v2_ReleaseSnapshot, ReleaseSnapshot__Output as _superverse_v2_ReleaseSnapshot__Output } from '../../superverse/v2/ReleaseSnapshot';
import type { ResourceProgress as _superverse_v2_ResourceProgress, ResourceProgress__Output as _superverse_v2_ResourceProgress__Output } from '../../superverse/v2/ResourceProgress';

export interface WatchReleaseResponse {
  'snapshot'?: (_superverse_v2_ReleaseSnapshot | null);
  'progress'?: (_superverse_v2_ResourceProgress | null);
  'resumeCursor'?: (string);
  'event'?: "snapshot"|"progress";
}

export interface WatchReleaseResponse__Output {
  'snapshot'?: (_superverse_v2_ReleaseSnapshot__Output | null);
  'progress'?: (_superverse_v2_ResourceProgress__Output | null);
  'resumeCursor': (string);
  'event'?: "snapshot"|"progress";
}
