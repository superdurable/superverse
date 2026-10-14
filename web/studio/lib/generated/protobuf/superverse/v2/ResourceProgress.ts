// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Long } from '@grpc/proto-loader';

export interface ResourceProgress {
  'phase'?: (string);
  'revision'?: (number | string | Long);
  'createdAt'?: (_google_protobuf_Timestamp | null);
}

export interface ResourceProgress__Output {
  'phase': (string);
  'revision': (string);
  'createdAt': (_google_protobuf_Timestamp__Output | null);
}
