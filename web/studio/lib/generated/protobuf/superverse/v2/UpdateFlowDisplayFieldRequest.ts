// Original file: api/superverse/v2/superverse.proto

import type { FlowApiContext as _superverse_v2_FlowApiContext, FlowApiContext__Output as _superverse_v2_FlowApiContext__Output } from '../../superverse/v2/FlowApiContext';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface UpdateFlowDisplayFieldRequest {
  'context'?: (_superverse_v2_FlowApiContext | null);
  'flowType'?: (string);
  'flowId'?: (string);
  'attributeKey'?: (string);
  'valueJson'?: (Buffer | Uint8Array | string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface UpdateFlowDisplayFieldRequest__Output {
  'context': (_superverse_v2_FlowApiContext__Output | null);
  'flowType': (string);
  'flowId': (string);
  'attributeKey': (string);
  'valueJson': (Buffer);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
