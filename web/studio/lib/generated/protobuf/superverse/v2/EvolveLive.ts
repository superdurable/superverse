// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Long } from '@grpc/proto-loader';

export interface EvolveLive {
  'revision'?: (number | string | Long);
  'releaseId'?: (string);
  'buildCommit'?: (string);
  'committedAt'?: (_google_protobuf_Timestamp | null);
}

export interface EvolveLive__Output {
  'revision': (string);
  'releaseId': (string);
  'buildCommit': (string);
  'committedAt': (_google_protobuf_Timestamp__Output | null);
}
