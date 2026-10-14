// Original file: api/superverse/v2/superverse.proto

import type { FlowApiContext as _superverse_v2_FlowApiContext, FlowApiContext__Output as _superverse_v2_FlowApiContext__Output } from '../../superverse/v2/FlowApiContext';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface TimeTravelFlowRequest {
  'context'?: (_superverse_v2_FlowApiContext | null);
  'flowId'?: (string);
  'runId'?: (string);
  'timeTravelType'?: (number);
  'reason'?: (string);
  'stepType'?: (string);
  'stepExecutionId'?: (string);
  'historyEventTime'?: (string);
  'stepMethod'?: (number);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface TimeTravelFlowRequest__Output {
  'context': (_superverse_v2_FlowApiContext__Output | null);
  'flowId': (string);
  'runId': (string);
  'timeTravelType': (number);
  'reason': (string);
  'stepType': (string);
  'stepExecutionId': (string);
  'historyEventTime': (string);
  'stepMethod': (number);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
