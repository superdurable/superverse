// Original file: api/superverse/v2/superverse.proto

import type { FlowApiContext as _superverse_v2_FlowApiContext, FlowApiContext__Output as _superverse_v2_FlowApiContext__Output } from '../../superverse/v2/FlowApiContext';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface InvokeFlowActionRequest {
  'context'?: (_superverse_v2_FlowApiContext | null);
  'flowType'?: (string);
  'flowId'?: (string);
  'rpcName'?: (string);
  'inputJson'?: (Buffer | Uint8Array | string);
  'attributeSnapshotJson'?: (Buffer | Uint8Array | string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface InvokeFlowActionRequest__Output {
  'context': (_superverse_v2_FlowApiContext__Output | null);
  'flowType': (string);
  'flowId': (string);
  'rpcName': (string);
  'inputJson': (Buffer);
  'attributeSnapshotJson': (Buffer);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
