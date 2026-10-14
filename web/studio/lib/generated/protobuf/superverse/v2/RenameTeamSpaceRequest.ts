// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface RenameTeamSpaceRequest {
  'teamSpaceId'?: (string);
  'expectedRevision'?: (number | string | Long);
  'name'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface RenameTeamSpaceRequest__Output {
  'teamSpaceId': (string);
  'expectedRevision': (string);
  'name': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
