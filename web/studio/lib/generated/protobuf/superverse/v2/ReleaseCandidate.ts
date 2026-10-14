// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { CommitVersion as _superverse_v2_CommitVersion, CommitVersion__Output as _superverse_v2_CommitVersion__Output } from '../../superverse/v2/CommitVersion';

export interface ReleaseCandidate {
  'sourceCommit'?: (string);
  'subject'?: (string);
  'committedAt'?: (_google_protobuf_Timestamp | null);
  'version'?: (_superverse_v2_CommitVersion | null);
}

export interface ReleaseCandidate__Output {
  'sourceCommit': (string);
  'subject': (string);
  'committedAt': (_google_protobuf_Timestamp__Output | null);
  'version': (_superverse_v2_CommitVersion__Output | null);
}
