// Original file: api/superverse/v2/superverse.proto

import type { FlowApiContext as _superverse_v2_FlowApiContext, FlowApiContext__Output as _superverse_v2_FlowApiContext__Output } from '../../superverse/v2/FlowApiContext';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface SaveOperationConfigurationRequest {
  'context'?: (_superverse_v2_FlowApiContext | null);
  'connectorId'?: (string);
  'connectionName'?: (string);
  'operationId'?: (string);
  'flowType'?: (string);
  'stepType'?: (string);
  'configurationJson'?: (Buffer | Uint8Array | string);
  'followPreview'?: (boolean);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface SaveOperationConfigurationRequest__Output {
  'context': (_superverse_v2_FlowApiContext__Output | null);
  'connectorId': (string);
  'connectionName': (string);
  'operationId': (string);
  'flowType': (string);
  'stepType': (string);
  'configurationJson': (Buffer);
  'followPreview': (boolean);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
