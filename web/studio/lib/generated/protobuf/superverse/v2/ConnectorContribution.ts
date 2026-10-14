// Original file: api/superverse/v2/superverse.proto

import type { Long } from '@grpc/proto-loader';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_ConnectorContribution_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_NEW: 'KIND_NEW',
  KIND_OPERATION: 'KIND_OPERATION',
  KIND_FIX: 'KIND_FIX',
} as const;

export type _superverse_v2_ConnectorContribution_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_NEW'
  | 1
  | 'KIND_OPERATION'
  | 2
  | 'KIND_FIX'
  | 3

export type _superverse_v2_ConnectorContribution_Kind__Output = typeof _superverse_v2_ConnectorContribution_Kind[keyof typeof _superverse_v2_ConnectorContribution_Kind]

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_ConnectorContribution_Location = {
  LOCATION_UNSPECIFIED: 'LOCATION_UNSPECIFIED',
  LOCATION_UPSTREAM: 'LOCATION_UPSTREAM',
  LOCATION_LOCAL_GIT_HOST: 'LOCATION_LOCAL_GIT_HOST',
  LOCATION_SANDBOX: 'LOCATION_SANDBOX',
} as const;

export type _superverse_v2_ConnectorContribution_Location =
  | 'LOCATION_UNSPECIFIED'
  | 0
  | 'LOCATION_UPSTREAM'
  | 1
  | 'LOCATION_LOCAL_GIT_HOST'
  | 2
  | 'LOCATION_SANDBOX'
  | 3

export type _superverse_v2_ConnectorContribution_Location__Output = typeof _superverse_v2_ConnectorContribution_Location[keyof typeof _superverse_v2_ConnectorContribution_Location]

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_ConnectorContribution_ReleaseCandidateStatus = {
  RELEASE_CANDIDATE_STATUS_UNSPECIFIED: 'RELEASE_CANDIDATE_STATUS_UNSPECIFIED',
  RELEASE_CANDIDATE_STATUS_PUBLISHED: 'RELEASE_CANDIDATE_STATUS_PUBLISHED',
  RELEASE_CANDIDATE_STATUS_FAILED: 'RELEASE_CANDIDATE_STATUS_FAILED',
} as const;

export type _superverse_v2_ConnectorContribution_ReleaseCandidateStatus =
  | 'RELEASE_CANDIDATE_STATUS_UNSPECIFIED'
  | 0
  | 'RELEASE_CANDIDATE_STATUS_PUBLISHED'
  | 1
  | 'RELEASE_CANDIDATE_STATUS_FAILED'
  | 2

export type _superverse_v2_ConnectorContribution_ReleaseCandidateStatus__Output = typeof _superverse_v2_ConnectorContribution_ReleaseCandidateStatus[keyof typeof _superverse_v2_ConnectorContribution_ReleaseCandidateStatus]

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_ConnectorContribution_Status = {
  STATUS_UNSPECIFIED: 'STATUS_UNSPECIFIED',
  STATUS_PULL_REQUEST_PENDING: 'STATUS_PULL_REQUEST_PENDING',
  STATUS_UNPUBLISHED: 'STATUS_UNPUBLISHED',
  STATUS_PULL_REQUEST_FAILED: 'STATUS_PULL_REQUEST_FAILED',
  STATUS_DRAFT: 'STATUS_DRAFT',
  STATUS_CLOSED: 'STATUS_CLOSED',
} as const;

export type _superverse_v2_ConnectorContribution_Status =
  | 'STATUS_UNSPECIFIED'
  | 0
  | 'STATUS_PULL_REQUEST_PENDING'
  | 1
  | 'STATUS_UNPUBLISHED'
  | 2
  | 'STATUS_PULL_REQUEST_FAILED'
  | 3
  | 'STATUS_DRAFT'
  | 4
  | 'STATUS_CLOSED'
  | 5

export type _superverse_v2_ConnectorContribution_Status__Output = typeof _superverse_v2_ConnectorContribution_Status[keyof typeof _superverse_v2_ConnectorContribution_Status]

export interface ConnectorContribution {
  'slug'?: (string);
  'module'?: (string);
  'kind'?: (_superverse_v2_ConnectorContribution_Kind);
  'status'?: (_superverse_v2_ConnectorContribution_Status);
  'branch'?: (string);
  'headCommit'?: (string);
  'location'?: (_superverse_v2_ConnectorContribution_Location);
  'pullRequestNumber'?: (number | string | Long);
  'pullRequestUrl'?: (string);
  'failure'?: (string);
  'description'?: (string);
  'releaseCandidateVersion'?: (string);
  'releaseCandidateStatus'?: (_superverse_v2_ConnectorContribution_ReleaseCandidateStatus);
  'releaseCandidateFailure'?: (string);
}

export interface ConnectorContribution__Output {
  'slug': (string);
  'module': (string);
  'kind': (_superverse_v2_ConnectorContribution_Kind__Output);
  'status': (_superverse_v2_ConnectorContribution_Status__Output);
  'branch': (string);
  'headCommit': (string);
  'location': (_superverse_v2_ConnectorContribution_Location__Output);
  'pullRequestNumber': (string);
  'pullRequestUrl': (string);
  'failure': (string);
  'description': (string);
  'releaseCandidateVersion': (string);
  'releaseCandidateStatus': (_superverse_v2_ConnectorContribution_ReleaseCandidateStatus__Output);
  'releaseCandidateFailure': (string);
}
