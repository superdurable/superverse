// Original file: api/superverse/v2/superverse.proto

import type { DeploymentSnapshot as _superverse_v2_DeploymentSnapshot, DeploymentSnapshot__Output as _superverse_v2_DeploymentSnapshot__Output } from '../../superverse/v2/DeploymentSnapshot';

export interface ListDeploymentsResponse {
  'deployments'?: (_superverse_v2_DeploymentSnapshot)[];
  'nextPageToken'?: (string);
}

export interface ListDeploymentsResponse__Output {
  'deployments': (_superverse_v2_DeploymentSnapshot__Output)[];
  'nextPageToken': (string);
}
