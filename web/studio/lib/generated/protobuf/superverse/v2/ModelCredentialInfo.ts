// Original file: api/superverse/v2/superverse.proto

import type { ModelCredentialStatus as _superverse_v2_ModelCredentialStatus, ModelCredentialStatus__Output as _superverse_v2_ModelCredentialStatus__Output } from '../../superverse/v2/ModelCredentialStatus';
import type { Actor as _superverse_v2_Actor, Actor__Output as _superverse_v2_Actor__Output } from '../../superverse/v2/Actor';
import type { ModelProvider as _superverse_v2_ModelProvider, ModelProvider__Output as _superverse_v2_ModelProvider__Output } from '../../superverse/v2/ModelProvider';
import type { Long } from '@grpc/proto-loader';

export interface ModelCredentialInfo {
  'revision'?: (number | string | Long);
  'status'?: (_superverse_v2_ModelCredentialStatus);
  'last4'?: (string);
  'setBy'?: (_superverse_v2_Actor | null);
  'setAt'?: (string);
  'pendingOperation'?: (string);
  'pendingRevision'?: (number | string | Long);
  'pendingDelete'?: (boolean);
  'provider'?: (_superverse_v2_ModelProvider);
}

export interface ModelCredentialInfo__Output {
  'revision': (string);
  'status': (_superverse_v2_ModelCredentialStatus__Output);
  'last4': (string);
  'setBy': (_superverse_v2_Actor__Output | null);
  'setAt': (string);
  'pendingOperation': (string);
  'pendingRevision': (string);
  'pendingDelete': (boolean);
  'provider': (_superverse_v2_ModelProvider__Output);
}
