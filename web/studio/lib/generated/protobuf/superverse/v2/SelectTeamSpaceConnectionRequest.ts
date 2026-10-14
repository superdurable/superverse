// Original file: api/superverse/v2/superverse.proto

import type { FlowApiContext as _superverse_v2_FlowApiContext, FlowApiContext__Output as _superverse_v2_FlowApiContext__Output } from '../../superverse/v2/FlowApiContext';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface SelectTeamSpaceConnectionRequest {
  'context'?: (_superverse_v2_FlowApiContext | null);
  'connectorId'?: (string);
  'connectionName'?: (string);
  'teamSpaceConnectionId'?: (string);
  'followPreview'?: (boolean);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface SelectTeamSpaceConnectionRequest__Output {
  'context': (_superverse_v2_FlowApiContext__Output | null);
  'connectorId': (string);
  'connectionName': (string);
  'teamSpaceConnectionId': (string);
  'followPreview': (boolean);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
