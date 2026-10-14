// Original file: api/superverse/v2/superverse.proto

import type { AgentUserInputOption as _superverse_v2_AgentUserInputOption, AgentUserInputOption__Output as _superverse_v2_AgentUserInputOption__Output } from '../../superverse/v2/AgentUserInputOption';

export interface AgentUserInputQuestion {
  'id'?: (string);
  'header'?: (string);
  'question'?: (string);
  'options'?: (_superverse_v2_AgentUserInputOption)[];
}

export interface AgentUserInputQuestion__Output {
  'id': (string);
  'header': (string);
  'question': (string);
  'options': (_superverse_v2_AgentUserInputOption__Output)[];
}
