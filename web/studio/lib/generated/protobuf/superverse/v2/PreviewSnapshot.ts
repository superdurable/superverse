// Original file: api/superverse/v2/superverse.proto

import type { PreviewStatus as _superverse_v2_PreviewStatus, PreviewStatus__Output as _superverse_v2_PreviewStatus__Output } from '../../superverse/v2/PreviewStatus';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { PreparedPreviewSourceSnapshot as _superverse_v2_PreparedPreviewSourceSnapshot, PreparedPreviewSourceSnapshot__Output as _superverse_v2_PreparedPreviewSourceSnapshot__Output } from '../../superverse/v2/PreparedPreviewSourceSnapshot';
import type { PreviewDeploymentSnapshot as _superverse_v2_PreviewDeploymentSnapshot, PreviewDeploymentSnapshot__Output as _superverse_v2_PreviewDeploymentSnapshot__Output } from '../../superverse/v2/PreviewDeploymentSnapshot';
import type { ConfigurationRequirementGap as _superverse_v2_ConfigurationRequirementGap, ConfigurationRequirementGap__Output as _superverse_v2_ConfigurationRequirementGap__Output } from '../../superverse/v2/ConfigurationRequirementGap';
import type { PhaseTiming as _superverse_v2_PhaseTiming, PhaseTiming__Output as _superverse_v2_PhaseTiming__Output } from '../../superverse/v2/PhaseTiming';
import type { AppliedPreviewConfiguration as _superverse_v2_AppliedPreviewConfiguration, AppliedPreviewConfiguration__Output as _superverse_v2_AppliedPreviewConfiguration__Output } from '../../superverse/v2/AppliedPreviewConfiguration';
import type { PreviewStartRecord as _superverse_v2_PreviewStartRecord, PreviewStartRecord__Output as _superverse_v2_PreviewStartRecord__Output } from '../../superverse/v2/PreviewStartRecord';
import type { CommitVersion as _superverse_v2_CommitVersion, CommitVersion__Output as _superverse_v2_CommitVersion__Output } from '../../superverse/v2/CommitVersion';
import type { Long } from '@grpc/proto-loader';

export interface PreviewSnapshot {
  'projectId'?: (string);
  'status'?: (_superverse_v2_PreviewStatus);
  'revision'?: (number | string | Long);
  'generation'?: (number | string | Long);
  'url'?: (string);
  'failure'?: (_superverse_v2_Failure | null);
  'preparedSource'?: (_superverse_v2_PreparedPreviewSourceSnapshot | null);
  'workspaceRequestId'?: (string);
  'sourceCommit'?: (string);
  'definitionRevision'?: (string);
  'servingRevision'?: (number | string | Long);
  'automaticDeployment'?: (_superverse_v2_PreviewDeploymentSnapshot | null);
  'configurationGaps'?: (_superverse_v2_ConfigurationRequirementGap)[];
  'phaseTimings'?: (_superverse_v2_PhaseTiming)[];
  'appliedConfiguration'?: (_superverse_v2_AppliedPreviewConfiguration | null);
  'recentStarts'?: (_superverse_v2_PreviewStartRecord)[];
  'servingVersion'?: (_superverse_v2_CommitVersion | null);
}

export interface PreviewSnapshot__Output {
  'projectId': (string);
  'status': (_superverse_v2_PreviewStatus__Output);
  'revision': (string);
  'generation': (string);
  'url': (string);
  'failure': (_superverse_v2_Failure__Output | null);
  'preparedSource': (_superverse_v2_PreparedPreviewSourceSnapshot__Output | null);
  'workspaceRequestId': (string);
  'sourceCommit': (string);
  'definitionRevision': (string);
  'servingRevision': (string);
  'automaticDeployment': (_superverse_v2_PreviewDeploymentSnapshot__Output | null);
  'configurationGaps': (_superverse_v2_ConfigurationRequirementGap__Output)[];
  'phaseTimings': (_superverse_v2_PhaseTiming__Output)[];
  'appliedConfiguration': (_superverse_v2_AppliedPreviewConfiguration__Output | null);
  'recentStarts': (_superverse_v2_PreviewStartRecord__Output)[];
  'servingVersion': (_superverse_v2_CommitVersion__Output | null);
}
