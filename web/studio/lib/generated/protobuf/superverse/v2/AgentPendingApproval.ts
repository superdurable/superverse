// Original file: api/superverse/v2/superverse.proto


export interface AgentPendingApproval {
  'callId'?: (string);
  'toolName'?: (string);
  'argumentsJson'?: (string);
  'startedAt'?: (string);
  '_startedAt'?: "startedAt";
}

export interface AgentPendingApproval__Output {
  'callId': (string);
  'toolName': (string);
  'argumentsJson': (string);
  'startedAt'?: (string);
  '_startedAt'?: "startedAt";
}
