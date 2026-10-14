// Original file: api/superverse/v2/superverse.proto

import type { AgentUserInputQuestion as _superverse_v2_AgentUserInputQuestion, AgentUserInputQuestion__Output as _superverse_v2_AgentUserInputQuestion__Output } from '../../superverse/v2/AgentUserInputQuestion';
import type { AgentRequirements as _superverse_v2_AgentRequirements, AgentRequirements__Output as _superverse_v2_AgentRequirements__Output } from '../../superverse/v2/AgentRequirements';

// Original file: api/superverse/v2/superverse.proto

export const _superverse_v2_AgentPendingUserInput_Kind = {
  KIND_UNSPECIFIED: 'KIND_UNSPECIFIED',
  KIND_QUESTION: 'KIND_QUESTION',
  KIND_REQUIREMENTS: 'KIND_REQUIREMENTS',
} as const;

export type _superverse_v2_AgentPendingUserInput_Kind =
  | 'KIND_UNSPECIFIED'
  | 0
  | 'KIND_QUESTION'
  | 1
  | 'KIND_REQUIREMENTS'
  | 2

export type _superverse_v2_AgentPendingUserInput_Kind__Output = typeof _superverse_v2_AgentPendingUserInput_Kind[keyof typeof _superverse_v2_AgentPendingUserInput_Kind]

export interface AgentPendingUserInput {
  'callId'?: (string);
  'questions'?: (_superverse_v2_AgentUserInputQuestion)[];
  'startedAt'?: (string);
  'kind'?: (_superverse_v2_AgentPendingUserInput_Kind);
  'requirements'?: (_superverse_v2_AgentRequirements | null);
  '_startedAt'?: "startedAt";
}

export interface AgentPendingUserInput__Output {
  'callId': (string);
  'questions': (_superverse_v2_AgentUserInputQuestion__Output)[];
  'startedAt'?: (string);
  'kind': (_superverse_v2_AgentPendingUserInput_Kind__Output);
  'requirements': (_superverse_v2_AgentRequirements__Output | null);
  '_startedAt'?: "startedAt";
}
