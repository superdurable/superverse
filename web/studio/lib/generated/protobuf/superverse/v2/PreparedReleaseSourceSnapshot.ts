// Original file: api/superverse/v2/superverse.proto

import type { ReleaseStatus as _superverse_v2_ReleaseStatus, ReleaseStatus__Output as _superverse_v2_ReleaseStatus__Output } from '../../superverse/v2/ReleaseStatus';
import type { AppManifestSnapshot as _superverse_v2_AppManifestSnapshot, AppManifestSnapshot__Output as _superverse_v2_AppManifestSnapshot__Output } from '../../superverse/v2/AppManifestSnapshot';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { PhaseTiming as _superverse_v2_PhaseTiming, PhaseTiming__Output as _superverse_v2_PhaseTiming__Output } from '../../superverse/v2/PhaseTiming';
import type { CommitVersion as _superverse_v2_CommitVersion, CommitVersion__Output as _superverse_v2_CommitVersion__Output } from '../../superverse/v2/CommitVersion';

export interface PreparedReleaseSourceSnapshot {
  'status'?: (_superverse_v2_ReleaseStatus);
  'sourceCommit'?: (string);
  'definitionRevision'?: (string);
  'appManifest'?: (_superverse_v2_AppManifestSnapshot | null);
  'failure'?: (_superverse_v2_Failure | null);
  'phaseTimings'?: (_superverse_v2_PhaseTiming)[];
  'version'?: (_superverse_v2_CommitVersion | null);
}

export interface PreparedReleaseSourceSnapshot__Output {
  'status': (_superverse_v2_ReleaseStatus__Output);
  'sourceCommit': (string);
  'definitionRevision': (string);
  'appManifest': (_superverse_v2_AppManifestSnapshot__Output | null);
  'failure': (_superverse_v2_Failure__Output | null);
  'phaseTimings': (_superverse_v2_PhaseTiming__Output)[];
  'version': (_superverse_v2_CommitVersion__Output | null);
}
