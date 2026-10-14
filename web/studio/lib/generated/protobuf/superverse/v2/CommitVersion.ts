// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

export interface CommitVersion {
  'number'?: (number | string | Long);
  'subject'?: (string);
}

export interface CommitVersion__Output {
  'number': (string);
  'subject': (string);
}
