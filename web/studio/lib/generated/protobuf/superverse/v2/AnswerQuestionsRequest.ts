// Original file: api/superverse/v2/superverse.proto

import type { AgentAnswerQuestionsInput as _superverse_v2_AgentAnswerQuestionsInput, AgentAnswerQuestionsInput__Output as _superverse_v2_AgentAnswerQuestionsInput__Output } from '../../superverse/v2/AgentAnswerQuestionsInput';
import type { CallerContext as _superverse_v2_CallerContext, CallerContext__Output as _superverse_v2_CallerContext__Output } from '../../superverse/v2/CallerContext';

export interface AnswerQuestionsRequest {
  'projectId'?: (string);
  'input'?: (_superverse_v2_AgentAnswerQuestionsInput | null);
  'caller'?: (_superverse_v2_CallerContext | null);
}

export interface AnswerQuestionsRequest__Output {
  'projectId': (string);
  'input': (_superverse_v2_AgentAnswerQuestionsInput__Output | null);
  'caller': (_superverse_v2_CallerContext__Output | null);
}
