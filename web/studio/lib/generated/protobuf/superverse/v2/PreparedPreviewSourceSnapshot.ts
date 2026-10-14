// Original file: api/superverse/v2/superverse.proto

import type { PreviewSourceStatus as _superverse_v2_PreviewSourceStatus, PreviewSourceStatus__Output as _superverse_v2_PreviewSourceStatus__Output } from '../../superverse/v2/PreviewSourceStatus';
import type { ObjectReference as _superverse_v2_ObjectReference, ObjectReference__Output as _superverse_v2_ObjectReference__Output } from '../../superverse/v2/ObjectReference';
import type { Failure as _superverse_v2_Failure, Failure__Output as _superverse_v2_Failure__Output } from '../../superverse/v2/Failure';
import type { CommitVersion as _superverse_v2_CommitVersion, CommitVersion__Output as _superverse_v2_CommitVersion__Output } from '../../superverse/v2/CommitVersion';
import type { Long } from '@grpc/proto-loader';

export interface PreparedPreviewSourceSnapshot {
  'status'?: (_superverse_v2_PreviewSourceStatus);
  'requestId'?: (string);
  'workspaceRequestId'?: (string);
  'generation'?: (number | string | Long);
  'sourceCommit'?: (string);
  'definitions'?: (_superverse_v2_ObjectReference | null);
  'definitionRevision'?: (string);
  'failure'?: (_superverse_v2_Failure | null);
  'version'?: (_superverse_v2_CommitVersion | null);
}

export interface PreparedPreviewSourceSnapshot__Output {
  'status': (_superverse_v2_PreviewSourceStatus__Output);
  'requestId': (string);
  'workspaceRequestId': (string);
  'generation': (string);
  'sourceCommit': (string);
  'definitions': (_superverse_v2_ObjectReference__Output | null);
  'definitionRevision': (string);
  'failure': (_superverse_v2_Failure__Output | null);
  'version': (_superverse_v2_CommitVersion__Output | null);
}
