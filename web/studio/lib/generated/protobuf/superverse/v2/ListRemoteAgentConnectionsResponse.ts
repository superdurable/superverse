// Original file: api/superverse/v2/superverse.proto

import type { RemoteAgentConnection as _superverse_v2_RemoteAgentConnection, RemoteAgentConnection__Output as _superverse_v2_RemoteAgentConnection__Output } from '../../superverse/v2/RemoteAgentConnection';
import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';
import type { Long } from '@grpc/proto-loader';

export interface ListRemoteAgentConnectionsResponse {
  'connections'?: (_superverse_v2_RemoteAgentConnection)[];
  'epoch'?: (number | string | Long);
  'currentConnectionId'?: (string);
  'currentOwner'?: (_superverse_v2_Actor | null);
  'endpoint'?: (string);
}

export interface ListRemoteAgentConnectionsResponse__Output {
  'connections': (_superverse_v2_RemoteAgentConnection__Output)[];
  'epoch': (string);
  'currentConnectionId': (string);
  'currentOwner': (_superverse_v2_Actor__Output | null);
  'endpoint': (string);
}
