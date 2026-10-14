// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { StartOutcome as _superverse_v2_StartOutcome, StartOutcome__Output as _superverse_v2_StartOutcome__Output } from '../../superverse/v2/StartOutcome';
import type { PhaseTiming as _superverse_v2_PhaseTiming, PhaseTiming__Output as _superverse_v2_PhaseTiming__Output } from '../../superverse/v2/PhaseTiming';
import type { CommitVersion as _superverse_v2_CommitVersion, CommitVersion__Output as _superverse_v2_CommitVersion__Output } from '../../superverse/v2/CommitVersion';
import type { Long } from '@grpc/proto-loader';

export interface PreviewStartRecord {
  'commandId'?: (string);
  'actorAccountId'?: (string);
  'sourceCommit'?: (string);
  'configurationRevision'?: (number | string | Long);
  'applied'?: (boolean);
  'admittedAt'?: (_google_protobuf_Timestamp | null);
  'endedAt'?: (_google_protobuf_Timestamp | null);
  'outcome'?: (_superverse_v2_StartOutcome);
  'failureCode'?: (string);
  'phaseTimings'?: (_superverse_v2_PhaseTiming)[];
  'version'?: (_superverse_v2_CommitVersion | null);
}

export interface PreviewStartRecord__Output {
  'commandId': (string);
  'actorAccountId': (string);
  'sourceCommit': (string);
  'configurationRevision': (string);
  'applied': (boolean);
  'admittedAt': (_google_protobuf_Timestamp__Output | null);
  'endedAt': (_google_protobuf_Timestamp__Output | null);
  'outcome': (_superverse_v2_StartOutcome__Output);
  'failureCode': (string);
  'phaseTimings': (_superverse_v2_PhaseTiming__Output)[];
  'version': (_superverse_v2_CommitVersion__Output | null);
}
