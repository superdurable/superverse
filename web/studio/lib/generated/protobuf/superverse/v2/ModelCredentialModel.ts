// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

export interface ModelCredentialModel {
  'modelId'?: (string);
  'contextWindow'?: (number | string | Long);
}

export interface ModelCredentialModel__Output {
  'modelId': (string);
  'contextWindow': (string);
}
