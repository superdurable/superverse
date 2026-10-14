// Original file: api/superverse/v2/superverse.proto

import type { Timestamp as _google_protobuf_Timestamp, Timestamp__Output as _google_protobuf_Timestamp__Output } from '../../google/protobuf/Timestamp';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_PreviewValidation_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_VALIDATED: 'KIND_VALIDATED',
  KIND_SKIPPED: 'KIND_SKIPPED',
  KIND_LIVE_CODE: 'KIND_LIVE_CODE',
} as const;

export type _superverse_v2_PreviewValidation_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_VALIDATED'
  | 1
  | 'KIND_SKIPPED'
  | 2
  | 'KIND_LIVE_CODE'
  | 3

export type _superverse_v2_PreviewValidation_Kind__Output = typeof _superverse_v2_PreviewValidation_Kind[keyof typeof _superverse_v2_PreviewValidation_Kind]

export interface PreviewValidation {
  'kind'?: (_superverse_v2_PreviewValidation_Kind);
  'startCommandId'?: (string);
  'startedAt'?: (_google_protobuf_Timestamp | null);
  'skipReason'?: (string);
  'actorAccountId'?: (string);
}

export interface PreviewValidation__Output {
  'kind': (_superverse_v2_PreviewValidation_Kind__Output);
  'startCommandId': (string);
  'startedAt': (_google_protobuf_Timestamp__Output | null);
  'skipReason': (string);
  'actorAccountId': (string);
}
