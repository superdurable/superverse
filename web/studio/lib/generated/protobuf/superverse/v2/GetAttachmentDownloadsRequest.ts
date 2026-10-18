// Original file: api/superverse/v2/superverse.proto

import type { AttachmentDisposition as _superverse_v2_AttachmentDisposition, AttachmentDisposition__Output as _superverse_v2_AttachmentDisposition__Output } from '../../superverse/v2/AttachmentDisposition';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface GetAttachmentDownloadsRequest {
  'projectId'?: (string);
  'attachmentIds'?: (string)[];
  'disposition'?: (_superverse_v2_AttachmentDisposition);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface GetAttachmentDownloadsRequest__Output {
  'projectId': (string);
  'attachmentIds': (string)[];
  'disposition': (_superverse_v2_AttachmentDisposition__Output);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
