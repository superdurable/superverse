// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface StartReleaseBuildRequest {
  'projectId'?: (string);
  'requestId'?: (string);
  'releaseId'?: (string);
  'buildRevisionId'?: (string);
  'configurationRevision'?: (number | string | Long);
  'expectedRevision'?: (number | string | Long);
  'autoDeploy'?: (boolean);
  'expectedLiveRevision'?: (number | string | Long);
  'previewSkipReason'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface StartReleaseBuildRequest__Output {
  'projectId': (string);
  'requestId': (string);
  'releaseId': (string);
  'buildRevisionId': (string);
  'configurationRevision': (string);
  'expectedRevision': (string);
  'autoDeploy': (boolean);
  'expectedLiveRevision': (string);
  'previewSkipReason': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
