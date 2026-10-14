// Original file: api/superverse/v2/superverse.proto

import type { FlowApiContext as _superverse_v2_FlowApiContext, FlowApiContext__Output as _superverse_v2_FlowApiContext__Output } from '../../superverse/v2/FlowApiContext';
import type { FlowSearchFilter as _superverse_v2_FlowSearchFilter, FlowSearchFilter__Output as _superverse_v2_FlowSearchFilter__Output } from '../../superverse/v2/FlowSearchFilter';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface SearchFlowsRequest {
  'context'?: (_superverse_v2_FlowApiContext | null);
  'flowType'?: (string);
  'filters'?: (_superverse_v2_FlowSearchFilter)[];
  'pageSize'?: (number);
  'pageToken'?: (string);
  'workQueueOnly'?: (boolean);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface SearchFlowsRequest__Output {
  'context': (_superverse_v2_FlowApiContext__Output | null);
  'flowType': (string);
  'filters': (_superverse_v2_FlowSearchFilter__Output)[];
  'pageSize': (number);
  'pageToken': (string);
  'workQueueOnly': (boolean);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
