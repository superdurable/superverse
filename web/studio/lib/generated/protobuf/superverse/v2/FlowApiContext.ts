// Original file: api/superverse/v2/superverse.proto

import type { FlowApiTarget as _superverse_v2_FlowApiTarget, FlowApiTarget__Output as _superverse_v2_FlowApiTarget__Output } from '../../superverse/v2/FlowApiTarget';
import type { Long } from '@grpc/proto-loader';

export interface FlowApiContext {
  'target'?: (_superverse_v2_FlowApiTarget | null);
  'definitionRevision'?: (string);
  'appManifestRevision'?: (number | string | Long);
  'configurationRevision'?: (number | string | Long);
  'credentialRevision'?: (number | string | Long);
}

export interface FlowApiContext__Output {
  'target': (_superverse_v2_FlowApiTarget__Output | null);
  'definitionRevision': (string);
  'appManifestRevision': (string);
  'configurationRevision': (string);
  'credentialRevision': (string);
}
