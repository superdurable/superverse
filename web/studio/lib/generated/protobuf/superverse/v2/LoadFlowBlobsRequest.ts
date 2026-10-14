// Original file: api/superverse/v2/superverse.proto

import type { FlowApiContext as _superverse_v2_FlowApiContext, FlowApiContext__Output as _superverse_v2_FlowApiContext__Output } from '../../superverse/v2/FlowApiContext';
import type { FlowBlobReference as _superverse_v2_FlowBlobReference, FlowBlobReference__Output as _superverse_v2_FlowBlobReference__Output } from '../../superverse/v2/FlowBlobReference';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface LoadFlowBlobsRequest {
  'context'?: (_superverse_v2_FlowApiContext | null);
  'flowId'?: (string);
  'values'?: (_superverse_v2_FlowBlobReference)[];
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface LoadFlowBlobsRequest__Output {
  'context': (_superverse_v2_FlowApiContext__Output | null);
  'flowId': (string);
  'values': (_superverse_v2_FlowBlobReference__Output)[];
  'caller': (_superverse_v2_CallerContext__Output | null);
}
