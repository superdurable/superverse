// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface StartTeamSpaceConnectionOAuthResponse {
  'authorizationUrl'?: (string);
  'expiresAt'?: (_google_protobuf_Timestamp | null);
}

export interface StartTeamSpaceConnectionOAuthResponse__Output {
  'authorizationUrl': (string);
  'expiresAt': (_google_protobuf_Timestamp__Output | null);
}
