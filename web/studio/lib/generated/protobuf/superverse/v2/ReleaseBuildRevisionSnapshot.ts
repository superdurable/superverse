// Original file: api/superverse/v2/superverse.proto

import type { ReleaseStatus as _superverse_v2_ReleaseStatus, ReleaseStatus__Output as _superverse_v2_ReleaseStatus__Output } from '../../superverse/v2/ReleaseStatus';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { AutoDeployStatus as _superverse_v2_AutoDeployStatus, AutoDeployStatus__Output as _superverse_v2_AutoDeployStatus__Output } from '../../superverse/v2/AutoDeployStatus';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { PhaseTiming as _superverse_v2_PhaseTiming, PhaseTiming__Output as _superverse_v2_PhaseTiming__Output } from '../../superverse/v2/PhaseTiming';
import type { PreviewValidation as _superverse_v2_PreviewValidation, PreviewValidation__Output as _superverse_v2_PreviewValidation__Output } from '../../superverse/v2/PreviewValidation';
import type { Cancellation as _superverse_v2_Cancellation, Cancellation__Output as _superverse_v2_Cancellation__Output } from '../../superverse/v2/Cancellation';
import type { Long } from '@grpc/proto-loader';

export interface ReleaseBuildRevisionSnapshot {
  'buildRevisionId'?: (string);
  'status'?: (_superverse_v2_ReleaseStatus);
  'revision'?: (number | string | Long);
  'configurationRevision'?: (number | string | Long);
  'imageDigest'?: (string);
  'buildProfileVersion'?: (string);
  'failure'?: (_superverse_v2_Failure | null);
  'autoDeploy'?: (boolean);
  'autoDeployStatus'?: (_superverse_v2_AutoDeployStatus);
  'autoDeploymentId'?: (string);
  'autoDeployFailure'?: (_superverse_v2_Failure | null);
  'createdAt'?: (_google_protobuf_Timestamp | null);
  'draftConfigurationRevision'?: (number | string | Long);
  'providerSettled'?: (boolean);
  'phaseTimings'?: (_superverse_v2_PhaseTiming)[];
  'previewValidation'?: (_superverse_v2_PreviewValidation | null);
  'cancellation'?: (_superverse_v2_Cancellation | null);
}

export interface ReleaseBuildRevisionSnapshot__Output {
  'buildRevisionId': (string);
  'status': (_superverse_v2_ReleaseStatus__Output);
  'revision': (string);
  'configurationRevision': (string);
  'imageDigest': (string);
  'buildProfileVersion': (string);
  'failure': (_superverse_v2_Failure__Output | null);
  'autoDeploy': (boolean);
  'autoDeployStatus': (_superverse_v2_AutoDeployStatus__Output);
  'autoDeploymentId': (string);
  'autoDeployFailure': (_superverse_v2_Failure__Output | null);
  'createdAt': (_google_protobuf_Timestamp__Output | null);
  'draftConfigurationRevision': (string);
  'providerSettled': (boolean);
  'phaseTimings': (_superverse_v2_PhaseTiming__Output)[];
  'previewValidation': (_superverse_v2_PreviewValidation__Output | null);
  'cancellation': (_superverse_v2_Cancellation__Output | null);
}
