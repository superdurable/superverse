// Original file: api/superverse/v2/superverse.proto

import type { UploadHeader as _superverse_v2_UploadHeader, UploadHeader__Output as _superverse_v2_UploadHeader__Output } from '../../superverse/v2/UploadHeader';

export interface AttachmentUploadTarget {
  'url'?: (string);
  'headers'?: (_superverse_v2_UploadHeader)[];
  'expiresAt'?: (string);
}

export interface AttachmentUploadTarget__Output {
  'url': (string);
  'headers': (_superverse_v2_UploadHeader__Output)[];
  'expiresAt': (string);
}
