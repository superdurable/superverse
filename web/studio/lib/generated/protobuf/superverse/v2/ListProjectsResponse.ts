// Original file: api/superverse/v2/superverse.proto

import type { ProjectSnapshot as _superverse_v2_ProjectSnapshot, ProjectSnapshot__Output as _superverse_v2_ProjectSnapshot__Output } from '../../superverse/v2/ProjectSnapshot';

export interface ListProjectsResponse {
  'projects'?: (_superverse_v2_ProjectSnapshot)[];
  'nextPageToken'?: (string);
}

export interface ListProjectsResponse__Output {
  'projects': (_superverse_v2_ProjectSnapshot__Output)[];
  'nextPageToken': (string);
}
