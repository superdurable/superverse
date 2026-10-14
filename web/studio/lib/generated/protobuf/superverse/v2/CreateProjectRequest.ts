// Original file: api/superverse/v2/superverse.proto

import type { ManagedRepositorySource as _superverse_v2_ManagedRepositorySource, ManagedRepositorySource__Output as _superverse_v2_ManagedRepositorySource__Output } from '../../superverse/v2/ManagedRepositorySource';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface CreateProjectRequest {
  'requestId'?: (string);
  'name'?: (string);
  'managedRepository'?: (_superverse_v2_ManagedRepositorySource | null);
  'teamSpaceId'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface CreateProjectRequest__Output {
  'requestId': (string);
  'name': (string);
  'managedRepository': (_superverse_v2_ManagedRepositorySource__Output | null);
  'teamSpaceId': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
