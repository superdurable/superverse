// Original file: api/superverse/v2/superverse.proto

import type { ProjectScope as _superverse_v2_ProjectScope, ProjectScope__Output as _superverse_v2_ProjectScope__Output } from '../../superverse/v2/ProjectScope';

export interface ProjectConfigurationTarget {
  'projectId'?: (string);
  'scope'?: (_superverse_v2_ProjectScope);
  'releaseId'?: (string);
  'nextPublish'?: (boolean);
}

export interface ProjectConfigurationTarget__Output {
  'projectId': (string);
  'scope': (_superverse_v2_ProjectScope__Output);
  'releaseId': (string);
  'nextPublish': (boolean);
}
