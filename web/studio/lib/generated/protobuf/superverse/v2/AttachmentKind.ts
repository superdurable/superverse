// Original file: api/superverse/v2/superverse.proto

export const AttachmentKind = {
  ATTACHMENT_KIND_UNSPECIFIED: 'ATTACHMENT_KIND_UNSPECIFIED',
  ATTACHMENT_KIND_IMAGE: 'ATTACHMENT_KIND_IMAGE',
  ATTACHMENT_KIND_DOCUMENT: 'ATTACHMENT_KIND_DOCUMENT',
  ATTACHMENT_KIND_JSON: 'ATTACHMENT_KIND_JSON',
} as const;

export type AttachmentKind =
  | 'ATTACHMENT_KIND_UNSPECIFIED'
  | 0
  | 'ATTACHMENT_KIND_IMAGE'
  | 1
  | 'ATTACHMENT_KIND_DOCUMENT'
  | 2
  | 'ATTACHMENT_KIND_JSON'
  | 3

export type AttachmentKind__Output = typeof AttachmentKind[keyof typeof AttachmentKind]
