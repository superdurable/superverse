// Original file: api/superverse/v2/superverse.proto

import type { ReleaseSnapshot as _superverse_v2_ReleaseSnapshot, ReleaseSnapshot__Output as _superverse_v2_ReleaseSnapshot__Output } from '../../superverse/v2/ReleaseSnapshot';

export interface ListReleasesResponse {
  'releases'?: (_superverse_v2_ReleaseSnapshot)[];
  'nextPageToken'?: (string);
}

export interface ListReleasesResponse__Output {
  'releases': (_superverse_v2_ReleaseSnapshot__Output)[];
  'nextPageToken': (string);
}
