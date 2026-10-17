// Original file: api/superverse/v2/superverse.proto

import type { ProjectStatus as _superverse_v2_ProjectStatus, ProjectStatus__Output as _superverse_v2_ProjectStatus__Output } from '../../superverse/v2/ProjectStatus';
import type { Repository as _superverse_v2_Repository, Repository__Output as _superverse_v2_Repository__Output } from '../../superverse/v2/Repository';
import type { SetupSnapshot as _superverse_v2_SetupSnapshot, SetupSnapshot__Output as _superverse_v2_SetupSnapshot__Output } from '../../superverse/v2/SetupSnapshot';
import type { AppManifestSnapshot as _superverse_v2_AppManifestSnapshot, AppManifestSnapshot__Output as _superverse_v2_AppManifestSnapshot__Output } from '../../superverse/v2/AppManifestSnapshot';
import type { ProjectLive as _superverse_v2_ProjectLive, ProjectLive__Output as _superverse_v2_ProjectLive__Output } from '../../superverse/v2/ProjectLive';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';
import type { ActiveDeploymentReference as _superverse_v2_ActiveDeploymentReference, ActiveDeploymentReference__Output as _superverse_v2_ActiveDeploymentReference__Output } from '../../superverse/v2/ActiveDeploymentReference';
import type { ProjectRole as _superverse_v2_ProjectRole, ProjectRole__Output as _superverse_v2_ProjectRole__Output } from '../../superverse/v2/ProjectRole';
import type { ProjectExample as _superverse_v2_ProjectExample, ProjectExample__Output as _superverse_v2_ProjectExample__Output } from '../../superverse/v2/ProjectExample';
import type { Long } from '@grpc/proto-loader';

export interface ProjectSnapshot {
  'projectId'?: (string);
  'creatorAccountId'?: (string);
  'name'?: (string);
  'status'?: (_superverse_v2_ProjectStatus);
  'revision'?: (number | string | Long);
  'repository'?: (_superverse_v2_Repository | null);
  'setup'?: (_superverse_v2_SetupSnapshot | null);
  'appManifest'?: (_superverse_v2_AppManifestSnapshot | null);
  'live'?: (_superverse_v2_ProjectLive | null);
  'failure'?: (_superverse_v2_Failure | null);
  'createdAt'?: (_google_protobuf_Timestamp | null);
  'updatedAt'?: (_google_protobuf_Timestamp | null);
  'templateRevision'?: (string);
  'activeDeployment'?: (_superverse_v2_ActiveDeploymentReference | null);
  'sessionStartFailure'?: (_superverse_v2_Failure | null);
  'teamSpaceId'?: (string);
  'callerRole'?: (_superverse_v2_ProjectRole);
  'example'?: (_superverse_v2_ProjectExample | null);
}

export interface ProjectSnapshot__Output {
  'projectId': (string);
  'creatorAccountId': (string);
  'name': (string);
  'status': (_superverse_v2_ProjectStatus__Output);
  'revision': (string);
  'repository': (_superverse_v2_Repository__Output | null);
  'setup': (_superverse_v2_SetupSnapshot__Output | null);
  'appManifest': (_superverse_v2_AppManifestSnapshot__Output | null);
  'live': (_superverse_v2_ProjectLive__Output | null);
  'failure': (_superverse_v2_Failure__Output | null);
  'createdAt': (_google_protobuf_Timestamp__Output | null);
  'updatedAt': (_google_protobuf_Timestamp__Output | null);
  'templateRevision': (string);
  'activeDeployment': (_superverse_v2_ActiveDeploymentReference__Output | null);
  'sessionStartFailure': (_superverse_v2_Failure__Output | null);
  'teamSpaceId': (string);
  'callerRole': (_superverse_v2_ProjectRole__Output);
  'example': (_superverse_v2_ProjectExample__Output | null);
}
