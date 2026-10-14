// Original file: api/superverse/v2/superverse.proto

import type { FlowApiContext as _superverse_v2_FlowApiContext, FlowApiContext__Output as _superverse_v2_FlowApiContext__Output } from '../../superverse/v2/FlowApiContext';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface GetFlowHistoryRequest {
  'context'?: (_superverse_v2_FlowApiContext | null);
  'flowId'?: (string);
  'runId'?: (string);
  'pageToken'?: (string);
  'pageSize'?: (number);
  'afterEventId'?: (number | string | Long);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface GetFlowHistoryRequest__Output {
  'context': (_superverse_v2_FlowApiContext__Output | null);
  'flowId': (string);
  'runId': (string);
  'pageToken': (string);
  'pageSize': (number);
  'afterEventId': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
