// Original file: api/superverse/v2/superverse.proto

import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';
import type { RemoteAgentConnectionStatus as _superverse_v2_RemoteAgentConnectionStatus, RemoteAgentConnectionStatus__Output as _superverse_v2_RemoteAgentConnectionStatus__Output } from '../../superverse/v2/RemoteAgentConnectionStatus';
import type { RemoteAgentBinding as _superverse_v2_RemoteAgentBinding, RemoteAgentBinding__Output as _superverse_v2_RemoteAgentBinding__Output } from '../../superverse/v2/RemoteAgentBinding';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface RemoteAgentConnection {
  'connectionId'?: (string);
  'owner'?: (_superverse_v2_Actor | null);
  'status'?: (_superverse_v2_RemoteAgentConnectionStatus);
  'binding'?: (_superverse_v2_RemoteAgentBinding | null);
  'createdAt'?: (_google_protobuf_Timestamp | null);
  'lastSeenAt'?: (_google_protobuf_Timestamp | null);
}

export interface RemoteAgentConnection__Output {
  'connectionId': (string);
  'owner': (_superverse_v2_Actor__Output | null);
  'status': (_superverse_v2_RemoteAgentConnectionStatus__Output);
  'binding': (_superverse_v2_RemoteAgentBinding__Output | null);
  'createdAt': (_google_protobuf_Timestamp__Output | null);
  'lastSeenAt': (_google_protobuf_Timestamp__Output | null);
}
