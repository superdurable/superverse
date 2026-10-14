// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

export interface ProjectConfigurationValidation {
  'requestId'?: (string);
  'configurationRevision'?: (number | string | Long);
}

export interface ProjectConfigurationValidation__Output {
  'requestId': (string);
  'configurationRevision': (string);
}
