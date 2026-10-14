// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { StartOutcome as _superverse_v2_StartOutcome, StartOutcome__Output as _superverse_v2_StartOutcome__Output } from '../../superverse/v2/StartOutcome';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { PhaseTiming as _superverse_v2_PhaseTiming, PhaseTiming__Output as _superverse_v2_PhaseTiming__Output } from '../../superverse/v2/PhaseTiming';
import type { CommitVersion as _superverse_v2_CommitVersion, CommitVersion__Output as _superverse_v2_CommitVersion__Output } from '../../superverse/v2/CommitVersion';
import type { Long } from '@grpc/proto-loader';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_EnvironmentEvent_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_PREVIEW_CONFIGURATION_APPLIED: 'KIND_PREVIEW_CONFIGURATION_APPLIED',
  KIND_PREVIEW_STARTED: 'KIND_PREVIEW_STARTED',
  KIND_PREVIEW_START_RESULT: 'KIND_PREVIEW_START_RESULT',
  KIND_PUBLISHED: 'KIND_PUBLISHED',
  KIND_DEPLOYED_EXISTING_VERSION: 'KIND_DEPLOYED_EXISTING_VERSION',
  KIND_PUBLISH_CANCELLED: 'KIND_PUBLISH_CANCELLED',
} as const;

export type _superverse_v2_EnvironmentEvent_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_PREVIEW_CONFIGURATION_APPLIED'
  | 1
  | 'KIND_PREVIEW_STARTED'
  | 2
  | 'KIND_PREVIEW_START_RESULT'
  | 3
  | 'KIND_PUBLISHED'
  | 4
  | 'KIND_DEPLOYED_EXISTING_VERSION'
  | 5
  | 'KIND_PUBLISH_CANCELLED'
  | 6

export type _superverse_v2_EnvironmentEvent_Kind__Output = typeof _superverse_v2_EnvironmentEvent_Kind[keyof typeof _superverse_v2_EnvironmentEvent_Kind]

export interface EnvironmentEvent {
  'kind'?: (_superverse_v2_EnvironmentEvent_Kind);
  'occurredAt'?: (_google_protobuf_Timestamp | null);
  'actorAccountId'?: (string);
  'actorDisplay'?: (string);
  'commit'?: (string);
  'outcome'?: (_superverse_v2_StartOutcome);
  'failure'?: (_superverse_v2_Failure | null);
  'phaseTimings'?: (_superverse_v2_PhaseTiming)[];
  'releaseId'?: (string);
  'deploymentId'?: (string);
  'buildRevisionId'?: (string);
  'configurationRevision'?: (number | string | Long);
  'previewSkipReason'?: (string);
  'version'?: (_superverse_v2_CommitVersion | null);
  'rolledBack'?: (boolean);
}

export interface EnvironmentEvent__Output {
  'kind': (_superverse_v2_EnvironmentEvent_Kind__Output);
  'occurredAt': (_google_protobuf_Timestamp__Output | null);
  'actorAccountId': (string);
  'actorDisplay': (string);
  'commit': (string);
  'outcome': (_superverse_v2_StartOutcome__Output);
  'failure': (_superverse_v2_Failure__Output | null);
  'phaseTimings': (_superverse_v2_PhaseTiming__Output)[];
  'releaseId': (string);
  'deploymentId': (string);
  'buildRevisionId': (string);
  'configurationRevision': (string);
  'previewSkipReason': (string);
  'version': (_superverse_v2_CommitVersion__Output | null);
  'rolledBack': (boolean);
}
