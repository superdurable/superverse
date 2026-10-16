// Original file: api/superverse/v2/superverse.proto

import type { ManagedRepositorySource as _superverse_v2_ManagedRepositorySource, ManagedRepositorySource__Output as _superverse_v2_ManagedRepositorySource__Output } from '../../superverse/v2/ManagedRepositorySource';
import type { AgentModel as _superverse_v2_AgentModel, AgentModel__Output as _superverse_v2_AgentModel__Output } from '../../superverse/v2/AgentModel';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface CreateProjectRequest {
  'requestId'?: (string);
  'name'?: (string);
  'managedRepository'?: (_superverse_v2_ManagedRepositorySource | null);
  'teamSpaceId'?: (string);
  'initialMessage'?: (string);
  'model'?: (_superverse_v2_AgentModel);
  'messageId'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface CreateProjectRequest__Output {
  'requestId': (string);
  'name': (string);
  'managedRepository': (_superverse_v2_ManagedRepositorySource__Output | null);
  'teamSpaceId': (string);
  'initialMessage': (string);
  'model': (_superverse_v2_AgentModel__Output);
  'messageId': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
