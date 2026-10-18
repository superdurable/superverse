// Original file: api/superverse/v2/superverse.proto

import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';
import type { Long } from '@grpc/proto-loader';

export interface CreateAttachmentUploadRequest {
  'teamSpaceId'?: (string);
  'requestId'?: (string);
  'fileName'?: (string);
  'mediaType'?: (string);
  'byteSize'?: (number | string | Long);
  'sha256'?: (string);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface CreateAttachmentUploadRequest__Output {
  'teamSpaceId': (string);
  'requestId': (string);
  'fileName': (string);
  'mediaType': (string);
  'byteSize': (string);
  'sha256': (string);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
