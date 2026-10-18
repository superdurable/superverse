// Original file: api/superverse/v2/superverse.proto

export const AttachmentDisposition = {
  ATTACHMENT_DISPOSITION_UNSPECIFIED: 'ATTACHMENT_DISPOSITION_UNSPECIFIED',
  ATTACHMENT_DISPOSITION_INLINE: 'ATTACHMENT_DISPOSITION_INLINE',
  ATTACHMENT_DISPOSITION_DOWNLOAD: 'ATTACHMENT_DISPOSITION_DOWNLOAD',
} as const;

export type AttachmentDisposition =
  | 'ATTACHMENT_DISPOSITION_UNSPECIFIED'
  | 0
  | 'ATTACHMENT_DISPOSITION_INLINE'
  | 1
  | 'ATTACHMENT_DISPOSITION_DOWNLOAD'
  | 2

export type AttachmentDisposition__Output = typeof AttachmentDisposition[keyof typeof AttachmentDisposition]
