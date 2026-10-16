// Original file: api/superverse/v2/superverse.proto

import type { AgentUserInputAnswer as _superverse_v2_AgentUserInputAnswer, AgentUserInputAnswer__Output as _superverse_v2_AgentUserInputAnswer__Output } from '../../superverse/v2/AgentUserInputAnswer';

export interface AgentAnswerQuestionsInput {
  'callId'?: (string);
  'answers'?: (_superverse_v2_AgentUserInputAnswer)[];
}

export interface AgentAnswerQuestionsInput__Output {
  'callId': (string);
  'answers': (_superverse_v2_AgentUserInputAnswer__Output)[];
}
