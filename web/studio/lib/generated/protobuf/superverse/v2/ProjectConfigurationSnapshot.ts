// Original file: api/superverse/v2/superverse.proto

import type { ProjectScope as _superverse_v2_ProjectScope, ProjectScope__Output as _superverse_v2_ProjectScope__Output } from '../../superverse/v2/ProjectScope';
import type { ConfigurationStatus as _superverse_v2_ConfigurationStatus, ConfigurationStatus__Output as _superverse_v2_ConfigurationStatus__Output } from '../../superverse/v2/ConfigurationStatus';
import type { AppManifestSnapshot as _superverse_v2_AppManifestSnapshot, AppManifestSnapshot__Output as _superverse_v2_AppManifestSnapshot__Output } from '../../superverse/v2/AppManifestSnapshot';
import type { ProjectConfigurationValidation as _superverse_v2_ProjectConfigurationValidation, ProjectConfigurationValidation__Output as _superverse_v2_ProjectConfigurationValidation__Output } from '../../superverse/v2/ProjectConfigurationValidation';
import type { ConfigurationRequirementGap as _superverse_v2_ConfigurationRequirementGap, ConfigurationRequirementGap__Output as _superverse_v2_ConfigurationRequirementGap__Output } from '../../superverse/v2/ConfigurationRequirementGap';
import type { ConfigurationItem as _superverse_v2_ConfigurationItem, ConfigurationItem__Output as _superverse_v2_ConfigurationItem__Output } from '../../superverse/v2/ConfigurationItem';
import type { Long } from '@grpc/proto-loader';

export interface ProjectConfigurationSnapshot {
  'projectId'?: (string);
  'scope'?: (_superverse_v2_ProjectScope);
  'status'?: (_superverse_v2_ConfigurationStatus);
  'revision'?: (number | string | Long);
  'releaseId'?: (string);
  'sourceCommit'?: (string);
  'validationErrors'?: (string)[];
  'appManifest'?: (_superverse_v2_AppManifestSnapshot | null);
  'validation'?: (_superverse_v2_ProjectConfigurationValidation | null);
  'appManifestRevision'?: (number | string | Long);
  'manifestDigest'?: (string);
  'connectorContractDigest'?: (string);
  'environmentContractDigest'?: (string);
  'requirementGaps'?: (_superverse_v2_ConfigurationRequirementGap)[];
  'items'?: (_superverse_v2_ConfigurationItem)[];
  'unappliedChangeCount'?: (number);
  'isStale'?: (boolean);
  'nextPublishRevision'?: (number | string | Long);
  'previewConfigurationRevision'?: (number | string | Long);
}

export interface ProjectConfigurationSnapshot__Output {
  'projectId': (string);
  'scope': (_superverse_v2_ProjectScope__Output);
  'status': (_superverse_v2_ConfigurationStatus__Output);
  'revision': (string);
  'releaseId': (string);
  'sourceCommit': (string);
  'validationErrors': (string)[];
  'appManifest': (_superverse_v2_AppManifestSnapshot__Output | null);
  'validation': (_superverse_v2_ProjectConfigurationValidation__Output | null);
  'appManifestRevision': (string);
  'manifestDigest': (string);
  'connectorContractDigest': (string);
  'environmentContractDigest': (string);
  'requirementGaps': (_superverse_v2_ConfigurationRequirementGap__Output)[];
  'items': (_superverse_v2_ConfigurationItem__Output)[];
  'unappliedChangeCount': (number);
  'isStale': (boolean);
  'nextPublishRevision': (string);
  'previewConfigurationRevision': (string);
}
