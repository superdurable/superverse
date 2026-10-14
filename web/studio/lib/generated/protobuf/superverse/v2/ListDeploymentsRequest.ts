// Original file: api/superverse/v2/superverse.proto

import type { DeploymentStatus as _superverse_v2_DeploymentStatus, DeploymentStatus__Output as _superverse_v2_DeploymentStatus__Output } from '../../superverse/v2/DeploymentStatus';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface ListDeploymentsRequest {
  'projectId'?: (string);
  'pageSize'?: (number);
  'pageToken'?: (string);
  'statuses'?: (_superverse_v2_DeploymentStatus)[];
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface ListDeploymentsRequest__Output {
  'projectId': (string);
  'pageSize': (number);
  'pageToken': (string);
  'statuses': (_superverse_v2_DeploymentStatus__Output)[];
  'caller': (_superverse_v2_CallerContext__Output | null);
}
