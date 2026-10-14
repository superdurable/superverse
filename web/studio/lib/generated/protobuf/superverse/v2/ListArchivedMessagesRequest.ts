// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface ListArchivedMessagesRequest {
  'projectId'?: (string);
  'beforeSequence'?: (number | string | Long);
  'pageSize'?: (number);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface ListArchivedMessagesRequest__Output {
  'projectId': (string);
  'beforeSequence': (string);
  'pageSize': (number);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
