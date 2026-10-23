// Original file: api/superverse/v2/superverse.proto

import type { ModelCredentialInfo as _superverse_v2_ModelCredentialInfo, ModelCredentialInfo__Output as _superverse_v2_ModelCredentialInfo__Output } from '../../superverse/v2/ModelCredentialInfo';
import type { ModelCredentialSaveNotice as _superverse_v2_ModelCredentialSaveNotice, ModelCredentialSaveNotice__Output as _superverse_v2_ModelCredentialSaveNotice__Output } from '../../superverse/v2/ModelCredentialSaveNotice';

export interface SetModelCredentialResponse {
  'credential'?: (_superverse_v2_ModelCredentialInfo | null);
  'notice'?: (_superverse_v2_ModelCredentialSaveNotice);
  'noticeMessage'?: (string);
}

export interface SetModelCredentialResponse__Output {
  'credential': (_superverse_v2_ModelCredentialInfo__Output | null);
  'notice': (_superverse_v2_ModelCredentialSaveNotice__Output);
  'noticeMessage': (string);
}
