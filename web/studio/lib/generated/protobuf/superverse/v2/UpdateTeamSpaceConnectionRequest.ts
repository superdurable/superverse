// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface UpdateTeamSpaceConnectionRequest {
  'teamSpaceId'?: (string);
  'connectionId'?: (string);
  'expectedRevision'?: (number | string | Long);
  'settingsJson'?: (string);
  'credentialsJson'?: (string);
  'keepCredentialFields'?: (string)[];
  'expectedCredentialRevision'?: (number | string | Long);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface UpdateTeamSpaceConnectionRequest__Output {
  'teamSpaceId': (string);
  'connectionId': (string);
  'expectedRevision': (string);
  'settingsJson': (string);
  'credentialsJson': (string);
  'keepCredentialFields': (string)[];
  'expectedCredentialRevision': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
