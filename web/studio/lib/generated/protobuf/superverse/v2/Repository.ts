// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

export interface Repository {
  'repositoryId'?: (number | string | Long);
  'fullName'?: (string);
  'url'?: (string);
  'defaultBranch'?: (string);
}

export interface Repository__Output {
  'repositoryId': (string);
  'fullName': (string);
  'url': (string);
  'defaultBranch': (string);
}
