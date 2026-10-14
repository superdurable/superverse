// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Long } from '@grpc/proto-loader';

export interface PhaseTiming {
  'phase'?: (string);
  'startedAt'?: (_google_protobuf_Timestamp | null);
  'durationMs'?: (number | string | Long);
}

export interface PhaseTiming__Output {
  'phase': (string);
  'startedAt': (_google_protobuf_Timestamp__Output | null);
  'durationMs': (string);
}
