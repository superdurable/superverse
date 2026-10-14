// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

export interface ActiveDeploymentReference {
  'deploymentId'?: (string);
  'generation'?: (number | string | Long);
}

export interface ActiveDeploymentReference__Output {
  'deploymentId': (string);
  'generation': (string);
}
