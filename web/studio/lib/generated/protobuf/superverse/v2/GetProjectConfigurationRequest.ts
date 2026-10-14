// Original file: api/superverse/v2/superverse.proto

import type { ProjectConfigurationTarget as _superverse_v2_ProjectConfigurationTarget, ProjectConfigurationTarget__Output as _superverse_v2_ProjectConfigurationTarget__Output } from '../../superverse/v2/ProjectConfigurationTarget';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface GetProjectConfigurationRequest {
  'target'?: (_superverse_v2_ProjectConfigurationTarget | null);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface GetProjectConfigurationRequest__Output {
  'target': (_superverse_v2_ProjectConfigurationTarget__Output | null);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
