// Original file: api/superverse/v2/superverse.proto

import type { AgentUserInputAnswer as _superverse_v2_AgentUserInputAnswer, AgentUserInputAnswer__Output as _superverse_v2_AgentUserInputAnswer__Output } from '../../superverse/v2/AgentUserInputAnswer';
import type { AgentRequirementsDecision as _superverse_v2_AgentRequirementsDecision, AgentRequirementsDecision__Output as _superverse_v2_AgentRequirementsDecision__Output } from '../../superverse/v2/AgentRequirementsDecision';

export interface AgentAnswerQuestionsInput {
  'callId'?: (string);
  'answers'?: (_superverse_v2_AgentUserInputAnswer)[];
  'requirements'?: (_superverse_v2_AgentRequirementsDecision | null);
}

export interface AgentAnswerQuestionsInput__Output {
  'callId': (string);
  'answers': (_superverse_v2_AgentUserInputAnswer__Output)[];
  'requirements': (_superverse_v2_AgentRequirementsDecision__Output | null);
}
