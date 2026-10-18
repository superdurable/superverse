// Original file: api/superverse/v2/superverse.proto

import type { Attachment as _superverse_v2_Attachment, Attachment__Output as _superverse_v2_Attachment__Output } from '../../superverse/v2/Attachment';
import type { AttachmentUploadTarget as _superverse_v2_AttachmentUploadTarget, AttachmentUploadTarget__Output as _superverse_v2_AttachmentUploadTarget__Output } from '../../superverse/v2/AttachmentUploadTarget';

export interface CreateAttachmentUploadResponse {
  'attachment'?: (_superverse_v2_Attachment | null);
  'upload'?: (_superverse_v2_AttachmentUploadTarget | null);
}

export interface CreateAttachmentUploadResponse__Output {
  'attachment': (_superverse_v2_Attachment__Output | null);
  'upload': (_superverse_v2_AttachmentUploadTarget__Output | null);
}
