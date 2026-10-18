// Original file: api/superverse/v2/superverse.proto

import type { AttachmentKind as _superverse_v2_AttachmentKind, AttachmentKind__Output as _superverse_v2_AttachmentKind__Output } from '../../superverse/v2/AttachmentKind';
import type { Long } from '@grpc/proto-loader';

export interface AgentAttachment {
  'attachmentId'?: (string);
  'fileName'?: (string);
  'mediaType'?: (string);
  'kind'?: (_superverse_v2_AttachmentKind);
  'byteSize'?: (number | string | Long);
  'width'?: (number);
  'height'?: (number);
  'jsonFormat'?: (string);
  'redactions'?: (number);
}

export interface AgentAttachment__Output {
  'attachmentId': (string);
  'fileName': (string);
  'mediaType': (string);
  'kind': (_superverse_v2_AttachmentKind__Output);
  'byteSize': (string);
  'width': (number);
  'height': (number);
  'jsonFormat': (string);
  'redactions': (number);
}
