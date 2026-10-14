// Original file: api/superverse/v2/superverse.proto

import type { AccountSnapshot as _superverse_v2_AccountSnapshot, AccountSnapshot__Output as _superverse_v2_AccountSnapshot__Output } from '../../superverse/v2/AccountSnapshot';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

export interface AccountLogin {
  'account'?: (_superverse_v2_AccountSnapshot | null);
  'sessionToken'?: (string);
  'expiresAt'?: (_google_protobuf_Timestamp | null);
}

export interface AccountLogin__Output {
  'account': (_superverse_v2_AccountSnapshot__Output | null);
  'sessionToken': (string);
  'expiresAt': (_google_protobuf_Timestamp__Output | null);
}
