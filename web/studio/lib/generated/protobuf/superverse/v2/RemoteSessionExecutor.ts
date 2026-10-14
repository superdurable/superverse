// Original file: api/superverse/v2/superverse.proto

import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';
import type { RemoteAgentBinding as _superverse_v2_RemoteAgentBinding, RemoteAgentBinding__Output as _superverse_v2_RemoteAgentBinding__Output } from '../../superverse/v2/RemoteAgentBinding';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Long } from '@grpc/proto-loader';

export interface RemoteSessionExecutor {
  'connectionId'?: (string);
  'owner'?: (_superverse_v2_Actor | null);
  'binding'?: (_superverse_v2_RemoteAgentBinding | null);
  'reported'?: (number | string | Long);
  'activatedAt'?: (_google_protobuf_Timestamp | null);
  'lastSeenAt'?: (_google_protobuf_Timestamp | null);
}

export interface RemoteSessionExecutor__Output {
  'connectionId': (string);
  'owner': (_superverse_v2_Actor__Output | null);
  'binding': (_superverse_v2_RemoteAgentBinding__Output | null);
  'reported': (string);
  'activatedAt': (_google_protobuf_Timestamp__Output | null);
  'lastSeenAt': (_google_protobuf_Timestamp__Output | null);
}
