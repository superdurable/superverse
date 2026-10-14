// Original file: api/superverse/v2/superverse.proto

import type { ProjectScope as _superverse_v2_ProjectScope, ProjectScope__Output as _superverse_v2_ProjectScope__Output } from '../../superverse/v2/ProjectScope';
import type { Long } from '@grpc/proto-loader';

export interface FlowApiTarget {
  'projectId'?: (string);
  'scope'?: (_superverse_v2_ProjectScope);
  'releaseId'?: (string);
  'deploymentId'?: (string);
  'workspaceRequestId'?: (string);
  'generation'?: (number | string | Long);
  'nextPublish'?: (boolean);
}

export interface FlowApiTarget__Output {
  'projectId': (string);
  'scope': (_superverse_v2_ProjectScope__Output);
  'releaseId': (string);
  'deploymentId': (string);
  'workspaceRequestId': (string);
  'generation': (string);
  'nextPublish': (boolean);
}
