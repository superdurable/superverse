// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';
import type { Long } from '@grpc/proto-loader';

export interface CreateRemoteAgentPairingResponse {
  'connectionId'?: (string);
  'code'?: (string);
  'expiresAt'?: (_google_protobuf_Timestamp | null);
  'endpoint'?: (string);
  'basisEpoch'?: (number | string | Long);
  'currentConnectionId'?: (string);
  'currentOwner'?: (_superverse_v2_Actor | null);
}

export interface CreateRemoteAgentPairingResponse__Output {
  'connectionId': (string);
  'code': (string);
  'expiresAt': (_google_protobuf_Timestamp__Output | null);
  'endpoint': (string);
  'basisEpoch': (string);
  'currentConnectionId': (string);
  'currentOwner': (_superverse_v2_Actor__Output | null);
}
