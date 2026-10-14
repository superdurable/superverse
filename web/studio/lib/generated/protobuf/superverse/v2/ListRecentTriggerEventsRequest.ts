// Original file: api/superverse/v2/superverse.proto

import type { ProjectScope as _superverse_v2_ProjectScope, ProjectScope__Output as _superverse_v2_ProjectScope__Output } from '../../superverse/v2/ProjectScope';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface ListRecentTriggerEventsRequest {
  'projectId'?: (string);
  'scope'?: (_superverse_v2_ProjectScope);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface ListRecentTriggerEventsRequest__Output {
  'projectId': (string);
  'scope': (_superverse_v2_ProjectScope__Output);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
