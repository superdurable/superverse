// Original file: api/superverse/v2/superverse.proto

import type { AgentModel as _superverse_v2_AgentModel, AgentModel__Output as _superverse_v2_AgentModel__Output } from '../../superverse/v2/AgentModel';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface UpdateSessionModelRequest {
  'projectId'?: (string);
  'model'?: (_superverse_v2_AgentModel);
  'expectedRevision'?: (number | string | Long);
  'caller'?: (_superverse_v2_CallerContext | null);
  '_expectedRevision'?: "expectedRevision";
}

export interface UpdateSessionModelRequest__Output {
  'projectId': (string);
  'model': (_superverse_v2_AgentModel__Output);
  'expectedRevision'?: (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
  '_expectedRevision'?: "expectedRevision";
}
