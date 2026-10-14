// Original file: api/superverse/v2/superverse.proto

import type { ProjectScope as _superverse_v2_ProjectScope, ProjectScope__Output as _superverse_v2_ProjectScope__Output } from '../../superverse/v2/ProjectScope';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface ListEnvironmentEventsRequest {
  'projectId'?: (string);
  'scope'?: (_superverse_v2_ProjectScope);
  'pageSize'?: (number);
  'pageToken'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface ListEnvironmentEventsRequest__Output {
  'projectId': (string);
  'scope': (_superverse_v2_ProjectScope__Output);
  'pageSize': (number);
  'pageToken': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
