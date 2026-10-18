// Original file: api/superverse/v2/superverse.proto

import type { AttachmentKind as _superverse_v2_AttachmentKind, AttachmentKind__Output as _superverse_v2_AttachmentKind__Output } from '../../superverse/v2/AttachmentKind';
import type { AttachmentStatus as _superverse_v2_AttachmentStatus, AttachmentStatus__Output as _superverse_v2_AttachmentStatus__Output } from '../../superverse/v2/AttachmentStatus';
import type { AttachmentSummary as _superverse_v2_AttachmentSummary, AttachmentSummary__Output as _superverse_v2_AttachmentSummary__Output } from '../../superverse/v2/AttachmentSummary';
import type { AttachmentFailure as _superverse_v2_AttachmentFailure, AttachmentFailure__Output as _superverse_v2_AttachmentFailure__Output } from '../../superverse/v2/AttachmentFailure';
import type { Long } from '@grpc/proto-loader';

export interface Attachment {
  'attachmentId'?: (string);
  'teamSpaceId'?: (string);
  'fileName'?: (string);
  'mediaType'?: (string);
  'kind'?: (_superverse_v2_AttachmentKind);
  'byteSize'?: (number | string | Long);
  'status'?: (_superverse_v2_AttachmentStatus);
  'summary'?: (_superverse_v2_AttachmentSummary | null);
  'failure'?: (_superverse_v2_AttachmentFailure | null);
  'expiresAt'?: (string);
}

export interface Attachment__Output {
  'attachmentId': (string);
  'teamSpaceId': (string);
  'fileName': (string);
  'mediaType': (string);
  'kind': (_superverse_v2_AttachmentKind__Output);
  'byteSize': (string);
  'status': (_superverse_v2_AttachmentStatus__Output);
  'summary': (_superverse_v2_AttachmentSummary__Output | null);
  'failure': (_superverse_v2_AttachmentFailure__Output | null);
  'expiresAt': (string);
}
