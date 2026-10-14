// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Long } from '@grpc/proto-loader';

export interface UnroutedEvents {
  'count'?: (number | string | Long);
  'lastAt'?: (_google_protobuf_Timestamp | null);
  'lastEventId'?: (string);
}

export interface UnroutedEvents__Output {
  'count': (string);
  'lastAt': (_google_protobuf_Timestamp__Output | null);
  'lastEventId': (string);
}
