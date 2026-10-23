// Original file: api/superverse/v2/superverse.proto

export const ModelCredentialSaveNotice = {
  MODEL_CREDENTIAL_SAVE_NOTICE_UNSPECIFIED: 'MODEL_CREDENTIAL_SAVE_NOTICE_UNSPECIFIED',
  MODEL_CREDENTIAL_SAVE_NOTICE_RATE_LIMITED_UNVERIFIED: 'MODEL_CREDENTIAL_SAVE_NOTICE_RATE_LIMITED_UNVERIFIED',
} as const;

export type ModelCredentialSaveNotice =
  | 'MODEL_CREDENTIAL_SAVE_NOTICE_UNSPECIFIED'
  | 0
  | 'MODEL_CREDENTIAL_SAVE_NOTICE_RATE_LIMITED_UNVERIFIED'
  | 1

export type ModelCredentialSaveNotice__Output = typeof ModelCredentialSaveNotice[keyof typeof ModelCredentialSaveNotice]
