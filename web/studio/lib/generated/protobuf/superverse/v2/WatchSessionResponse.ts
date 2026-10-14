// Original file: api/superverse/v2/superverse.proto

import type { SessionSnapshot as _superverse_v2_SessionSnapshot, SessionSnapshot__Output as _superverse_v2_SessionSnapshot__Output } from '../../superverse/v2/SessionSnapshot';
import type { SessionProgress as _superverse_v2_SessionProgress, SessionProgress__Output as _superverse_v2_SessionProgress__Output } from '../../superverse/v2/SessionProgress';

export interface WatchSessionResponse {
  'snapshot'?: (_superverse_v2_SessionSnapshot | null);
  'progress'?: (_superverse_v2_SessionProgress | null);
  'resumeCursor'?: (string);
  'event'?: "snapshot"|"progress";
}

export interface WatchSessionResponse__Output {
  'snapshot'?: (_superverse_v2_SessionSnapshot__Output | null);
  'progress'?: (_superverse_v2_SessionProgress__Output | null);
  'resumeCursor': (string);
  'event'?: "snapshot"|"progress";
}
