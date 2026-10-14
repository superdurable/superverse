// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Long } from '@grpc/proto-loader';

export interface AppliedPreviewConfiguration {
  'configurationRevision'?: (number | string | Long);
  'sourceCommit'?: (string);
  'appliedAt'?: (_google_protobuf_Timestamp | null);
  'appliedByAccountId'?: (string);
}

export interface AppliedPreviewConfiguration__Output {
  'configurationRevision': (string);
  'sourceCommit': (string);
  'appliedAt': (_google_protobuf_Timestamp__Output | null);
  'appliedByAccountId': (string);
}
